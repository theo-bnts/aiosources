package store

import (
	"encoding/json"
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"strings"
	"testing"
)

func TestIdentificationDiagnosticsExcludeStaleDecisionsAndProtectFields(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, id := range []string{"matched", "ambiguous", "unmatched", "pending", "changed", "revision", "queued", "deleted-work"} {
		publishSharingRow(t, ctx, db, "one", id, `{"title":"Release `+id+`","download_url":"https://source.invalid/synthetic-credential","arbitrary":"private-field"}`, nil)
	}
	publishSharingRow(t, ctx, db, "two", "pending", `{"title":"Other %_ release"}`, nil)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years,revision) VALUES('movie','1','tt1234567',ARRAY[2021],2);
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT provider_id,source_id,CASE WHEN source_id='changed' THEN 'old' ELSE md5(fields::text) END,
 CASE WHEN source_id IN ('matched','revision') THEN (SELECT id FROM ingest.aio_works) ELSE NULL END,
 CASE WHEN source_id='matched' THEN 2 ELSE 1 END,
 CASE WHEN source_id IN ('ambiguous','queued') THEN 'ambiguous' WHEN source_id='unmatched' THEN 'unmatched' ELSE 'matched' END,
 'stored explanation' FROM ingest.torrents WHERE provider_id='one' AND source_id<>'pending';
DELETE FROM ingest.aio_match_queue WHERE source_id<>'queued'`); err != nil {
		t.Fatal(err)
	}
	status, err := db.AIOStatus(ctx)
	if err != nil || status.Matched != 1 || status.Ambiguous != 1 || status.Unmatched != 1 || status.MatchingPending != 6 {
		t.Fatalf("current counts: %+v %v", status, err)
	}
	counts, err := db.SourceTorrentStats(ctx, []string{"one", "two", "empty", "one"})
	if err != nil || len(counts.Items) != 3 {
		t.Fatalf("source counts: %+v %v", counts, err)
	}
	if counts.Totals.Imported != 9 || counts.Totals.AwaitingImport != 0 {
		t.Fatalf("global totals included pending imports or dropped published records: %+v", counts.Totals)
	}
	want := []model.SourceTorrentStats{
		{ProviderID: "empty"},
		{ProviderID: "one", Imported: 8, Matched: 1, Ambiguous: 1, Unmatched: 1, Pending: 5},
		{ProviderID: "two", Imported: 1, Pending: 1},
	}
	for i, item := range counts.Items {
		if item != want[i] || item.Imported != item.Matched+item.Ambiguous+item.Unmatched+item.Pending {
			t.Fatalf("source totals differ from current decisions: %+v, want %+v", counts, want)
		}
	}
	all, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Limit: 200})
	if err != nil || all.Total != 9 || len(all.Items) != 9 {
		t.Fatalf("all diagnostics: %+v %v", all, err)
	}
	for _, item := range all.Items {
		if item.Status == "pending" && (item.IMDb != "" || item.Reason != "awaiting identification" || item.UpdatedAt != nil) {
			t.Fatalf("obsolete decision exposed: %+v", item)
		}
		if !ValidOccurrenceID(item.OccurrenceID) {
			t.Fatalf("missing occurrence identity: %+v", item)
		}
	}
	encoded, err := json.Marshal(all)
	if err != nil || strings.Contains(string(encoded), "synthetic-credential") || strings.Contains(string(encoded), "private-field") || strings.Contains(string(encoded), "download_url") {
		t.Fatal("diagnostics exposed private fields", err)
	}
	for name, total := range map[string]int64{"matched": 1, "ambiguous": 1, "unmatched": 1, "pending": 6} {
		rows, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: name, Limit: 1})
		if err != nil || rows.Total != total || len(rows.Items) != 1 || rows.Items[0].Status != name {
			t.Fatalf("filter %s: %+v %v", name, rows, err)
		}
	}
	filtered, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{ProviderID: "two", Status: "pending", Query: "%_"})
	if err != nil || filtered.Total != 1 || filtered.Items[0].ProviderID != "two" {
		t.Fatalf("literal provider search: %+v %v", filtered, err)
	}
	page, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "pending", Limit: 2, Offset: 5})
	if err != nil || page.Total != 6 || len(page.Items) != 1 {
		t.Fatalf("pagination: %+v %v", page, err)
	}
	if _, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{Status: "unknown"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid status accepted: %v", err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES('one','matched')`); err != nil {
		t.Fatal(err)
	}
	status, err = db.AIOStatus(ctx)
	if err != nil || status.Matched != 0 || status.MatchingPending != 7 {
		t.Fatalf("queued result remains identified: %+v %v", status, err)
	}
	counts, err = db.SourceTorrentStats(ctx, []string{"one"})
	if err != nil || len(counts.Items) != 1 || counts.Items[0].Matched != 0 || counts.Items[0].Pending != 6 || counts.Items[0].Imported != 8 {
		t.Fatalf("queued result remains identified in source counts: %+v %v", counts, err)
	}
	if counts.Totals.Imported != 9 {
		t.Fatalf("filtering source cards changed the global denominator: %+v", counts.Totals)
	}
}
