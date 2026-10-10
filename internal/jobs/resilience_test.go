package jobs

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
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

func TestRepeatedTrackerFailuresRecoverOnNextScheduledRun(t *testing.T) {
	for _, adapter := range []string{"http_json", "torznab"} {
		for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
			t.Run(adapter+"/"+string(mode), func(t *testing.T) {
				ctx, dsn, db := schedulerDatabase(t)
				var phase, status, failures atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if adapter == "torznab" && r.URL.Query().Get("t") == "caps" {
						fmt.Fprint(w, `<caps><limits default="1" max="1"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
						return
					}
					ids := []string{"2", "1"}
					if phase.Load() != 0 {
						ids = []string{"6", "5", "4", "3", "2", "1"}
					}
					offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
					if err != nil || offset < 0 || offset >= len(ids) {
						t.Errorf("unexpected source offset: %s", r.URL.Query().Get("offset"))
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if phase.Load() == 1 && offset == 2 {
						failures.Add(1)
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(int(status.Load()))
						return
					}
					if adapter == "torznab" {
						w.Header().Set("Content-Type", "application/xml")
						fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%d" total="%d"/><item><guid>%s</guid><title>Record %s</title></item></channel></rss>`, offset, len(ids), ids[offset], ids[offset])
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"items":[{"id":%q,"title":%q}],"total":%d}`, ids[offset], "Record "+ids[offset], len(ids))
					}
				}))
				t.Cleanup(server.Close)
				registry, err := providers.New(t.TempDir(), connectors.Validate)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = registry.Close() })
				p := schedulerProvider("outage-source", server.URL)
				p.Schedule = model.Schedule{Every: "1m", Mode: mode, KnownPages: 1}
				if adapter == "torznab" {
					p.Adapter, p.HTTP, p.Mapping, p.Pagination = adapter, model.HTTPConfig{Method: http.MethodGet}, model.Mapping{}, model.Pagination{}
				}
				saveScheduledProvider(t, registry, p, "")
				manager := New(db, registry, nil, 1)
				seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
				if err != nil {
					t.Fatal(err)
				}
				collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
				start := time.Now()
				if _, err := manager.scheduleDue(ctx, start); err != nil {
					t.Fatal(err)
				}
				phase.Store(1)
				for attempt, code := range []int{500, 502, 503} {
					status.Store(int32(code))
					if _, err := manager.scheduleDue(ctx, start.Add(time.Duration(attempt+1)*time.Minute)); err != nil {
						t.Fatal(err)
					}
					latest, err := db.LatestRuns(ctx, []string{p.ID})
					if err != nil {
						t.Fatal(err)
					}
					failed := collectScheduledAttempt(t, ctx, manager, db, latest[p.ID].ID, model.StatusFailed)
					if failed.Pages != 2 || failed.DistinctRecords != 2 || failed.TraversalDone || failed.Errors != 4 {
						t.Fatalf("failed attempt lost its prefix or certified the unvisited tail: %+v", failed)
					}
					want := []string{"1", "2"}
					if mode == model.ModeIncremental {
						want = append(want, "5", "6")
					}
					assertResilienceIDs(t, ctx, db, p.ID, want)
					// Reopen the database connection and recreate the manager between
					// failures: neither the baseline nor the scheduler lives in memory.
					db.Close()
					db, err = store.Open(ctx, dsn)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(db.Close)
					manager = New(db, registry, nil, 1)
				}
				phase.Store(2)
				if _, err := manager.scheduleDue(ctx, start.Add(4*time.Minute)); err != nil {
					t.Fatal(err)
				}
				latest, err := db.LatestRuns(ctx, []string{p.ID})
				if err != nil {
					t.Fatal(err)
				}
				next := latest[p.ID]
				known, err := db.Known(ctx, p.ID, []string{"1", "2", "5", "6"}, next.CreatedAt)
				if err != nil || !reflect.DeepEqual(known, map[string]bool{"1": true, "2": true}) {
					t.Fatalf("failed prefixes became a premature boundary: %+v %v", known, err)
				}
				completed := collectScheduledAttempt(t, ctx, manager, db, next.ID, model.StatusSucceeded)
				wantPages := 6
				if mode == model.ModeIncremental {
					wantPages = 5
				}
				if completed.Pages != wantPages || failures.Load() != 12 {
					t.Fatalf("next scheduled run skipped the formerly unreachable records: %+v failures=%d", completed, failures.Load())
				}
				assertResilienceIDs(t, ctx, db, p.ID, []string{"1", "2", "3", "4", "5", "6"})
			})
		}
	}
}

func assertResilienceIDs(t *testing.T, ctx context.Context, db *store.Store, provider string, want []string) {
	t.Helper()
	rows, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(rows.Items))
	for i, row := range rows.Items {
		ids[i] = row.SourceID
	}
	slices.Sort(ids)
	if !slices.Equal(ids, want) {
		t.Fatalf("published records changed unexpectedly: got %v, want %v", ids, want)
	}
}

// This worker runs only in a child test process, against its parent's disposable
// database. Killing it exercises actual session loss without orderly cleanup.
func TestCollectionCrashWorker(t *testing.T) {
	dsn := os.Getenv("CRASH_TEST_DATABASE_URL")
	if dsn == "" {
		return
	}
	parsed, err := url.Parse(dsn)
	if err != nil || !strings.HasPrefix(parsed.Path, "/ingest_test_") {
		t.Fatal("crash worker requires an isolated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	registry, err := providers.New(os.Getenv("CRASH_TEST_PROVIDERS"), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	manager := New(db, registry, nil, 1)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	<-manager.Done()
	t.Fatal("crash worker exited before its parent killed it")
}

func TestAbruptProcessRestartRecoversCommittedImport(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, dsn, db := schedulerDatabase(t)
			var update, hold atomic.Bool
			entered := make(chan struct{})
			var once sync.Once
			var firstRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ids := []string{"old-1", "old-2"}
				if update.Load() {
					ids = []string{"new-1", "new-2", "old-1", "old-2"}
				}
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				if err != nil || offset < 0 || offset >= len(ids) {
					t.Errorf("unexpected crash-test offset: %s", r.URL.Query().Get("offset"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if update.Load() && offset == 0 {
					firstRequests.Add(1)
				}
				if hold.Load() && offset == 1 {
					once.Do(func() { close(entered) })
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"items":[{"id":%q,"title":%q}],"total":%d}`, ids[offset], "Record "+ids[offset], len(ids))
			}))
			t.Cleanup(server.Close)
			directory := t.TempDir()
			registry, err := providers.New(directory, connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("crash-source", server.URL)
			p.Schedule = model.Schedule{KnownPages: 1}
			saveScheduledProvider(t, registry, p, "")
			manager := New(db, registry, nil, 1)
			seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
			update.Store(true)
			hold.Store(true)
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCollectionCrashWorker$", "-test.timeout=65s")
			child.Env = append(os.Environ(), "CRASH_TEST_DATABASE_URL="+dsn, "CRASH_TEST_PROVIDERS="+directory)
			var output bytes.Buffer
			child.Stdout, child.Stderr = &output, &output
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				if !waited {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			})
			awaitJobSignal(t, ctx, entered)
			committed, err := db.GetRun(ctx, run.ID)
			if err != nil || committed.Status != model.StatusRunning || committed.Pages != 1 || len(committed.Cursor) == 0 {
				t.Fatalf("child did not commit its first page: %+v %v", committed, err)
			}
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err == nil {
				t.Fatal("child exited normally instead of being killed")
			}
			waited = true
			hold.Store(false)
			db.Close()
			db, err = store.Open(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(db.Close)
			manager = New(db, registry, nil, 1)
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
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				completed, err := db.GetRun(ctx, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				if completed.Status == model.StatusSucceeded {
					if firstRequests.Load() != 1 || completed.Errors != 0 {
						t.Fatalf("restart refetched committed work or lost clean progress: %+v", completed)
					}
					assertResilienceIDs(t, ctx, db, p.ID, []string{"new-1", "new-2", "old-1", "old-2"})
					break
				}
				if completed.Status == model.StatusFailed || completed.Status == model.StatusCancelled {
					t.Fatalf("restart failed to recover: %+v; child: %s", completed, output.String())
				}
				select {
				case <-deadline.C:
					t.Fatalf("restart did not resume automatically: %+v; child: %s", completed, output.String())
				case <-ticker.C:
				}
			}
		})
	}
}
