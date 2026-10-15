package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSourceStorageEstimatesIncludePreparedDataWithoutReadingSourceRows(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
 SELECT 'published',n::text,jsonb_build_object('title','Synthetic private title') FROM generate_series(1,200) n`)
	if err != nil {
		t.Fatal(err)
	}
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "prepared", Adapter: "http_json"}, model.ModeFull)
	items := []model.Record{}
	for i := 0; i < 100; i++ {
		items = append(items, model.Record{SourceID: strings.Repeat("x", i+1), Fields: map[string]any{"title": "Synthetic prepared title"}})
	}
	run, err = db.SavePage(ctx, run, model.Page{Items: items}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusCancelled)
	if _, err = db.pool.Exec(ctx, `ANALYZE ingest.torrents; ANALYZE ingest.staged_torrents; ANALYZE ingest.runs;
 ANALYZE ingest.run_record_identities; ANALYZE ingest.run_scope_memberships; ANALYZE ingest.run_progress_identities`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	readCounts := func() []int64 {
		t.Helper()
		var counts []int64
		if err := tx.QueryRow(ctx, `SELECT ARRAY[pg_stat_get_xact_tuples_returned('ingest.torrents'::regclass),
 pg_stat_get_xact_tuples_returned('ingest.staged_torrents'::regclass),pg_stat_get_xact_tuples_returned('ingest.raw_records'::regclass)]`).Scan(&counts); err != nil {
			t.Fatal(err)
		}
		return counts
	}
	before := readCounts()
	estimates, err := sourceStorageEstimates(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	after := readCounts()
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("storage estimates read source rows: %v -> %v", before, after)
		}
	}
	bySource := map[string]int64{}
	for _, item := range estimates {
		bySource[item.ProviderID] = item.Bytes
	}
	if bySource["published"] <= 0 || bySource["prepared"] <= 0 {
		t.Fatalf("published or retained prepared data omitted: %+v", estimates)
	}
	encoded, err := json.Marshal(estimates)
	if err != nil || strings.Contains(string(encoded), "Synthetic") || strings.Contains(string(encoded), "title") {
		t.Fatalf("estimate exposed record data: %s, %v", encoded, err)
	}
}
