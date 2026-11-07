package jobs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

// A complete two-page import includes HTTP, cursor advancement, persistence,
// publication and subsequent incremental updates. Every iteration validates data.
func BenchmarkSourceImport(b *testing.B) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
		b.Run(string(mode), func(b *testing.B) {
			ctx, _, db := schedulerDatabase(b)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				items := make([]map[string]any, 0, 100)
				for i := offset; i < offset+100 && i < 200; i++ {
					items = append(items, map[string]any{"id": i + 1, "title": fmt.Sprintf("Synthetic.Documentary.%d.2026", i+1), "info_hash": fmt.Sprintf("%040x", i+1)})
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": 200})
			}))
			b.Cleanup(server.Close)
			registry, err := providers.New(b.TempDir(), connectors.Validate)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("synthetic", server.URL)
			p.PageSize, p.Schedule.MaxPages, p.Schedule.KnownPages = 100, 0, 0
			p.Mapping.Fields["info_hash"] = "/info_hash"
			p.Output.Fields = append(p.Output.Fields, "info_hash")
			saveScheduledProvider(b, registry, p, "")
			manager := New(db, registry, nil, 1)
			collect := func(mode model.RunMode) {
				run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: mode})
				if err != nil {
					b.Fatal(err)
				}
				finished := collectScheduledAttempt(b, ctx, manager, db, run.ID, model.StatusSucceeded)
				page, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID, Limit: 200})
				if err != nil || !finished.TraversalDone || page.Total != 200 || len(page.Items) != 200 || page.Items[0].Fields["info_hash"] == nil {
					b.Fatal("Complete import lost publication or fields", err)
				}
			}
			if mode == model.ModeIncremental {
				collect(model.ModeFull)
			}
			b.ResetTimer()
			for b.Loop() {
				collect(mode)
			}
			b.ReportMetric(200, "published/op")
		})
	}
}
