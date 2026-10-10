package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRawFragmentSearchUsesIndexBeforeResolvingPayloads(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "bulk", Config: model.Provider{ID: "bulk"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	var pageID int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type) VALUES($1,'bulk',0,''::bytea,'application/json') RETURNING id`, run.ID).Scan(&pageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields,payload_retained)
SELECT $1,'bulk',n::text,$2,0,''::bytea,'application/json',jsonb_build_object('title','Ordinary.Release.'||n,'size',9007199254740993),false FROM generate_series(1,20000) n`, run.ID, pageID); err != nil {
		t.Fatal(err)
	}
	var keeper int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields)
VALUES($1,'bulk','marker',$2,0,''::bytea,'application/json','{"title":"Rare.Astronomical.%_Sample","size":9007199254740993}') RETURNING id`, run.ID, pageID).Scan(&keeper); err != nil {
		t.Fatal(err)
	}
	var reference int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields,payload_id)
VALUES($1,'reference','different-identity',$2,0,''::bytea,'application/json','{}',$3) RETURNING id`, run.ID, pageID, keeper).Scan(&reference); err != nil {
		t.Fatal(err)
	}
	// Match the completed-upgrade index (and normal vacuum maintenance),
	// rather than measuring an unflushed bulk-load pending list.
	if _, err := db.pool.Exec(ctx, "SELECT pg_catalog.gin_clean_pending_list('ingest.raw_records_fragments'); ANALYZE ingest.raw_records"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query, provider string
		count           int64
	}{
		{"RONOMIC", "", 2}, {"%_", "", 2}, {"different-identity", "reference", 1},
		{"size", "reference", 1}, {"{}", "reference", 0}, {"fields", "", 0},
	} {
		list, err := db.ListRaw(ctx, model.ListOptions{Query: test.query, ProviderID: test.provider, Limit: 1})
		if err != nil || list.Total != test.count || list.Counts.Accepted != test.count {
			t.Fatalf("literal/reference query %q returned incorrect counts: %+v error=%v", test.query, list, err)
		}
		if test.count > 0 && (len(list.Items) != 1 || list.Items[0].Fields["size"] != json.Number("9007199254740993")) {
			t.Fatal("indexed search lost exact canonical fields or page order")
		}
	}
	selection, args := rawSelection(model.ListOptions{Query: "ronomic"})
	var indexed, original string
	if err := db.pool.QueryRow(ctx, "EXPLAIN (ANALYZE,FORMAT JSON) SELECT count(*) FROM ("+selection+") AS selected", args...).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(indexed, `"Index Name": "raw_records_fragments"`) || !strings.Contains(indexed, `"Index Name": "raw_records_payload"`) {
		t.Fatalf("selective search still scanned all fields/references: %s", indexed)
	}
	if err := db.pool.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) SELECT count(*)`+rawSource+` WHERE strpos(lower(record.source_id),'ronomic')>0 OR strpos(lower(COALESCE(payload.fields,record.fields)::text),'ronomic')>0 OR strpos(lower(record.error),'ronomic')>0`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	executionTime := func(plan string) float64 {
		var result []struct {
			Time float64 `json:"Execution Time"`
		}
		if err := json.Unmarshal([]byte(plan), &result); err != nil || len(result) != 1 {
			t.Fatal("invalid query plan")
		}
		return result[0].Time
	}
	t.Logf("20,002 observations: original search %.3f ms; indexed search %.3f ms", executionTime(original), executionTime(indexed))
}
