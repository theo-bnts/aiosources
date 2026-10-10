package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestFullSmallScopeRecoverySurvivesRestartAndPublishesEveryNativeIdentity(t *testing.T) {
	ctx, dsn, db := schedulerDatabase(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/list" || r.URL.Query().Get("category") != "1" {
			t.Error("small category reconciliation requested numeric recovery or another scope")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":1,"title":"First","category":1,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"id":2,"title":"Second","category":1,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},{"id":3,"title":"Reclassified","category":2,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"total":3}`))
	}))
	t.Cleanup(upstream.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("scope-recovery", upstream.URL+"/list")
	p.PageSize = 3
	p.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1}
	p.Mapping.Fields["category_id"] = "/category"
	p.Mapping.Fields["info_hash"] = "/hash"
	p.Traversal = &model.JSONTraversal{WindowPages: 100, MinimumTotal: 3, TotalMode: "at_least", TotalPaths: []string{"/total"}, Scopes: []model.JSONScope{
		{ID: "selected", Query: map[string]any{"category": 1}, Match: map[string]any{"category_id": 1}},
		{ID: "other", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}},
	}, IDRecovery: &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatal("claim original collection", err)
	}
	checkpoint := json.RawMessage(`{"version":3,"phase":"resolve","scope":2,"page":1,"ends":[],"totals":[3,1],"recovery":true,"next_id":3955,"max_id":4000}`)
	sharedHash := strings.Repeat("a", 40)
	saved, err := db.SavePage(ctx, *claimed, model.Page{Body: []byte("original response"), Next: checkpoint, Items: []model.Record{
		{SourceID: "1", Fields: map[string]any{"title": "First", "category_id": 1, "info_hash": sharedHash}, CoverageScopes: []string{"selected"}},
		{SourceID: "2", Fields: map[string]any{"title": "Second", "category_id": 1, "info_hash": sharedHash}, CoverageScopes: []string{"selected"}},
		{SourceID: "3", Fields: map[string]any{"title": "Reclassified", "category_id": 2, "info_hash": sharedHash}, CoverageScopes: []string{"other"}},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, saved.ID, model.StatusPaused, "page budget reached", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	manager = New(reopened, registry, nil, 1)
	for _, status := range []model.RunStatus{model.StatusPaused, model.StatusSucceeded} {
		if _, err := manager.Resume(ctx, saved.ID); err != nil {
			t.Fatal(err)
		}
		saved = collectScheduledAttempt(t, ctx, manager, reopened, saved.ID, status)
		published, err := reopened.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
		if err != nil || (status == model.StatusPaused && published.Total != 0) || (status == model.StatusSucceeded && published.Total != 3) {
			t.Fatalf("Full published prematurely or lost an identity: %s count=%d err=%v", status, published.Total, err)
		}
		if status == model.StatusSucceeded {
			for _, record := range published.Items {
				if record.Fields["info_hash"] != sharedHash || (record.SourceID == "3" && connectors.FieldString(record.Fields["category_id"]) != "2") {
					t.Fatal("reconciliation changed an identity's fields or classification")
				}
			}
		}
	}
	if saved.Errors != 0 || saved.DistinctRecords != 3 || !saved.TraversalDone || requests.Load() != 1 {
		t.Fatalf("recovered collection did not finish with one bounded root check: %+v requests=%d", saved, requests.Load())
	}
	events, err := reopened.Events(ctx, saved.ID, model.ListOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, event := range events.Items {
		if event.Message == "Complete source category checked; collection target updated" {
			found = event.Data["coverage_scope"] == "selected" && connectors.FieldString(event.Data["coverage_previous_target"]) == "3" && connectors.FieldString(event.Data["coverage_target"]) == "2"
		}
	}
	if !found {
		t.Fatal("history does not explain the verified target adjustment")
	}
}
