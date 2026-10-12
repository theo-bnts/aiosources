package jobs

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

// Complete-history regressions explicitly opt into full storage; the default
// compact path is exercised by the other lifecycle and storage contract tests.
func useFullStorage(t *testing.T, ctx context.Context, db *store.Store) {
	t.Helper()
	current, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Settings.Mode = model.StorageFull
	if err := db.UpdateStorageSettings(ctx, current.Settings); err != nil {
		t.Fatal(err)
	}
}

func TestCompactMetadataWithNoUsefulFieldsFinishesWithoutContactingTheSource(t *testing.T) {
	var requests atomic.Int32
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "Unexpected request", http.StatusBadRequest)
	}, func(p *model.Provider) {
		p.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1}
		p.Mapping.Fields["info_hash"] = "/hash"
		p.Mapping.Fields["category_id"] = "/category"
		p.Traversal = &model.JSONTraversal{
			WindowPages: 100, TotalMode: "at_least", TotalPaths: []string{"/total"},
			Scopes:       []model.JSONScope{{ID: "selected", Match: map[string]any{"category_id": 1}}},
			EnrichFields: []string{"description"}, MetadataAfterIncremental: true,
			IDRecovery: &model.JSONIDRecovery{
				First: 1, DiscoveryQuery: map[string]any{"sort": "desc"},
				ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f0-9]{40}$", DetailURL: "/detail/{value}",
				Mapping: model.Mapping{ID: "/id", Fields: map[string]string{
					"title": "/title", "info_hash": "/hash", "category_id": "/category", "description": "/description",
				}},
			},
		}
	})
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeMetadata})
	if err != nil {
		t.Fatal(err)
	}
	finished := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
	if requests.Load() != 0 || !finished.TraversalDone || finished.StorageMode != model.StorageCompact {
		t.Fatalf("unused enrichment fetched data or lost its completion checkpoint: %+v calls=%d", finished, requests.Load())
	}
}
