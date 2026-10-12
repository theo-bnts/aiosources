package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestSavedConcurrencyControlsRealImportsAcrossTheEntireRange(t *testing.T) {
	ctx, databaseURL, initial := schedulerDatabase(t)
	initial.Close()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("pool_max_conns", "4")
	parsed.RawQuery = query.Encode()
	db, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	type sourceResponse struct {
		release chan struct{}
		once    sync.Once
	}
	const sourceCount = model.MaxCollectionWorkers + 1
	sources := make(map[string]*sourceResponse, sourceCount)
	for index := range sourceCount {
		sources[fmt.Sprintf("concurrency-%02d", index)] = &sourceResponse{release: make(chan struct{})}
	}
	entered := make(chan string, sourceCount)
	var requests, active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/")
		response := sources[id]
		if response == nil {
			t.Errorf("unexpected source request: %s", id)
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); current > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, current) {
				break
			}
		}
		select {
		case entered <- id:
		case <-r.Context().Done():
			return
		}
		// Hold the real HTTP response open, rather than merely marking a
		// database row running. Each admitted import must reach its source.
		select {
		case <-response.release:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"one","title":"Synthetic release"}],"total":1}`))
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	release := func(id string) {
		sources[id].once.Do(func() { close(sources[id].release) })
	}
	t.Cleanup(func() {
		for id := range sources {
			release(id)
		}
	})
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	for index := range sourceCount {
		id := fmt.Sprintf("concurrency-%02d", index)
		provider := schedulerProvider(id, server.URL+"/"+id)
		provider.RequestTimeout = "2m"
		provider.Schedule = model.Schedule{}
		saveScheduledProvider(t, registry, provider, "")
	}
	// A bootstrap value of one must not impose a fixed runtime worker cap.
	manager := New(db, registry, nil, 1)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	runs := make(map[string]model.Run, sourceCount)
	for index := range sourceCount {
		id := fmt.Sprintf("concurrency-%02d", index)
		run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: id, Mode: model.ModeFull})
		if err != nil {
			t.Fatal(err)
		}
		runs[id] = run
	}
	setLimit := func(limit int) {
		t.Helper()
		settings, err := db.CollectionSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		settings.Workers = limit
		if err := db.UpdateCollectionSettings(ctx, settings); err != nil {
			t.Fatal(err)
		}
		// No direct dispatcher wake-up: exercise the saved setting's real
		// database notification path used by the administration API.
	}
	awaitRequest := func() string {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case id := <-entered:
			return id
		case <-manager.Done():
			t.Fatal("collection manager stopped before the source request")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-timer.C:
			t.Fatalf("saved concurrency was not reached: active HTTP requests=%d", active.Load())
		}
		return ""
	}
	assertQueue := func(running, queued int) {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		overview, err := db.CollectionOverview(bounded)
		if err != nil || overview.Running != running || overview.Queued != queued {
			t.Fatalf("unexpected real import queue: %+v %v; want running=%d queued=%d", overview, err, running, queued)
		}
		if got := int(active.Load()); got != running {
			t.Fatalf("database running=%d but only %d real HTTP requests are active", running, got)
		}
	}
	awaitSuccess := func(id string) {
		t.Helper()
		deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			run, err := db.GetRun(deadline, runs[id].ID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status == model.StatusSucceeded {
				if run.Pages != 1 || run.Records != 1 || run.Errors != 0 {
					t.Fatalf("concurrent import did not preserve its source record: %+v", run)
				}
				return
			}
			select {
			case <-ticker.C:
			case <-manager.Done():
				t.Fatal("collection manager stopped before import completion")
			case <-deadline.Done():
				t.Fatalf("concurrent import did not finish: %+v", run)
			}
		}
	}
	var started []string
	for limit := 1; limit <= model.MaxCollectionWorkers; limit++ {
		if limit > 1 {
			setLimit(limit)
		}
		started = append(started, awaitRequest())
		assertQueue(limit, sourceCount-limit)
		t.Logf("saved limit %d reached by %d simultaneous source requests", limit, active.Load())
	}
	const reducedLimit = 5
	const draining = model.MaxCollectionWorkers - reducedLimit
	setLimit(reducedLimit)
	for index := range draining {
		release(started[index])
		awaitSuccess(started[index])
		assertQueue(model.MaxCollectionWorkers-index-1, 1)
	}
	// At five active imports the queued source must still wait. Finishing
	// another import must admit it immediately into the newly free slot.
	release(started[draining])
	awaitSuccess(started[draining])
	started = append(started, awaitRequest())
	assertQueue(reducedLimit, 0)
	for _, id := range started[draining+1:] {
		release(id)
	}
	for _, id := range started[draining+1:] {
		awaitSuccess(id)
	}
	assertQueue(0, 0)
	if requests.Load() != sourceCount || peak.Load() != model.MaxCollectionWorkers {
		t.Fatalf("unexpected HTTP admission: requests=%d peak=%d", requests.Load(), peak.Load())
	}
	for id := range sources {
		published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: id})
		if err != nil || published.Total != 1 {
			t.Fatalf("concurrent import failed to publish %s: %+v %v", id, published, err)
		}
	}
}
