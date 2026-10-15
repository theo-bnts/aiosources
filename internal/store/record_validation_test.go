package store

import (
	"encoding/json"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCompactPageValidationFallsBackWithoutAdvancingIncompleteFull(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, _, db := activityDatabase(t)
			run := mirrorClaim(t, ctx, db, model.Provider{ID: "synthetic", Adapter: "http_json"}, mode)
			defer func() { _, _ = db.FinishRun(ctx, run.ID, model.StatusCancelled, "", "") }()
			before := json.RawMessage(`{"page":1}`)
			var err error
			run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("first")}, Next: before}, "")
			if err != nil {
				t.Fatal(err)
			}
			before = append(json.RawMessage{}, run.Cursor...)
			items := make([]model.Record, 100)
			for i := range items {
				items[i] = coverageRecord("valid")
			}
			items[96] = coverageRecord("nul-title")
			items[96].Fields["title"] = "not\x00representable"
			items[97] = coverageRecord("overflow")
			items[97].Fields["size"] = json.Number("1e1000000")
			items[98] = coverageRecord("nul\x00identity")
			items[99] = coverageRecord("nul-type")
			items[99].ContentType = "application/\x00json"
			run, err = db.SavePage(ctx, run, model.Page{Items: items, Next: json.RawMessage(`{"page":2}`), Done: true}, "")
			if err != nil || run.Errors != 4 {
				t.Fatalf("invalid records lost diagnostics: %+v %v", run, err)
			}
			observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || observations.Total != 4 {
				t.Fatalf("compact validation archived successes or lost errors: %+v %v", observations, err)
			}
			for _, observation := range observations.Items {
				if observation.Error == "" || len(observation.Fields) != 0 {
					t.Fatalf("invalid observation: %+v", observation)
				}
			}
			if mode == model.ModeFull {
				if string(run.Cursor) != string(before) || run.TraversalDone || run.DistinctRecords != 1 || run.Pages != 1 {
					t.Fatalf("partial Full page advanced its checkpoint: %+v", run)
				}
				var staged int
				if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || staged != 1 {
					t.Fatalf("incomplete Full published preparation: %d %v", staged, err)
				}
			} else {
				rows := mirrorRows(t, ctx, db, "synthetic")
				if len(rows) != 2 || rows["valid"].SourceID != "valid" || !run.TraversalDone {
					t.Fatalf("incremental error handling changed: %+v %+v", run, rows)
				}
			}
		})
	}
}
