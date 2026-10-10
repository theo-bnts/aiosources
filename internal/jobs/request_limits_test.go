package jobs

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestRequestLimitsSurviveBudgetPauseAndManagerRestart(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			_, _ = w.Write([]byte(`{"items":[{"id":"first","title":"First"}],"total":2}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"second","title":"Second"}],"total":2}`))
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("durable-request-limit", server.URL)
	provider.Schedule = model.Schedule{}
	provider.RequestLimits = &model.RequestLimits{PerMinute: 1}
	document := saveScheduledProvider(t, registry, provider, "")
	manager := New(db, registry, nil, 1)
	unlimitedPages, oneSecond := 0, 1
	run, err := manager.Enqueue(ctx, model.StartRun{
		ProviderID: provider.ID, Mode: model.ModeFull,
		MaxPages: &unlimitedPages, MaxDurationSeconds: &oneSecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	held := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusPaused)
	if held.PauseReason != model.PauseBudget || held.Pages != 1 || held.DistinctRecords != 1 || held.Errors != 0 || requests.Load() != 1 {
		t.Fatalf("quota wait did not preserve the first page at the duration boundary: run=%+v requests=%d", held, requests.Load())
	}
	checkpoint := bytes.Clone(held.Cursor)
	// Editing the source cannot erase the saved run's stricter limit or the
	// shared admission ledger when Resume constructs a fresh HTTP client.
	provider.RequestLimits = &model.RequestLimits{PerMinute: 100}
	saveScheduledProvider(t, registry, provider, document.Revision)
	restarted := New(db, registry, nil, 1)
	if _, err := restarted.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	again := collectScheduledAttempt(t, ctx, restarted, db, run.ID, model.StatusPaused)
	if again.PauseReason != model.PauseBudget || again.Pages != 1 || again.DistinctRecords != 1 || again.Errors != 0 || requests.Load() != 1 || !bytes.Equal(checkpoint, again.Cursor) {
		t.Fatalf("Resume bypassed quota history or changed the committed cursor: run=%+v requests=%d", again, requests.Load())
	}
}
