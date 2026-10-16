package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func setStorageMode(t *testing.T, ctx context.Context, db *Store, mode model.StorageMode) {
	t.Helper()
	current, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Settings.Mode = mode
	if err := db.UpdateStorageSettings(ctx, current.Settings); err != nil {
		t.Fatal(err)
	}
}

func TestStorageSavingPreservesMatchingAndPlaybackDataWithMuchSmallerRecords(t *testing.T) {
	ctx, db := sharingDatabase(t)
	settings, err := db.StorageOverview(ctx)
	if err != nil || settings.Settings.Mode != model.StorageCompact {
		t.Fatalf("compact must be the default: %+v %v", settings, err)
	}
	fields := map[string]any{
		"title": "Example.Series.S02.COMPLETE.MULTi.2160p.WEB.H265", "info_hash": strings.Repeat("a", 40),
		"size": json.Number("9007199254740993"), "published_at": "2026-09-01T12:00:00Z", "duration": json.Number("3600"),
		"external_ids": map[string]any{"imdb": []any{"tt1234567", "tt7654321"}, "tvdb": "55", "unused": strings.Repeat("x", 2000)},
		"attributes":   map[string]any{"tmdbid": []any{"42"}, "season": []any{"2"}, "language": []any{"fr", "en"}, "description": strings.Repeat("x", 2000)},
		"categories":   []any{json.Number("5000")}, "description": strings.Repeat("large unused description ", 6000),
		"download_url": "https://example.invalid/unused-download", "magnet": "magnet:?xt=urn:btih:" + strings.Repeat("a", 40),
		"poster": "https://example.invalid/poster.jpg", "filelist": []any{map[string]any{"unused": "payload"}},
	}
	var records []model.AIORecord
	var sizes []int
	for _, mode := range []model.StorageMode{model.StorageCompact, model.StorageFull} {
		setStorageMode(t, ctx, db, mode)
		provider := model.Provider{ID: string(mode), Adapter: "http_json"}
		run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
		if run.StorageMode != mode {
			t.Fatalf("storage policy was not captured: %+v", run)
		}
		run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{{SourceID: "one", Fields: fields, CoverageScopes: []string{"chosen"}}}, Done: true}, "complete")
		if err != nil {
			t.Fatal(err)
		}
		assertCoverage(t, ctx, db, run.ID, map[string]int64{"chosen": 1}, []string{"one"}, map[string]bool{"one": true})
		mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
		for _, query := range []string{"synthetic-fragment-unused", "Seri", strings.Repeat("a", 12)} {
			results, err := db.ListTorrents(ctx, model.ListOptions{
				Query: query, ProviderID: provider.ID, Category: "5000",
				MinSize: "9007199254740993", MaxSize: "9007199254740993",
				PublishedAfter: "2026-09-01T00:00:00Z", PublishedBefore: "2026-09-02T00:00:00Z",
			})
			want := int64(1)
			if query == "synthetic-fragment-unused" {
				want = 0
			}
			if err != nil || results.Total != want || len(results.Items) != int(want) {
				t.Fatalf("%s storage changed partial search or combined filters: %+v %v", mode, results, err)
			}
		}
		record, err := scanAIORecord(db.pool.QueryRow(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t WHERE provider_id=$1`, provider.ID))
		if err != nil {
			t.Fatal(err)
		}
		record.Provider, record.Fingerprint = "", ""
		records = append(records, record)
		var size int
		if err := db.pool.QueryRow(ctx, `SELECT octet_length(fields::text) FROM ingest.torrents WHERE provider_id=$1`, provider.ID).Scan(&size); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, size)
	}
	if !reflect.DeepEqual(records[0], records[1]) || records[0].IMDb != "invalid conflicting values" || records[0].Season == nil || *records[0].Season != 2 {
		t.Fatalf("storage saving changed matching/playback evidence: %+v versus %+v", records[0], records[1])
	}
	if sizes[0] >= sizes[1]/50 {
		t.Fatalf("unused payload was retained: compact=%d full=%d", sizes[0], sizes[1])
	}
	if fields["description"] == nil || fields["external_ids"].(map[string]any)["unused"] == nil {
		t.Fatal("source objects were mutated")
	}
	var compactObservations, fullObservations int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE provider_id='compact'),count(*) FILTER(WHERE provider_id='full') FROM ingest.raw_records`).Scan(&compactObservations, &fullObservations); err != nil || compactObservations != 0 || fullObservations != 1 {
		t.Fatalf("successful observations were duplicated in compact mode: %d %d %v", compactObservations, fullObservations, err)
	}
}

func TestCompactStorageRetainsErrorsAndFilterEvidenceWithoutSuccessObservations(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := model.Provider{ID: "compact-evidence", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	page := model.Page{Position: "options:scope:one", Items: []model.Record{coverageRecord("one", "scope"), coverageRecord("two", "scope")},
		Metadata: map[string]any{"traversal_phase": "options_list", "total": 2, "fingerprint_scope": "filter"}, Next: json.RawMessage(`{"page":2}`)}
	var err error
	run, err = db.SavePage(ctx, run, page, "filter")
	if err != nil {
		t.Fatal(err)
	}
	var observations, filterIDs int
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.raw_records WHERE run_id=$1),(SELECT count(*) FROM ingest.run_filter_records WHERE page_id IN (SELECT id FROM ingest.pages WHERE run_id=$1))`, run.ID).Scan(&observations, &filterIDs); err != nil || observations != 0 || filterIDs != 2 {
		t.Fatalf("durable filter evidence was lost: %d %d %v", observations, filterIDs, err)
	}
	complete, err := db.FilterComplete(ctx, run.ID, "scope", []string{"filter"})
	if err != nil || !complete {
		t.Fatalf("compact observations broke filter completeness: %v %v", complete, err)
	}
	invalid := coverageRecord("bad", "scope")
	invalid.Fields["unused"] = "invalid\x00value"
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{invalid}, Next: json.RawMessage(`{"page":3}`)}, "bad")
	if err != nil || run.Errors != 1 || run.DistinctRecords != 2 || string(run.Cursor) != `{"page": 2}` {
		t.Fatalf("discarded fields masked an invalid response or advanced coverage: %+v %v", run, err)
	}
	raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
	if err != nil || raw.Total != 1 || raw.Items[0].Error == "" {
		t.Fatalf("record error was not retained: %+v %v", raw, err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusPaused)
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"scope": 2}, []string{"one", "two", "bad"}, map[string]bool{"one": true, "two": true})
	final, err := db.SavePage(ctx, *resumed, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, final, model.StatusSucceeded)
	if rows := mirrorRows(t, ctx, db, provider.ID); len(rows) != 2 {
		t.Fatalf("resume lost staged records: %+v", rows)
	}
}

func TestStorageSnapshotSurvivesResumeAndChangesApplyOnlyToNewImports(t *testing.T) {
	ctx, db := sharingDatabase(t)
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := model.Provider{ID: "continued", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{{SourceID: "one", Fields: map[string]any{"title": "one", "description": "old selected detail"}}}}, "one")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusPaused)
	setStorageMode(t, ctx, db, model.StorageCompact)
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || resumed.StorageMode != model.StorageFull {
		t.Fatalf("resume changed storage mode: %+v %v", resumed, err)
	}
	completed, err := db.SavePage(ctx, *resumed, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, completed, model.StatusSucceeded)
	fresh := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	if fresh.StorageMode != model.StorageCompact {
		t.Fatal("new import did not adopt compact storage")
	}
	fresh, err = db.SavePage(ctx, fresh, model.Page{Items: []model.Record{{SourceID: "two", Fields: map[string]any{"title": "two", "description": "new unused detail"}}}, Done: true}, "two")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, fresh, model.StatusSucceeded)
	rows := mirrorRows(t, ctx, db, provider.ID)
	if rows["one"].Fields["description"] != "old selected detail" || rows["two"].Fields["description"] != nil {
		t.Fatalf("existing data was erased or new details retained: %+v", rows)
	}
}

func TestCompactMetadataFollowupKeepsTheDiscoveryModeAndSkipsDiscardedRequirements(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := metadataSource("compact-metadata")
	provider.Traversal.EnrichFields = []string{"external_ids", "description"}
	provider.Traversal.MetadataAfterIncremental = true
	parent := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	parent, err := db.SavePage(ctx, parent, model.Page{Items: []model.Record{coverageRecord("one")}, Done: true}, "discovery")
	if err != nil {
		t.Fatal(err)
	}
	setStorageMode(t, ctx, db, model.StorageFull)
	mirrorFinish(t, ctx, db, parent, model.StatusSucceeded)
	child, err := db.ClaimNext(ctx)
	if err != nil || child == nil || child.Mode != model.ModeMetadata || child.StorageMode != model.StorageCompact {
		t.Fatalf("metadata child changed its admitted storage mode: %+v %v", child, err)
	}
	if !reflect.DeepEqual(child.MetadataFields(), []string{"external_ids"}) {
		t.Fatalf("discarded details remained required: %v", child.MetadataFields())
	}
	candidates, err := db.MetadataCandidates(ctx, *child, "", 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("metadata identity ledger was lost: %+v %v", candidates, err)
	}
	detail := coverageRecord("one")
	detail.Fields["external_ids"] = map[string]any{"imdb": "tt1234567"}
	detail.Fields["description"] = "discard this detail"
	complete, err := db.SavePage(ctx, *child, model.Page{Items: []model.Record{detail}, Done: true}, "detail")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, complete, model.StatusSucceeded)
	rows := mirrorRows(t, ctx, db, provider.ID)
	if rows["one"].Fields["description"] != nil || rows["one"].Fields["external_ids"].(map[string]any)["imdb"] != "tt1234567" {
		t.Fatalf("essential metadata was lost or unused details retained: %+v", rows)
	}
}

func TestUnusedSharingKeepsNoSnapshotsAndActivationRemainsComplete(t *testing.T) {
	ctx, db := sharingDatabase(t)
	countSnapshots := func(want int64) {
		t.Helper()
		var count int64
		if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.catalog_journal)+(SELECT count(*) FROM ingest.catalog_baselines)`).Scan(&count); err != nil || count != want {
			t.Fatalf("snapshots=%d want=%d: %v", count, want, err)
		}
	}
	publishSharingRow(t, ctx, db, "native", "one", `{"title":"one","size":9007199254740993,"description":"unused","download_url":"private"}`, nil)
	countSnapshots(0)
	grant := sharingGrant(t, ctx, db)
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if len(page.Items) != 1 || page.Items[0].Fields["size"] != json.Number("9007199254740993") {
		t.Fatalf("first share missed existing catalog: %+v", page)
	}
	var fields []byte
	if err := db.pool.QueryRow(ctx, `SELECT fields FROM ingest.catalog_journal LIMIT 1`).Scan(&fields); err != nil || strings.Contains(string(fields), "private") || strings.Contains(string(fields), "description") {
		t.Fatalf("sharing copied private/unused fields: %s %v", fields, err)
	}
	second := sharingGrant(t, ctx, db)
	if err := db.DeleteShare(ctx, grant.Share.ID, grant.Share.Revision); err != nil {
		t.Fatal(err)
	}
	countSnapshots(1)
	if err := db.DeleteShare(ctx, second.Share.ID, second.Share.Revision); err != nil {
		t.Fatal(err)
	}
	countSnapshots(0)
	publishSharingRow(t, ctx, db, "native", "two", `{"title":"two"}`, nil)
	countSnapshots(0)
	third := sharingGrant(t, ctx, db)
	full := sharingPage(t, ctx, db, third, CatalogPageRequest{Limit: 1})
	if len(full.Items) != 1 || full.NextCursor == "" {
		t.Fatalf("rebuilt snapshot missed records: %+v", full)
	}
	last := sharingPage(t, ctx, db, third, CatalogPageRequest{Cursor: full.NextCursor})
	if len(last.Items) != 1 || last.Checkpoint == "" {
		t.Fatalf("rebuilt continuation incomplete: %+v", last)
	}
	// Expiration is reconciled by the same recurring cleanup as other histories.
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.shares SET expires_at=now()-interval '1 minute' WHERE id=$1`, third.Share.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PruneHistory(ctx); err != nil {
		t.Fatal(err)
	}
	countSnapshots(0)
}

func TestSourceResetIsFencedAndRetainsConfigurationMetadataAndCache(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := model.Provider{ID: "resettable", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("one", "scope")}, Done: true}, "all")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := db.StorageOverview(ctx)
	if err := db.ResetSourceData(ctx, settings.Settings.Revision); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("running reset accepted: %v", err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	grant := sharingGrant(t, ctx, db)
	before := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('tv','42','tt1234567',ARRAY[2025]);
 INSERT INTO ingest.aio_availability(service,account,hash,cached,checked_at) VALUES('torbox','account','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',TRUE,now())`); err != nil {
		t.Fatal(err)
	}
	// A queued attempt is removed, never silently resumed against old staging.
	queued, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ResetSourceData(ctx, settings.Settings.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetRun(ctx, queued.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("queued checkpoint survived reset: %v", err)
	}
	for _, table := range []string{"torrents", "raw_records", "pages", "runs", "events", "staged_torrents", "torrent_history", "run_scope_memberships", "aio_match_queue", "catalog_journal", "catalog_baselines", "collection_state"} {
		var count int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s survived reset: %d %v", table, count, err)
		}
	}
	for _, table := range []string{"shares", "aio_works", "aio_availability"} {
		var count int
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("useful %s was lost: %d %v", table, count, err)
		}
	}
	if empty, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{Checkpoint: before.Checkpoint}); err != nil || empty.Mode != "full" || len(empty.Items) != 0 {
		t.Fatalf("old checkpoint did not force an empty full catalog after reset: %+v %v", empty, err)
	}
	current, _ := db.StorageOverview(ctx)
	if current.Settings.Revision <= settings.Settings.Revision {
		t.Fatal("reset was not protected against replay")
	}
	if err := db.ResetSourceData(ctx, settings.Settings.Revision); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale reset accepted: %v", err)
	}
	fresh, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || fresh.Mode != model.ModeFull || !fresh.InitialFull {
		t.Fatalf("reset retained old collection baseline: %+v %v", fresh, err)
	}
	var logged int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='storage.sources_reset' AND created_at>$1`, time.Now().Add(-time.Minute)).Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("reset was not logged once: %d %v", logged, err)
	}
}

func TestStorageMigrationClearsUnusedSharingWithoutRewritingCatalogOrOldRunModes(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "unused", true: "active"}[active], func(t *testing.T) {
			ctx, endpoint := testutil.NewDatabase(t)
			db, err := Open(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := db.migrateThrough(ctx, 50); err != nil {
				t.Fatal(err)
			}
			fields := `{"title":"Legacy title","description":"` + strings.Repeat("old selected detail ", 1000) + `"}`
			publishSharingRow(t, ctx, db, "legacy", "one", fields, nil)
			publishSharingRow(t, ctx, db, "legacy", "two", fields, nil)
			if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,revision,mode,status,config)
 VALUES('legacy-queued','legacy-run','Legacy','','full','queued','{"id":"legacy-run"}')`); err != nil {
				t.Fatal(err)
			}
			var grant model.ShareCredential
			var first model.CatalogEnvelope
			if active {
				grant = legacyShareFixture(t, ctx, db, model.ShareInput{Name: "Legacy recipient", Scope: "all", Enabled: true, Fields: CatalogFields()})
				first = legacyCatalogPageFixture(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
			}
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			old, err := db.GetRun(ctx, "legacy-queued")
			if err != nil || old.StorageMode != model.StorageFull {
				t.Fatalf("upgrade changed an existing snapshot: %+v %v", old, err)
			}
			rows := mirrorRows(t, ctx, db, "legacy")
			if len(rows) != 2 || rows["one"].Fields["description"] != strings.Repeat("old selected detail ", 1000) {
				t.Fatal("upgrade rewrote published source fields")
			}
			var snapshots int
			if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal`).Scan(&snapshots); err != nil {
				t.Fatal(err)
			}
			if active {
				last := sharingPage(t, ctx, db, grant, CatalogPageRequest{Cursor: first.NextCursor})
				if snapshots != 2 || len(last.Items) != 1 || last.Checkpoint == "" {
					t.Fatalf("upgrade broke an active snapshot: %+v, rows=%d", last, snapshots)
				}
			} else if snapshots != 0 {
				t.Fatal("unused journal remained allocated")
			}
			if err := db.Migrate(ctx); err != nil {
				t.Fatal("repeated upgrade was not idempotent:", err)
			}
		})
	}
}

func TestSourceResetRollsBackDataAndSharingStateIfTheTransactionFails(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "kept", "one", `{"title":"Keep this"}`, nil)
	grant := sharingGrant(t, ctx, db)
	before := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	settings, _ := db.StorageOverview(ctx)
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.activity_logs ADD CONSTRAINT test_reset_failure CHECK(kind<>'storage.sources_reset')`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetSourceData(ctx, settings.Settings.Revision); err == nil {
		t.Fatal("synthetic reset failure was ignored")
	}
	if rows := mirrorRows(t, ctx, db, "kept"); len(rows) != 1 || rows["one"].Fields["title"] != "Keep this" {
		t.Fatal("failed reset removed published data")
	}
	if got, _ := db.StorageOverview(ctx); got.Settings != settings.Settings {
		t.Fatal("failed reset advanced the revision")
	}
	delta := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: before.Checkpoint})
	if delta.Mode != "incremental" || len(delta.Items) != 0 {
		t.Fatalf("failed reset changed sharing boundaries: %+v", delta)
	}
}

func waitForStorageLock(t *testing.T, ctx context.Context, db *Store, query string, args ...any) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var ready bool
		if err := db.pool.QueryRow(ctx, query, args...).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("operation did not reach its expected lock boundary")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestSharingPolicyEditsCannotDeadlockAConcurrentSourceReset(t *testing.T) {
	parent, db := sharingDatabase(t)
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	grant := sharingGrant(t, ctx, db)
	publication, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(publication)
	if _, err := publication.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3)`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := db.UpdateShare(ctx, grant.Share.ID, model.ShareInput{
			Name: "Updated recipient", Enabled: true, Scope: grant.Share.Scope, Fields: grant.Share.Fields,
			SourceIDs: grant.Share.SourceIDs, Revision: grant.Share.Revision,
			RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads,
		})
		result <- err
	}()
	waitForStorageLock(t, ctx, db, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND locktype='advisory' AND classid=1768843109 AND objid=3 AND NOT granted)`)
	// A reset holding publication must still be able to take the recipient.
	// NOWAIT makes an incorrect row-before-publication order fail immediately.
	var revision int64
	if err := publication.QueryRow(ctx, `SELECT revision FROM ingest.shares WHERE id=$1 FOR UPDATE NOWAIT`, grant.Share.ID).Scan(&revision); err != nil {
		t.Fatal("policy edit locked the recipient before publication:", err)
	}
	if _, err := publication.Exec(ctx, `UPDATE ingest.shares SET revision=revision+1 WHERE id=$1`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	if err := publication.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, model.ErrConflict) {
		t.Fatalf("policy edit crossed the reset revision boundary: %v", err)
	}
}

func TestSourceResetAndAnAdmittedCatalogExportCompleteWithoutALockCycle(t *testing.T) {
	parent, db := sharingDatabase(t)
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	publishSharingRow(t, ctx, db, "native", "one", `{"title":"Before reset"}`, nil)
	grant := sharingGrant(t, ctx, db)
	download, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = download.Close() }()
	metadata, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(metadata)
	if _, err := metadata.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	settings, _ := db.StorageOverview(ctx)
	reset := make(chan error, 1)
	go func() { reset <- db.ResetSourceData(ctx, settings.Settings.Revision) }()
	// Reset has cleared the snapshot and is waiting for the metadata fence.
	waitForStorageLock(t, ctx, db, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE relation='ingest.catalog_journal'::regclass AND mode='AccessExclusiveLock' AND granted)`)
	export := make(chan error, 1)
	go func() {
		_, err := download.Page(ctx, CatalogPageRequest{})
		export <- err
	}()
	waitForStorageLock(t, ctx, db, `SELECT count(*)>=2 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'`)
	if err := metadata.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-reset; err != nil {
		t.Fatal("reset failed while an old export was waiting:", err)
	}
	if err := <-export; !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("admitted export crossed the reset boundary: %v", err)
	}
	fresh := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if fresh.Mode != "full" || len(fresh.Items) != 0 {
		t.Fatalf("reset catalog was not empty and authoritative: %+v", fresh)
	}
}

func TestSourceResetWaitsForHistoryCleanupBeforeTakingPublication(t *testing.T) {
	parent, db := sharingDatabase(t)
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	retention, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(retention)
	// Model the cleanup's early table locks before it later takes publication.
	if _, err := retention.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4);
 LOCK TABLE ingest.raw_records IN ROW EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	settings, _ := db.StorageOverview(ctx)
	result := make(chan error, 1)
	go func() { result <- db.ResetSourceData(ctx, settings.Settings.Revision) }()
	waitForStorageLock(t, ctx, db, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND locktype='advisory' AND classid=1768843109 AND objid=4 AND NOT granted)`)
	var published bool
	if err := retention.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(1768843109,3)`).Scan(&published); err != nil || !published {
		t.Fatalf("reset blocks the history cleanup's publication boundary: %v %v", published, err)
	}
	if err := retention.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal("reset did not complete after history cleanup:", err)
	}
}
