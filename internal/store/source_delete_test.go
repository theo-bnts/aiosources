package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDeleteSourceDataPurgesOwnedDataWithoutBroadeningAccess(t *testing.T) {
	ctx, db := sharingDatabase(t)
	all := sharingGrant(t, ctx, db)
	only := sharingGrant(t, ctx, db, "removed")
	mixed := sharingGrant(t, ctx, db, "removed", "retained")
	private, err := db.CreateShare(ctx, model.ShareInput{Name: "Private source selection", Format: "stremio", Enabled: true, Scope: "selected", SourceIDs: []string{"removed"}, Fields: CatalogFields(), StreamPreferences: &model.StreamPreferences{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"removed", "retained"} {
		provider := model.Provider{ID: id, Adapter: "http_json"}
		run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
		var err error
		run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{{SourceID: "one", Fields: map[string]any{"title": "Synthetic release"}, CoverageScopes: []string{"chosen"}}}, Done: true}, "published")
		if err != nil {
			t.Fatal(err)
		}
		mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
		var occurrence string
		if err = db.pool.QueryRow(ctx, `SELECT occurrence_id FROM ingest.torrents WHERE provider_id=$1`, id).Scan(&occurrence); err != nil {
			t.Fatal(err)
		}
		if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,phase,finished_at) VALUES($1,'download','torbox','manual',1,'succeeded','waiting',now())`, id+"-download"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) VALUES($1,'waiting',0,0)`, id+"-download"); err != nil {
			t.Fatal(err)
		}
		if err = db.SaveOperationDownload(ctx, id+"-download", model.OperationDownload{WorkID: 1, Input: model.CatalogDownloadInput{OccurrenceID: occurrence, Service: "torbox"}, Account: "synthetic-account"}); err != nil {
			t.Fatal(err)
		}
	}
	provider := model.Provider{ID: "removed", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{{SourceID: "pending", Fields: map[string]any{"title": "Pending synthetic release"}, CoverageScopes: []string{"chosen"}}}}, "staging")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusFailed)
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,years) VALUES('movie','991','{2020}');
 INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,status,reason) SELECT provider_id,source_id,md5(fields::text),(SELECT id FROM ingest.aio_works WHERE tmdb_id='991'),'matched','fixture' FROM ingest.torrents;
 INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id) SELECT provider_id,source_id,md5(fields::text),md5(fields::text),'movie','991' FROM ingest.torrents;`); err != nil {
		t.Fatal(err)
	}
	if err = db.PutSecret(ctx, "shared-announce", []byte("encrypted-synthetic-value")); err != nil {
		t.Fatal(err)
	}
	config := DefaultAIOConfig()
	config.Trackers = map[string]model.AIOTracker{"removed": {Private: true, AnnounceSecret: "shared-announce"}, "retained": {Private: true, AnnounceSecret: "shared-announce"}}
	if err = db.SetAIOConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox',repeat('a',40),true,now(),'[]')`); err != nil {
		t.Fatal(err)
	}
	checkpoint := sharingPage(t, ctx, db, all, CatalogPageRequest{Limit: 100}).Checkpoint
	cursor := sharingPage(t, ctx, db, all, CatalogPageRequest{Limit: 1}).NextCursor
	before := mirrorRows(t, ctx, db, "retained")
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.operation_runs SET status='running',finished_at=NULL WHERE id='removed-download'`); err != nil {
		t.Fatal(err)
	}
	if err = db.DeleteSourceData(ctx, "removed", func() error { t.Error("active download allowed deletion"); return nil }); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("active download not protected: %v", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.operation_runs SET status='succeeded',finished_at=now() WHERE id='removed-download'`); err != nil {
		t.Fatal(err)
	}
	var otherJournal int64
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal WHERE provider_id='retained'`).Scan(&otherJournal); err != nil {
		t.Fatal(err)
	}
	removed := false
	if err = db.DeleteSourceData(ctx, "removed", func() error { removed = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("definition removal was not coordinated")
	}
	for _, table := range []string{"operation_runs", "operation_downloads"} {
		column := "id"
		if table == "operation_downloads" {
			column = "run_id"
		}
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE `+column+`='removed-download'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("download residue in %s: %d %v", table, count, err)
		}
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE `+column+`='retained-download'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("other download changed in %s: %d %v", table, count, err)
		}
	}
	for _, table := range []string{"runs", "torrents", "staged_torrents", "raw_records", "pages", "catalog_journal", "catalog_baselines", "torrent_history", "torrent_history_baselines", "aio_matches", "aio_match_queue", "aio_match_reviews", "activity_logs", "schedule_clocks", "collection_state", "provider_requests", "remote_checkpoints", "legacy_imports"} {
		var count int
		if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.`+table+` WHERE provider_id=$1`, "removed").Scan(&count); err != nil || count != 0 {
			t.Fatalf("remaining %s: %d %v", table, count, err)
		}
	}
	if got := mirrorRows(t, ctx, db, "retained"); !reflect.DeepEqual(before, got) {
		t.Fatal("other source publication changed")
	}
	var afterJournal int64
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal WHERE provider_id='retained'`).Scan(&afterJournal); err != nil || afterJournal != otherJournal {
		t.Fatalf("other source journal changed: %d %d %v", otherJournal, afterJournal, err)
	}
	if value, err := db.Secret(ctx, "shared-announce"); err != nil || string(value) != "encrypted-synthetic-value" {
		t.Fatalf("secret changed: %v", err)
	}
	config, err = db.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config.Trackers["removed"]; ok || len(config.Trackers) != 1 || config.Revision != 3 {
		t.Fatalf("tracker configuration not cleaned: %+v", config)
	}
	if _, err = db.Share(ctx, only.Share.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("source-only access remains: %v", err)
	}
	if _, err = db.Share(ctx, private.Share.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("private selection was broadened: %v", err)
	}
	var works int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works`).Scan(&works); err != nil || works != 1 {
		t.Fatalf("shared metadata changed: %d %v", works, err)
	}
	reduced, err := db.Share(ctx, mixed.Share.ID)
	if err != nil || !reflect.DeepEqual(reduced.SourceIDs, []string{"retained"}) || reduced.Scope != "selected" {
		t.Fatalf("access broadened: %+v %v", reduced, err)
	}
	if _, err = db.CatalogPage(ctx, all.Share.ID, all.Password, CatalogPageRequest{Cursor: cursor}); !errors.Is(err, ErrCatalogExpired) {
		t.Fatalf("old snapshot still usable: %v", err)
	}
	fresh := sharingPage(t, ctx, db, all, CatalogPageRequest{Checkpoint: checkpoint})
	if fresh.Mode != "full" || len(fresh.Items) != 1 || fresh.Items[0].Origin.ProviderID != "retained" {
		t.Fatalf("peer does not receive authoritative replacement: %+v", fresh)
	}
	var cached int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_shared_availability`).Scan(&cached); err != nil || cached != 1 {
		t.Fatalf("shared cache changed: %d %v", cached, err)
	}
}

func TestDeleteSourceDataRejectsActiveWorkAndRollsBackRemovalFailure(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := model.Provider{ID: "guarded", Adapter: "http_json"}
	queued, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	remove := func() error { called = true; return nil }
	if err = db.DeleteSourceData(ctx, provider.ID, remove); !errors.Is(err, model.ErrBusy) || called {
		t.Fatalf("active import removed: %v", err)
	}
	if _, err = db.RequestCancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic definition removal failure")
	if err = db.DeleteSourceData(ctx, provider.ID, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("removal error lost: %v", err)
	}
	if _, err = db.GetRun(ctx, queued.ID); err != nil {
		t.Fatalf("data removed despite rollback: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = db.DeleteSourceData(cancelled, provider.ID, remove); err == nil || called {
		t.Fatal("cancelled deletion continued")
	}
}
