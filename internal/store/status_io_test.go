package store

import (
	"encoding/json"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAIOStatusAggregatesCurrentDecisionsWithoutRepeatedSpooling(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "1", Titles: []string{"Example Film"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
SELECT 'example',n::text,jsonb_build_object('title','Example Film','size',n) FROM generate_series(1,20000) n;
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT provider_id,source_id,md5(fields::text),CASE WHEN source_id::int%4=1 THEN 1 END,1,
CASE source_id::int%4 WHEN 2 THEN 'ambiguous' WHEN 3 THEN 'unmatched' ELSE 'matched' END,'synthetic test' FROM ingest.torrents;
DELETE FROM ingest.aio_match_queue WHERE source_id::int%4<>0;
ANALYZE ingest.torrents; ANALYZE ingest.aio_matches; ANALYZE ingest.aio_match_queue;`)
	if err != nil {
		t.Fatal(err)
	}
	status, err := db.AIOStatus(ctx)
	if err != nil || status.Works != 1 || status.Matched != 5000 || status.Ambiguous != 5000 || status.Unmatched != 5000 || status.MatchingPending != 5000 {
		t.Fatalf("current status: %+v, %v", status, err)
	}
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SET work_mem='64kB'`); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, `RESET work_mem`)
	var plan []byte
	if err = conn.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) `+aioStatusQuery).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var roots []struct{ Plan map[string]any }
	if err = json.Unmarshal(plan, &roots); err != nil {
		t.Fatal(err)
	}
	scans := 0
	var inspect func(map[string]any)
	inspect = func(node map[string]any) {
		if node["Node Type"] == "CTE Scan" && node["CTE Name"] == "decisions" {
			scans++
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				inspect(child.(map[string]any))
			}
		}
	}
	for _, root := range roots {
		inspect(root.Plan)
	}
	t.Logf("decision spool scans=%d, temporary reads=%v writes=%v", scans, roots[0].Plan["Temp Read Blocks"], roots[0].Plan["Temp Written Blocks"])
	if scans != 0 {
		t.Fatalf("status repeatedly spooled all source decisions %d times", scans)
	}
}
