package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestJSONAdapterUpgradePreservesSnapshotsAndRemoteHistory(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 63); err != nil {
		t.Fatal(err)
	}
	for _, catalog := range []bool{false, true} {
		p := model.Provider{ID: "old-native", Adapter: "http_json"}
		if catalog {
			p.ID, p.HTTP.Catalog = "old-remote", true
		}
		if _, err = db.CreateRun(ctx, model.Run{ProviderID: p.ID, Mode: model.ModeFull, Config: p}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.runs SET status='paused',pause_reason='budget',cursor='{"page":23}',pages=22;
INSERT INTO ingest.collection_state(provider_id,initialized_at) VALUES('old-native',now());
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('old-native','one','{"title":"Example release"}');`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r) ORDER BY id)::text FROM ingest.runs r`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, jsonAdapterSchema); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil || snapshot() != before {
		t.Fatal("interrupted upgrade changed existing runs", err)
	}
	for range 2 {
		if err = db.Migrate(ctx); err != nil || snapshot() != before {
			t.Fatal("upgrade rewrote an immutable run or its classification", err)
		}
	}
	for _, catalog := range []bool{false, true} {
		p := model.Provider{ID: "new-native", Adapter: "json"}
		if catalog {
			p.ID, p.HTTP.Catalog = "new-remote", true
		}
		run, err := db.CreateRun(ctx, model.Run{ProviderID: p.ID, Mode: model.ModeFull, Config: p})
		if err != nil || run.RemoteCatalog != catalog {
			t.Fatal("new JSON source classified incorrectly", err)
		}
	}
	for _, kind := range []string{"source", "remote"} {
		page, err := db.ListRuns(ctx, model.ListOptions{SourceKind: kind, Limit: 50})
		if err != nil || page.Total != 2 || len(page.Items) != 2 {
			t.Fatalf("%s history lost old or new JSON sources: %+v %v", kind, page, err)
		}
		for _, run := range page.Items {
			if run.RemoteCatalog != (kind == "remote") {
				t.Fatal("source and remote histories were mixed")
			}
		}
	}
	updated := model.Provider{ID: "old-native", Adapter: "json"}
	run, err := db.EnqueueRun(ctx, model.Run{ProviderID: updated.ID, Config: updated, Mode: model.ModeIncremental})
	if err != nil || run.Mode != model.ModeIncremental || run.InitialFull {
		t.Fatal("adapter rename triggered another initial Full import", err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.torrents WHERE provider_id='old-native'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("adapter rename changed already imported data", err)
	}
}
