package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStremioHistoryFiltersDetailsAndRetention(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, outcome := range []string{"results", "empty", "failed", "invalid", "cancelled"} {
		item := model.StremioRequest{CreatedAt: time.Now(), MediaType: "movie", Resource: "imdb:tt1234567", Outcome: outcome, DurationMS: 25, Results: []model.StremioResult{}}
		if outcome == "results" {
			item.Matched = 1
			item.Eligible = 1
			item.Returned = 1
			item.Results = append(item.Results, model.StremioResult{Provider: "one", SourceID: "item", Title: "Example Film", Filename: "example.mp4", Size: 100, Service: "alldebrid", Cache: "cached"})
		}
		if err := db.RecordStremioRequest(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.StremioStatistics(ctx)
	if err != nil || stats.Requests != 5 || stats.Empty != 1 || stats.Failed != 3 || stats.Results != 1 || stats.AverageMS != 25 {
		t.Fatalf("incorrect statistics: %+v %v", stats, err)
	}
	list, err := db.ListStremioRequests(ctx, model.ListOptions{Query: "1234567", Status: "results", Limit: 1})
	if err != nil || list.Total != 1 || len(list.Items) != 1 || list.Items[0].Results != nil {
		t.Fatalf("list loads full result bodies: %+v %v", list, err)
	}
	detail, err := db.StremioRequest(ctx, list.Items[0].ID)
	if err != nil || len(detail.Results) != 1 || detail.Results[0].Service != "alldebrid" {
		t.Fatalf("missing returned results: %+v %v", detail, err)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "url") || strings.Contains(string(encoded), "token") {
		t.Fatal("history serializes forbidden fields")
	}
	if _, err = db.ListStremioRequests(ctx, model.ListOptions{Status: "unknown"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("invalid history filter accepted")
	}
	if err = db.RecordStremioRequest(ctx, model.StremioRequest{MediaType: "movie", Resource: "https://synthetic.invalid/private-value", Outcome: "empty"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("credential-bearing resource accepted")
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.stremio_requests SET created_at=now()-interval '31 days';
 INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results)
 SELECT now(),'movie','imdb:tt1234567',1,'empty',0,0,0,0,0,0,0,false,'[]' FROM generate_series(1,10001)`); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordStremioRequest(ctx, model.StremioRequest{MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "empty"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.stremio_requests`).Scan(&count); err != nil || count != 10000 {
		t.Fatalf("history not bounded: %d %v", count, err)
	}
	large := model.StremioRequest{MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "results", Returned: 1001}
	for range 1001 {
		large.Results = append(large.Results, model.StremioResult{Service: "direct", Cache: "direct", Title: "Example"})
	}
	if err = db.RecordStremioRequest(ctx, large); err != nil {
		t.Fatal(err)
	}
	last, _ := db.ListStremioRequests(ctx, model.ListOptions{Limit: 1})
	detail, err = db.StremioRequest(ctx, last.Items[0].ID)
	if err != nil || !detail.Truncated || detail.Returned != 1001 || len(detail.Results) != 1000 {
		t.Fatal("result cap lost the returned count", err)
	}
}
