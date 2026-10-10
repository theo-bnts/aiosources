package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func ageCollectionHistory(t *testing.T, ctx context.Context, db *Store, id string) {
	t.Helper()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('ingest.history_retention','on',true)`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE ingest.runs SET created_at=created_at-interval '8 days',started_at=started_at-interval '8 days',finished_at=finished_at-interval '8 days' WHERE id=$1`,
		`UPDATE ingest.torrents SET first_seen_at=first_seen_at-interval '8 days',last_seen_at=last_seen_at-interval '8 days',collection_verified_at=collection_verified_at-interval '8 days' WHERE raw_id IN (SELECT id FROM ingest.raw_records WHERE run_id=$1)`,
		`UPDATE ingest.pages SET created_at=created_at-interval '8 days' WHERE run_id=$1`,
		`UPDATE ingest.raw_records SET created_at=created_at-interval '8 days' WHERE run_id=$1`,
		`UPDATE ingest.events SET created_at=created_at-interval '8 days' WHERE run_id=$1`,
		`UPDATE ingest.activity_logs SET created_at=created_at-interval '8 days' WHERE run_id=$1`,
		`UPDATE ingest.torrent_history SET occurred_at=occurred_at-interval '8 days' WHERE run_id=$1`,
	} {
		if _, err = tx.Exec(ctx, query, id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func drainHistory(t *testing.T, ctx context.Context, db *Store) {
	t.Helper()
	for range 12 {
		removed, err := db.PruneHistory(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if removed == 0 {
			return
		}
	}
	t.Fatal("history cleanup did not settle")
}

func TestHistoryRetentionPreservesPublishedDataAndCollectionBaseline(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{ID: "retained-source", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("published")}, Done: true}, "finished-page")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	before := mirrorRows(t, ctx, db, provider.ID)["published"]
	ageCollectionHistory(t, ctx, db, run.ID)
	var queueBefore int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_match_queue`).Scan(&queueBefore); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	after := mirrorRows(t, ctx, db, provider.ID)["published"]
	if !reflect.DeepEqual(before.Fields, after.Fields) || after.RawID != nil || after.SourceID != "published" {
		t.Fatalf("history cleanup changed publication: before=%+v after=%+v", before, after)
	}
	if _, err = db.GetRun(ctx, run.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expired run remains: %v", err)
	}
	known, err := db.Known(ctx, provider.ID, []string{"published"}, time.Now().Add(time.Second))
	if err != nil || !known["published"] {
		t.Fatalf("expiry lost a successful boundary: %v %v", known, err)
	}
	next, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || next.Mode != model.ModeIncremental || next.InitialFull {
		t.Fatalf("expiry restarted initial Full: %+v %v", next, err)
	}
	var queueAfter int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_match_queue`).Scan(&queueAfter); err != nil || queueAfter != queueBefore {
		t.Fatalf("expiry created false identification work: %d %d %v", queueBefore, queueAfter, err)
	}
	for _, table := range []string{"raw_records", "pages", "events", "torrent_history"} {
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table).Scan(&count); err != nil || (table != "events" && count != 0) {
			t.Fatalf("old %s retained: %d %v", table, count, err)
		}
	}
}

func TestHistoryRetentionPreservesOldFullRecoveryAndCoverage(t *testing.T) {
	for _, status := range []model.RunStatus{model.StatusRunning, model.StatusPaused, model.StatusFailed, model.StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			ctx, endpoint, db := activityDatabase(t)
			provider := model.Provider{ID: "unfinished", Adapter: "http_json"}
			run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
			cursor := json.RawMessage(`{"page":2}`)
			run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("first", "root")}, Next: cursor, Metadata: map[string]any{"traversal_phase": "options_list", "fingerprint_scope": "root-filter", "total": 1}}, "first-page")
			if err != nil {
				t.Fatal(err)
			}
			if status != model.StatusRunning {
				reason := model.PauseReason("")
				if status == model.StatusPaused {
					reason = model.PauseBudget
				}
				if _, err = db.FinishRun(ctx, run.ID, status, "", reason); err != nil {
					t.Fatal(err)
				}
			}
			ageCollectionHistory(t, ctx, db, run.ID)
			drainHistory(t, ctx, db)
			if live := mirrorRows(t, ctx, db, provider.ID); len(live) != 0 {
				t.Fatal("cleanup published unfinished staging")
			}
			var staged, raw int
			if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1 AND raw_id IS NULL),(SELECT count(*) FROM ingest.raw_records WHERE run_id=$1)`, run.ID).Scan(&staged, &raw); err != nil || staged != 1 || raw != 0 {
				t.Fatalf("staging or expired records changed: %d %d %v", staged, raw, err)
			}
			if complete, e := db.FilterComplete(ctx, run.ID, "root", []string{"root-filter"}); e != nil || !complete {
				t.Fatalf("expiry lost filter evidence: %v %v", complete, e)
			}
			if status == model.StatusRunning {
				if _, err = db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			reopened, e := Open(ctx, endpoint)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			if _, err = reopened.ResumeRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			resumed, e := reopened.ClaimNext(ctx)
			if e != nil || resumed == nil {
				t.Fatalf("resume: %+v %v", resumed, e)
			}
			if string(resumed.Cursor) != `{"page": 2}` || resumed.DistinctRecords != 1 {
				t.Fatalf("checkpoint changed: %+v", resumed)
			}
			_, err = reopened.SavePage(ctx, *resumed, model.Page{Items: []model.Record{coverageRecord("first", "root")}, Next: json.RawMessage(`{"page":3}`)}, "first-page")
			if !errors.Is(err, model.ErrStalled) {
				t.Fatalf("expiry lost duplicate-page detection: %v", err)
			}
			saved, e := reopened.GetRun(ctx, run.ID)
			if e != nil {
				t.Fatal(e)
			}
			saved, err = reopened.SavePage(ctx, saved, model.Page{Items: []model.Record{coverageRecord("second", "root")}, Done: true}, "second-page")
			if err != nil {
				t.Fatal(err)
			}
			mirrorFinish(t, ctx, reopened, saved, model.StatusSucceeded)
			if live := mirrorRows(t, ctx, reopened, provider.ID); len(live) != 2 || live["first"].Fields["title"] == nil {
				t.Fatalf("resumed publication lost committed staging: %+v", live)
			}
		})
	}
}

func TestHistoryRetentionIsBoundedAndCancellationRollsBack(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "bounded", Adapter: "http_json"}, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("staged")}, Next: json.RawMessage(`{"page":2}`)}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,created_at)
 SELECT $1,'bounded',n::text,(SELECT id FROM ingest.pages WHERE run_id=$1 LIMIT 1),1,'','application/json',now() FROM generate_series(1,$2::int) n`, run.ID, historyBatchSize); err != nil {
		t.Fatal(err)
	}
	ageCollectionHistory(t, ctx, db, run.ID)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = db.PruneHistory(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled cleanup: %v", err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.raw_records`).Scan(&count); err != nil || count != historyBatchSize+1 {
		t.Fatalf("cancelled cleanup removed content: %d %v", count, err)
	}
	if _, err = db.PruneHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.raw_records`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup exceeded its record batch: %d %v", count, err)
	}
	drainHistory(t, ctx, db)
}

func TestHistoryRetentionPreservesMetadataParentAndRetryGeneration(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("metadata-parent")
	provider.Traversal.MetadataAfterIncremental = true
	parent := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	parent, err := db.SavePage(ctx, parent, model.Page{Items: []model.Record{coverageRecord("new")}, Done: true}, "listing")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, parent, model.StatusSucceeded)
	ageCollectionHistory(t, ctx, db, parent.ID)
	drainHistory(t, ctx, db)
	child, err := db.ClaimNext(ctx)
	if err != nil || child == nil || child.MetadataParentRunID != parent.ID {
		t.Fatalf("follow-up was lost: %+v %v", child, err)
	}
	candidates, err := db.MetadataCandidates(ctx, *child, "", 10)
	if err != nil || len(candidates) != 1 || candidates[0].SourceID != "new" {
		t.Fatalf("expired parent lost follow-up membership: %+v %v", candidates, err)
	}
	if _, err = db.FinishRun(ctx, child.ID, model.StatusFailed, "temporary failure", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ResumeRun(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	attempt, err := db.CoverageAttempt(ctx, child.ID)
	if err != nil || attempt == "" {
		t.Fatalf("missing retry generation: %q %v", attempt, err)
	}
	ageCollectionHistory(t, ctx, db, child.ID)
	drainHistory(t, ctx, db)
	if after, e := db.CoverageAttempt(ctx, child.ID); e != nil || after != attempt {
		t.Fatalf("event expiry lost retry generation: %q %q %v", attempt, after, e)
	}
}

func TestHistoryRetentionCompactsCatalogWithoutChangingRecentSnapshots(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "shared", "a", `{"title":"A original"}`, nil)
	publishSharingRow(t, ctx, db, "shared", "b", `{"title":"B original"}`, nil)
	grant := sharingGrant(t, ctx, db)
	oldPage := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	oldFull := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	publishSharingRow(t, ctx, db, "shared", "a", `{"title":"A at boundary"}`, nil)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('ingest.history_retention','on',true); UPDATE ingest.catalog_journal SET recorded_at=now()-interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "shared", "c", `{"title":"C recent"}`, nil)
	recent := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	recentFull := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	publishSharingRow(t, ctx, db, "shared", "a", `{"title":"A after snapshot"}`, nil)
	drainHistory(t, ctx, db)
	if _, err = db.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{Cursor: oldPage.NextCursor}); !errors.Is(err, ErrCatalogExpired) {
		t.Fatalf("expired cursor was accepted: %v", err)
	}
	reset := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: oldFull.Checkpoint})
	if reset.Mode != "full" || len(reset.Items) != 3 {
		t.Fatalf("old checkpoint did not restart authoritatively: %+v", reset)
	}
	next := sharingPage(t, ctx, db, grant, CatalogPageRequest{Cursor: recent.NextCursor})
	if len(next.Items) != 1 || next.Items[0].Fields["title"] != "A at boundary" {
		t.Fatalf("compaction changed a recent snapshot: %+v", next)
	}
	delta := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: recentFull.Checkpoint})
	if delta.Mode != "incremental" || len(delta.Items) != 1 || delta.Items[0].Fields["title"] != "A after snapshot" {
		t.Fatalf("recent delta changed: %+v", delta)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.catalog_journal)+(SELECT count(*) FROM ingest.catalog_baselines)`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("old duplicate versions remain: %d %v", count, err)
	}
	if _, err = db.pool.Exec(ctx, `DELETE FROM ingest.catalog_journal`); err == nil {
		t.Fatal("cleanup left the journal mutable")
	}
}

func TestHistoryRetentionCatalogRemainsCompleteAcrossCleanupBatches(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "shared", "a", `{"title":"A original"}`, nil)
	publishSharingRow(t, ctx, db, "shared", "b", `{"title":"B original"}`, nil)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.catalog_journal(provider_id,source_id,origin,fields,deleted)
SELECT 'shared','a',original.origin,jsonb_build_object('title','A version '||version),FALSE
FROM generate_series(1,$1::integer) version CROSS JOIN LATERAL (
 SELECT origin FROM ingest.catalog_journal WHERE provider_id='shared' AND source_id='a' ORDER BY sequence LIMIT 1
) original`, historyBatchSize); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('ingest.history_retention','on',true);
UPDATE ingest.catalog_journal SET recorded_at=now()-interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "shared", "a", `{"title":"A current"}`, nil)
	grant := sharingGrant(t, ctx, db)
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	if _, err = db.PruneHistory(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal WHERE recorded_at<now()-interval '7 days'`).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("first cleanup was not bounded: %d %v", remaining, err)
	}
	next := sharingPage(t, ctx, db, grant, CatalogPageRequest{Cursor: page.NextCursor})
	if len(next.Items) != 1 || next.Items[0].Fields["title"] != "A current" {
		t.Fatalf("partial cleanup changed the paged snapshot: %+v", next)
	}
	drainHistory(t, ctx, db)
	full := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if len(full.Items) != 2 || full.Items[0].Fields["title"] != "B original" || full.Items[1].Fields["title"] != "A current" {
		t.Fatalf("later cleanup lost a live catalog copy: %+v", full)
	}
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.catalog_baselines)+(SELECT count(*) FROM ingest.catalog_journal)`).Scan(&remaining); err != nil || remaining != 3 {
		t.Fatalf("expired versions remain: %d %v", remaining, err)
	}
}

func TestHistoryRetentionExpiresOperationsLogsAndIdleStremioRequests(t *testing.T) {
	ctx, db := sharingDatabase(t)
	old, err := db.BeginOperation(ctx, model.OperationRun{Kind: "matching", Service: "local", Trigger: "manual"}, "matcher")
	if err != nil {
		t.Fatal(err)
	}
	old.Status = "succeeded"
	if err = db.SaveOperation(ctx, old); err != nil {
		t.Fatal(err)
	}
	active, err := db.BeginOperation(ctx, model.OperationRun{Kind: "metadata", Service: "tmdb", Trigger: "automatic"}, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{8 * 24 * time.Hour, 6 * 24 * time.Hour} {
		if err = db.RecordStremioRequest(ctx, model.StremioRequest{CreatedAt: time.Now().Add(-age), MediaType: "movie", Resource: "imdb:tt123", Outcome: "empty"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.RecordActivity(ctx, "info", "service.started", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.operation_runs SET created_at=created_at-interval '8 days',updated_at=updated_at-interval '8 days',finished_at=finished_at-interval '8 days'; UPDATE ingest.operation_events SET created_at=created_at-interval '8 days'; UPDATE ingest.activity_logs SET created_at=created_at-interval '8 days'; INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results) VALUES(now()-interval '8 days','movie','imdb:tt123',1,'empty',0,0,0,0,0,0,0,false,'[]')`); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	if _, err = db.Operation(ctx, old.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("closed operation remains: %v", err)
	}
	if current, e := db.Operation(ctx, active.ID); e != nil || current.Status != "running" {
		t.Fatalf("cleanup expired active work: %+v %v", current, e)
	}
	logs, e := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}})
	if e != nil || logs.Total != 0 {
		t.Fatalf("old logs remain: %+v %v", logs, e)
	}
	stats, e := db.StremioStatistics(ctx)
	if e != nil || stats.Requests != 1 {
		t.Fatalf("idle requests were not purged: %+v %v", stats, e)
	}
}

func TestHistoryRetentionMigrationKeepsLegacyAcceptanceEvidence(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 45); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id     string
		reject bool
	}{{"trusted", false}, {"partial", true}} {
		run, err := db.CreateRun(ctx, model.Run{ProviderID: tc.id, Config: model.Provider{ID: tc.id, Adapter: "http_json"}, Mode: model.ModeIncremental})
		if err != nil {
			t.Fatal(err)
		}
		var page int64
		if err = db.pool.QueryRow(ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type) VALUES($1,$2,1,'','application/json') RETURNING id`, run.ID, tc.id).Scan(&page); err != nil {
			t.Fatal(err)
		}
		errorText := ""
		if tc.reject {
			errorText = "invalid primary"
		}
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,error) VALUES($1,$2,'accepted',$3,1,'','application/json',''),($1,$2,'rejected',$3,1,'','application/json',$4)`, []any{run.ID, tc.id, page, errorText}},
			{`INSERT INTO ingest.run_record_identities(run_id,source_id) VALUES($1,'accepted')`, []any{run.ID}},
			{`INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,'accepted','{"title":"Legacy"}')`, []any{tc.id}},
			{`UPDATE ingest.runs SET status='succeeded',finished_at=now(),errors=1 WHERE id=$1`, []any{run.ID}},
		} {
			if _, err = db.pool.Exec(ctx, statement.query, statement.args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    string
		known bool
	}{{"trusted", true}, {"partial", false}} {
		known, e := db.Known(ctx, tc.id, []string{"accepted"}, time.Now().Add(time.Second))
		if e != nil || known["accepted"] != tc.known {
			t.Fatalf("upgrade changed %s acceptance: %v %v", tc.id, known, e)
		}
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRetentionDetachesRecentReferencesToExpiredArchives(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{ID: "archive-reference", Adapter: "http_json"}
	old := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	old, err := db.SavePage(ctx, old, model.Page{Items: []model.Record{coverageRecord("same")}, Done: true}, "old-page")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, old, model.StatusSucceeded)
	var oldRaw, oldPage int64
	if err = db.pool.QueryRow(ctx, `SELECT id,page_id FROM ingest.raw_records WHERE run_id=$1`, old.ID).Scan(&oldRaw, &oldPage); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.raw_records SET raw='original',fields='{"title":"Original"}',payload_retained=TRUE WHERE id=$1`, oldRaw); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.pages SET body='original',payload_retained=TRUE WHERE id=$1`, oldPage); err != nil {
		t.Fatal(err)
	}
	ageCollectionHistory(t, ctx, db, old.ID)
	recent := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	recent, err = db.SavePage(ctx, recent, model.Page{Items: []model.Record{coverageRecord("same")}, Done: true}, "recent-page")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, recent, model.StatusSucceeded)
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.raw_records SET fields='{}',payload_id=$2,payload_retained=TRUE WHERE run_id=$1`, recent.ID, oldRaw); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.pages SET payload_id=$2,payload_retained=TRUE WHERE run_id=$1`, recent.ID, oldPage); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	for _, table := range []string{"raw_records", "pages"} {
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE run_id=$1 AND payload_id IS NULL AND NOT payload_retained`, recent.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("recent %s lost its identity or retained an expired reference: %d %v", table, count, err)
		}
	}
	if _, err = db.GetRun(ctx, recent.ID); err != nil {
		t.Fatal("recent run expired", err)
	}
	if _, err = db.GetRun(ctx, old.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("expired run remains", err)
	}
}

func TestHistoryRetentionRollsBackWhenPublicationIsBusy(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "busy-publication", Adapter: "http_json"}, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("held")}, Next: json.RawMessage(`{"page":2}`)}, "held-page")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseManual); err != nil {
		t.Fatal(err)
	}
	ageCollectionHistory(t, ctx, db, run.ID)
	reader, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(reader)
	if _, err = reader.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3)`); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err = db.PruneHistory(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked cleanup did not respect its deadline: %v", err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.raw_records WHERE run_id=$1`, run.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("interrupted cleanup partially committed: %d %v", count, err)
	}
	rollback(reader)
	drainHistory(t, ctx, db)
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.raw_records WHERE run_id=$1`, run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cleanup did not retry safely: %d %v", count, err)
	}
}
