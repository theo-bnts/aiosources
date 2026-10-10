package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

// These fixtures exercise Incremental behavior after an existing, empty
// catalogue has already been initialized, without making a source request.
func seedCollectionBaseline(t *testing.T, ctx context.Context, db *store.Store, provider model.Provider) {
	t.Helper()
	queued, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != queued.ID {
		t.Fatalf("claim existing catalogue baseline: %+v %v", run, err)
	}
	saved, err := db.SavePage(ctx, *run, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, saved.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestInitialFullTraversalContinuesThenUsesIncrementalQuery(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scheduled=%t", scheduled), func(t *testing.T) {
			ctx, _, db := schedulerDatabase(t)
			var fullRequests, incrementalRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("phase") == "incremental" {
					incrementalRequests.Add(1)
					_, _ = fmt.Fprint(w, `{"items":[{"id":"0","title":"First"}],"total":3}`)
					return
				}
				fullRequests.Add(1)
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				if err != nil || offset > 2 {
					t.Errorf("unexpected initial Full position: %s", r.URL.Query().Get("offset"))
					http.Error(w, "position", http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `{"items":[{"id":"%d","title":"Record %d"}],"total":3}`, offset, offset)
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			provider := schedulerProvider("initial-traversal", server.URL)
			provider.Schedule.Mode, provider.Schedule.KnownPages = model.ModeIncremental, 1
			provider.HTTP.IncrementalQuery = map[string]any{"phase": "incremental"}
			if !scheduled {
				provider.Schedule.Every = ""
			}
			saveScheduledProvider(t, registry, provider, "")
			manager := New(db, registry, nil, 1)
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			var initial model.Run
			if scheduled {
				if _, err := manager.scheduleDue(ctx, start); err != nil {
					t.Fatal(err)
				}
				if _, err := manager.scheduleDue(ctx, start.Add(time.Minute)); err != nil {
					t.Fatal(err)
				}
				latest, err := db.LatestRuns(ctx, []string{provider.ID})
				if err != nil {
					t.Fatal(err)
				}
				initial = latest[provider.ID]
			} else {
				budget, knownPages := 1, 1
				initial, err = manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, MaxPages: &budget, KnownPages: &knownPages})
				if err != nil {
					t.Fatal(err)
				}
			}
			if initial.Mode != model.ModeFull || !initial.InitialFull || initial.MaxPages != 1 {
				t.Fatalf("requested Incremental bypassed initial Full: %+v", initial)
			}
			for attempt, status := range []model.RunStatus{model.StatusPaused, model.StatusPaused, model.StatusSucceeded} {
				if attempt > 0 {
					if scheduled {
						_, err = manager.scheduleDue(ctx, start.Add(time.Duration(attempt+1)*time.Minute))
					} else {
						_, err = manager.Resume(ctx, initial.ID)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				finished := collectScheduledAttempt(t, ctx, manager, db, initial.ID, status)
				if !finished.InitialFull || finished.ID != initial.ID || finished.Pages != attempt+1 {
					t.Fatalf("continuation restarted initial Full: %+v", finished)
				}
				live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
				want := int64(0)
				if status == model.StatusSucceeded {
					want = 3
				}
				if err != nil || live.Total != want {
					t.Fatalf("incorrect initial publication: %+v %v", live, err)
				}
			}
			var next model.Run
			if scheduled {
				if _, err := manager.scheduleDue(ctx, start.Add(4*time.Minute)); err != nil {
					t.Fatal(err)
				}
				latest, err := db.LatestRuns(ctx, []string{provider.ID})
				if err != nil {
					t.Fatal(err)
				}
				next = latest[provider.ID]
			} else {
				next, err = manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental})
				if err != nil {
					t.Fatal(err)
				}
			}
			if next.Mode != model.ModeIncremental || next.InitialFull {
				t.Fatalf("initialized catalogue still selects Full: %+v", next)
			}
			collectScheduledAttempt(t, ctx, manager, db, next.ID, model.StatusSucceeded)
			if fullRequests.Load() != 3 || incrementalRequests.Load() != 1 {
				t.Fatalf("wrong traversal or repeated committed pages: full=%d incremental=%d", fullRequests.Load(), incrementalRequests.Load())
			}
		})
	}
}
