package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestSeederRetirementPreservesIdentificationProgressAndHistory(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.migrateThrough(ctx, 40); err != nil {
		t.Fatal(err)
	}
	const data = `{"title":"Film fixture","size":9007199254740993,"info_hash":"00112233445566778899aabbccddeeff00112233","seeders":12,"attributes":{"seeders":["12"],"imdbid":["tt123"]},"metadata":{"nested":[{"seeders":7,"edition":"original"},null,"seeders"]}}`
	for _, source := range []string{"fresh", "stale", "queued", "revision-stale"} {
		publishSharingRow(t, ctx, db, "fixture", source, data, nil)
	}
	if _, err = db.pool.Exec(ctx, `
 INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('movie','123','tt123',ARRAY[2026]);
 INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT provider_id,source_id,CASE WHEN source_id='stale' THEN repeat('0',32) ELSE md5(fields::text) END,
 (SELECT id FROM ingest.aio_works WHERE tmdb_id='123'),CASE WHEN source_id='revision-stale' THEN 0 ELSE 1 END,'matched','verified'
 FROM ingest.torrents;
 DELETE FROM ingest.aio_match_queue;
 INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES('fixture','queued');
 INSERT INTO ingest.saved_views(id,name,filters) VALUES('legacy-view','Exact size','{"q":"fixture","min_size":"9007199254740993","min_seeders":"1","max_seeders":"100","sort":"seeders_desc"}');
 CREATE TRIGGER test_disabled_guard BEFORE UPDATE ON ingest.torrents FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_publication_lock();
 ALTER TABLE ingest.torrents DISABLE TRIGGER test_disabled_guard;
 CREATE TRIGGER test_always_guard BEFORE UPDATE ON ingest.raw_records FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_publication_lock();
 ALTER TABLE ingest.raw_records ENABLE ALWAYS TRIGGER test_always_guard;
 `); err != nil {
		t.Fatal(err)
	}
	mixed, err := db.CreateShare(ctx, model.ShareInput{Name: "Mixed share", Scope: "all", Fields: []string{"title"}})
	if err != nil {
		t.Fatal(err)
	}
	only, err := db.CreateShare(ctx, model.ShareInput{Name: "Count-only share", Scope: "all", Fields: []string{"title"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.shares SET enabled=TRUE,fields=CASE WHEN id=$1 THEN ARRAY['title','seeders'] ELSE ARRAY['seeders'] END WHERE id IN ($1,$2)`, mixed.Share.ID, only.Share.ID); err != nil {
		t.Fatal(err)
	}
	// Historical fixtures cannot be created with a writer for a newer schema.
	run := model.Run{ID: "history-fixture-run", ProviderID: "history-fixture"}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,revision,mode,status,config) VALUES($1,$2,$2,'','full','running',jsonb_build_object('id',$2::text))`, run.ID, run.ProviderID); err != nil {
		t.Fatal(err)
	}
	var pageID int64
	if err = db.pool.QueryRow(ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type) VALUES($1,$2,1,'','application/json') RETURNING id`, run.ID, run.ProviderID).Scan(&pageID); err != nil {
		t.Fatal(err)
	}
	var rawID int64
	if err = db.pool.QueryRow(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields) VALUES($1,$2,'sample',$3,1,$4,'application/json',$5::jsonb) RETURNING id`, run.ID, run.ProviderID, pageID, []byte(data), data).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.staged_torrents(run_id,provider_id,source_id,fields,raw_id) VALUES($1,$2,'sample',$3::jsonb,$4)`, run.ID, run.ProviderID, data, rawID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.runs SET cursor='{"page":12}',pages=11,records=4 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	var journalBefore, historyBefore int64
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.catalog_journal),(SELECT count(*) FROM ingest.torrent_history)`).Scan(&journalBefore, &historyBefore); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []string{
		`SELECT count(*) FROM ingest.torrents WHERE fields @? '$.**.seeders'`,
		`SELECT count(*) FROM ingest.staged_torrents WHERE fields @? '$.**.seeders'`,
		`SELECT count(*) FROM ingest.raw_records WHERE fields @? '$.**.seeders'`,
		`SELECT count(*) FROM ingest.catalog_journal WHERE fields @? '$.**.seeders'`,
		`SELECT count(*) FROM ingest.torrent_history_baselines WHERE fields @? '$.**.seeders'`,
		`SELECT count(*) FROM ingest.torrent_history WHERE before_fields @? '$.**.seeders' OR after_fields @? '$.**.seeders'`,
		`SELECT count(*) FROM information_schema.columns WHERE table_schema='ingest' AND column_name='search_seeders'`,
	} {
		var count int64
		if err = db.pool.QueryRow(ctx, check).Scan(&count); err != nil || count != 0 {
			t.Fatalf("retired data remains: %d %v", count, err)
		}
	}
	var journalAfter, historyAfter, queue int64
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.catalog_journal),(SELECT count(*) FROM ingest.torrent_history),(SELECT count(*) FROM ingest.aio_match_queue)`).Scan(&journalAfter, &historyAfter, &queue); err != nil || journalAfter != journalBefore || historyAfter != historyBefore || queue != 1 {
		t.Fatalf("cleanup created publication/identification work: %d %d %d: %v", journalAfter, historyAfter, queue, err)
	}
	diagnostics, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range diagnostics.Items {
		want := "pending"
		if item.SourceID == "fresh" {
			want = "matched"
		}
		if item.Status != want {
			t.Fatalf("identification freshness changed: %+v", item)
		}
	}
	var fields, raw []byte
	var fingerprint string
	if err = db.pool.QueryRow(ctx, `SELECT fields FROM ingest.torrents WHERE source_id='fresh'`).Scan(&fields); err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	decoder := json.NewDecoder(bytes.NewReader(fields))
	decoder.UseNumber()
	if err = decoder.Decode(&actual); err != nil {
		t.Fatal(err)
	}
	if actual["size"] != json.Number("9007199254740993") || actual["title"] != "Film fixture" {
		t.Fatalf("durable data changed: %v", actual)
	}
	if err = db.pool.QueryRow(ctx, `SELECT fingerprint FROM ingest.aio_matches WHERE source_id='stale'`).Scan(&fingerprint); err != nil || fingerprint != "00000000000000000000000000000000" {
		t.Fatalf("stale match was refreshed: %q %v", fingerprint, err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT raw FROM ingest.raw_records WHERE run_id=$1`, run.ID).Scan(&raw); err != nil || string(raw) != data {
		t.Fatalf("original archive changed: %v", err)
	}
	preserved, err := db.GetRun(ctx, run.ID)
	if err != nil || string(preserved.Cursor) != `{"page": 12}` || preserved.Pages != 11 || preserved.Records != 4 {
		t.Fatalf("progress changed: %+v %v", preserved, err)
	}
	views, err := db.SavedViews(ctx)
	if err != nil || len(views) != 1 || views[0].Filters.MinSize != "9007199254740993" || views[0].Filters.Sort != "recent" || views[0].Revision != 2 {
		t.Fatalf("saved filters changed incorrectly: %+v %v", views, err)
	}
	remaining, err := db.Share(ctx, mixed.Share.ID)
	if err != nil || !remaining.Enabled || !reflect.DeepEqual(remaining.Fields, []string{"title"}) {
		t.Fatalf("remaining permissions lost: %+v %v", remaining, err)
	}
	empty, err := db.Share(ctx, only.Share.ID)
	if err != nil || empty.Enabled || len(empty.Fields) != 0 {
		t.Fatalf("empty share gained permissions: %+v %v", empty, err)
	}
	var disabled, always string
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT tgenabled::text FROM pg_trigger WHERE tgname='test_disabled_guard'),(SELECT tgenabled::text FROM pg_trigger WHERE tgname='test_always_guard')`).Scan(&disabled, &always); err != nil || disabled != "D" || always != "A" {
		t.Fatalf("trigger modes changed: %s %s %v", disabled, always, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=fields||'{"seeders":999}' WHERE source_id='fresh'`); err != nil {
		t.Fatal(err)
	}
	var latest int64
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal`).Scan(&latest); err != nil || latest != journalAfter {
		t.Fatalf("live count changes still create history: %d %v", latest, err)
	}
}
