package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStorageBreakdownExposesAllocatedPartsAndRetainedCancelledImports(t *testing.T) {
	ctx, db := sharingDatabase(t)
	p := model.Provider{ID: "synthetic", Name: "Display name", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, p, model.ModeFull)
	items := make([]model.Record, 10)
	for i := range items {
		items[i] = model.Record{SourceID: fmt.Sprint(i), Fields: map[string]any{"title": "never expose this title"}}
	}
	run, err := db.SavePage(ctx, run, model.Page{Items: items}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusCancelled)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.runs SET created_at=now()-interval '10 days',finished_at=now()-interval '9 days' WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "ANALYZE ingest.runs; ANALYZE ingest.staged_torrents; ANALYZE ingest.run_record_identities;"); err != nil {
		t.Fatal(err)
	}
	report, err := db.MeasureHealth(ctx, model.HealthDisk{})
	if err != nil || !report.ImportStorageAvailable || report.RetainedImportCount != 1 || report.CancelledImportCount != 1 || len(report.RetainedImports) != 1 {
		t.Fatalf("retained cancellation hidden: %+v %v", report, err)
	}
	item := report.RetainedImports[0]
	if item.RunID != run.ID || item.ProviderName != "Display name" || item.Bytes == nil || *item.Bytes <= 0 {
		t.Fatalf("missing import identity or allocation: %+v", item)
	}
	for _, category := range report.Database.Categories {
		var total, data, indexes int64
		for _, part := range category.Details {
			total += part.Bytes
			data += part.TableBytes
			indexes += part.IndexBytes
		}
		if total != category.Bytes || data != category.TableBytes || indexes != category.IndexBytes {
			t.Fatalf("allocation details do not reconcile: %+v", category)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "never expose") || strings.Contains(string(encoded), "info_hash") {
		t.Fatalf("storage report leaked payloads: %s %v", encoded, err)
	}
	var staged int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || staged != 10 {
		t.Fatal("measurement mutated preparation", staged, err)
	}
}

func TestRetainedImportSizesStayUnknownWithoutStatistics(t *testing.T) {
	ctx, db := sharingDatabase(t)
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "synthetic", Adapter: "http_json"}, model.ModeFull)
	defer func() { _, _ = db.FinishRun(ctx, run.ID, model.StatusCancelled, "", "") }()
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("one")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := db.MeasureHealth(ctx, model.HealthDisk{})
	if err != nil || !report.ImportStorageAvailable || len(report.RetainedImports) != 1 || report.RetainedImports[0].Bytes != nil {
		t.Fatalf("unknown attribution became zero: %+v %v", report, err)
	}
}
