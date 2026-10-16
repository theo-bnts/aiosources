package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestCatalogSearchUpgradePreservesExistingCatalogAndIsAtomic(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 52); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','101','tt1234567',ARRAY[2026]);
INSERT INTO ingest.aio_titles(work_id,title,normalized) SELECT id,'Example Film','example film' FROM ingest.aio_works;
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.Film.2026"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,fields_fingerprint,(SELECT id FROM ingest.aio_works),1,'matched','fixture' FROM ingest.torrents;
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,finished_at) VALUES('previous','cache','torbox','manual',1,'succeeded',now());`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object('torrents',(SELECT jsonb_agg(to_jsonb(t)) FROM ingest.torrents t),'matches',(SELECT jsonb_agg(to_jsonb(m)) FROM ingest.aio_matches m),'operations',(SELECT jsonb_agg(to_jsonb(r)) FROM ingest.operation_runs r),'old_migrations',(SELECT jsonb_agg(to_jsonb(v) ORDER BY version) FROM ingest.schema_migrations v WHERE version<=52))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, catalogSearchSchema); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='ingest' AND table_name='aio_works' AND column_name='display_title')`).Scan(&exists); err != nil || exists || snapshot() != before {
		t.Fatal("interrupted migration modified catalog", err)
	}
	for range 2 {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("upgrade changed torrents, matching, history or previous migrations")
		}
		items, err := db.CatalogTitles(ctx, model.ListOptions{Query: "Example"}, "example", model.CatalogSearchFilters{})
		if err != nil || items.Total != 1 || items.Items[0].Title != "Example Film" {
			t.Fatalf("existing metadata not searchable after upgrade: %+v %v", items, err)
		}
	}
}
