package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

func policyManager(t *testing.T, handler http.HandlerFunc, configure func(*model.Provider)) (context.Context, *store.Store, *Manager, model.Provider) {
	t.Helper()
	ctx, _, db := schedulerDatabase(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("policy-source", server.URL)
	provider.Schedule = model.Schedule{}
	if configure != nil {
		configure(&provider)
	}
	saveScheduledProvider(t, registry, provider, "")
	return ctx, db, New(db, registry, nil, 1), provider
}

func TestQuotaPolicyPreservesEveryResponseDeadline(t *testing.T) {
	for _, test := range []struct {
		name       string
		success    bool
		caps       bool
		duration   bool
		retries    int
		wantStatus model.RunStatus
		wantReason model.PauseReason
	}{
		{name: "long rejected wait", retries: 3, wantStatus: model.StatusPaused, wantReason: model.PauseQuota},
		{name: "exhausted retries", retries: 0, wantStatus: model.StatusFailed},
		{name: "successful exhausted quota", success: true, retries: 3, wantStatus: model.StatusPaused, wantReason: model.PauseQuota},
		{name: "capabilities exhausted quota", success: true, caps: true, retries: 3, wantStatus: model.StatusPaused, wantReason: model.PauseQuota},
		{name: "duration during rejected cooldown", duration: true, retries: 3, wantStatus: model.StatusPaused, wantReason: model.PauseBudget},
		{name: "duration during proactive cooldown", success: true, duration: true, retries: 3, wantStatus: model.StatusPaused, wantReason: model.PauseBudget},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			var before atomic.Int64
			var early atomic.Bool
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n == 1 {
					before.Store(time.Now().Add(2 * time.Second).UnixNano())
					if test.success {
						w.Header().Set("RateLimit-Remaining", "0")
						w.Header().Set("RateLimit-Reset", "2")
					} else {
						w.Header().Set("Retry-After", "2")
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte("original quota response"))
						return
					}
				} else if time.Now().UnixNano() < before.Load() {
					early.Store(true)
				}
				if test.caps {
					if r.URL.Query().Get("t") == "caps" {
						_, _ = w.Write([]byte(`<caps><limits max="100" default="1"/><searching><search available="yes" supportedParams="q"/></searching></caps>`))
					} else {
						_, _ = w.Write([]byte(`<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><newznab:response offset="0" total="1"/><item><guid>native-1</guid><title>Recovered release</title><newznab:attr name="size" value="1"/></item></channel></rss>`))
					}
				} else if test.success {
					_, _ = fmt.Fprintf(w, `{"items":[{"id":"native-%d","title":"Release"}],"total":2}`, n)
				} else {
					_, _ = w.Write([]byte(`{"items":[{"id":"recovered","title":"Recovered"}],"total":1}`))
				}
			}, func(p *model.Provider) {
				if test.caps {
					p.Adapter, p.HTTP.ItemsPath = "torznab", ""
					p.Mapping, p.Pagination = model.Mapping{}, model.Pagination{}
					p.Options = map[string]any{"id_source": "guid"}
				}
			})
			policy := model.CollectionPolicy{MaxQuotaRetries: test.retries, NoProgressAction: "warn"}
			if test.caps {
				// Capabilities are control evidence, not failed catalogue
				// progress; even the smallest guard must permit Resume to search.
				policy.NoProgressRequests, policy.NoProgressAction = 1, "pause"
			}
			if test.duration {
				policy.MaxDurationSeconds = 1
			} else if test.wantReason == model.PauseQuota {
				policy.MaxQuotaWaitSeconds = 1
			}
			run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull, Policy: &policy})
			if err != nil {
				t.Fatal(err)
			}
			stopped := collectScheduledAttempt(t, ctx, manager, db, run.ID, test.wantStatus)
			wantPages, wantErrors := 0, 1
			if test.success {
				wantErrors = 0
				if !test.caps {
					wantPages = 1
				}
			}
			if stopped.PauseReason != test.wantReason || stopped.Pages != wantPages || stopped.Errors != wantErrors || requests.Load() != 1 {
				t.Fatalf("quota boundary was not committed exactly once: requests=%d run=%+v", requests.Load(), stopped)
			}
			retryAt, err := db.RunRetryAt(ctx, run.ID)
			if err != nil || retryAt.UnixNano() < before.Load() {
				t.Fatalf("known source deadline disappeared: %v %v", retryAt, err)
			}
			if test.duration {
				// A new attempt gets a fresh duration allowance. Wait outside it so
				// this test does not race a one-second budget against its cooldown.
				time.Sleep(max(0, time.Until(retryAt)))
			}
			if _, err := manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			completed := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
			wantRequests, wantRecords := int32(2), int64(1)
			if test.caps {
				wantRequests = 3
			} else if test.success {
				wantRecords = 2
			}
			if early.Load() || requests.Load() != wantRequests || completed.DistinctRecords != wantRecords || completed.Errors != wantErrors {
				t.Fatalf("Resume bypassed a deadline or duplicated its archive: early=%t requests=%d run=%+v", early.Load(), requests.Load(), completed)
			}
		})
	}
}

func TestDurationBudgetCommitsInflightResponseAndRenewsOnResume(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual-precedence=%t", manual), func(t *testing.T) {
			entered := make(chan struct{})
			var requests atomic.Int32
			var interrupted atomic.Bool
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if n == 1 {
					close(entered)
				}
				timer := time.NewTimer(1100 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-r.Context().Done():
					interrupted.Store(true)
					return
				case <-timer.C:
				}
				_, _ = fmt.Fprintf(w, `{"items":[{"id":"native-%d","title":"Release"}],"total":2}`, n)
			}, nil)
			one := 1
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull, MaxDurationSeconds: &one})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			finished := make(chan error, 1)
			go func() {
				status, message, reason := manager.collect(ctx, *claimed)
				_, err := db.FinishRun(ctx, run.ID, status, message, reason)
				finished <- err
			}()
			awaitJobSignal(t, ctx, entered)
			if manual {
				if _, err := manager.Pause(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("duration boundary did not finish")
			}
			held, err := db.GetRun(ctx, run.ID)
			wantReason := model.PauseBudget
			if manual {
				wantReason = model.PauseManual
			}
			if err != nil || held.Status != model.StatusPaused || held.PauseReason != wantReason || held.Pages != 1 || held.Errors != 0 || interrupted.Load() || requests.Load() != 1 {
				t.Fatalf("duration cut an atomic response or overrode manual hold: %+v interrupted=%t requests=%d error=%v", held, interrupted.Load(), requests.Load(), err)
			}
			if _, err := manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			completed := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
			if completed.DistinctRecords != 2 || completed.Pages != 2 || completed.Errors != 0 || requests.Load() != 2 || interrupted.Load() {
				t.Fatalf("resume did not renew its budget or final response was paused: %+v", completed)
			}
		})
	}
}

func TestUsefulProgressGuardDoesNotPauseCompletedTraversal(t *testing.T) {
	for _, test := range []struct {
		name   string
		action string
		final  bool
	}{
		{name: "pause before completion", action: "pause"},
		{name: "warn and continue", action: "warn"},
		{name: "finish at threshold", action: "pause", final: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			total := 5
			if test.final {
				total = 4
			}
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				items := `[{"id":"first","title":"First"}]`
				switch r.URL.Query().Get("offset") {
				case "1":
					items = `[{"id":"second","title":"Second"}]`
				case "2":
					// A previously unseen page combination, but no new native ID.
					items = `[{"id":"first","title":"Changed"},{"id":"second","title":"Changed"}]`
				case "4":
					items = `[{"id":"third","title":"Third"}]`
				}
				_, _ = fmt.Fprintf(w, `{"items":%s,"total":%d}`, items, total)
			}, func(p *model.Provider) { p.PageSize = 2 })
			policy := model.CollectionPolicy{MaxQuotaRetries: 3, NoProgressRequests: 1, NoProgressAction: test.action}
			run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull, Policy: &policy})
			if err != nil {
				t.Fatal(err)
			}
			want := model.StatusSucceeded
			if test.action == "pause" && !test.final {
				want = model.StatusPaused
			}
			stopped := collectScheduledAttempt(t, ctx, manager, db, run.ID, want)
			if want == model.StatusPaused {
				if stopped.PauseReason != model.PauseNoProgress || stopped.RequestsWithoutNewIDs != 1 || requests.Load() != 3 {
					t.Fatalf("guard did not stop at the committed threshold: %+v requests=%d", stopped, requests.Load())
				}
				resumed, err := manager.Resume(ctx, run.ID)
				if err != nil || resumed.RequestsWithoutNewIDs != 0 || !bytes.Equal(resumed.Cursor, stopped.Cursor) {
					t.Fatalf("resume lost checkpoint or did not renew allowance: %+v %v", resumed, err)
				}
				stopped = collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
			}
			wantRecords := int64(3)
			if test.final {
				wantRecords = 2
			}
			if stopped.Status != model.StatusSucceeded || stopped.DistinctRecords != wantRecords {
				t.Fatalf("guard changed the published native-ID set: %+v", stopped)
			}
			events, err := db.Events(ctx, run.ID, model.ListOptions{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			warnings := 0
			for _, event := range events.Items {
				if event.Kind == "no_progress" {
					warnings++
				}
			}
			if warnings != 1 {
				t.Fatalf("one stagnant streak emitted %d useful-progress warnings", warnings)
			}
		})
	}
}

func TestRequestTimeoutInheritanceIsFrozenAtCreation(t *testing.T) {
	var requests atomic.Int32
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		timer := time.NewTimer(1200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			_, _ = w.Write([]byte(`{"items":[{"id":"native","title":"Release"}],"total":1}`))
		}
	}, func(p *model.Provider) { p.RequestTimeout = "1s" })
	old, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.registry.Get(provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider.RequestTimeout = "3s"
	saveScheduledProvider(t, manager.registry, provider, doc.Revision)
	collectScheduledAttempt(t, ctx, manager, db, old.ID, model.StatusFailed)
	fresh, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, db, fresh.ID, model.StatusSucceeded)
	doc, err = manager.registry.Get(provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider.RequestTimeout = "100ms"
	saveScheduledProvider(t, manager.registry, provider, doc.Revision)
	explicit, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, db, explicit.ID, model.StatusFailed)
	if requests.Load() != 3 {
		t.Fatalf("timeout handling unexpectedly retried %d source requests", requests.Load())
	}
}

func TestNewRunUsesFixedFallbacksAndPreservesExplicitOverrides(t *testing.T) {
	ctx, _, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("admission must not contact the source")
	}, func(p *model.Provider) { p.RequestTimeout = "" })
	for _, test := range []struct {
		mode                    model.RunMode
		pages, duration         *int
		wantPages, wantDuration int
	}{
		{mode: model.ModePreview, wantPages: model.DefaultPreviewPages},
		{mode: model.ModeFull},
		{mode: model.ModeFull, pages: new(7), duration: new(19), wantPages: 7, wantDuration: 19},
	} {
		run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: test.mode, MaxPages: test.pages, MaxDurationSeconds: test.duration})
		if err != nil || run.Config.RequestTimeout != model.DefaultRequestTimeout || run.MaxPages != test.wantPages || run.EffectivePolicy().MaxDurationSeconds != test.wantDuration {
			t.Fatalf("new run fallback or explicit override: %+v %v", run, err)
		}
		if _, err := manager.Cancel(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourcePageLimitsRemainFrozenAndAllowExplicitUnlimited(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModePreview} {
		t.Run(string(mode), func(t *testing.T) {
			var requests atomic.Int32
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = fmt.Fprintf(w, `{"items":[{"id":"native-%s","title":"Release"}],"total":3}`, r.URL.Query().Get("offset"))
			}, func(p *model.Provider) { p.Schedule.MaxPages = 1 })
			frozen, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := manager.registry.Get(provider.ID)
			if err != nil {
				t.Fatal(err)
			}
			provider.Schedule.MaxPages = 2
			saveScheduledProvider(t, manager.registry, provider, doc.Revision)
			wantStatus, wantRequests := model.StatusPaused, int32(1)
			if mode == model.ModePreview {
				wantStatus, wantRequests = model.StatusSucceeded, int32(model.DefaultPreviewPages)
			}
			finished := collectScheduledAttempt(t, ctx, manager, db, frozen.ID, wantStatus)
			if requests.Load() != wantRequests || finished.Pages != int(wantRequests) {
				t.Fatalf("settings edit changed an existing attempt's page boundary: requests=%d run=%+v", requests.Load(), finished)
			}
			if mode == model.ModeFull {
				if finished.PauseReason != model.PauseBudget {
					t.Fatalf("page limit did not retain a budget pause: %+v", finished)
				}
				if _, err := manager.Cancel(ctx, frozen.ID); err != nil {
					t.Fatal(err)
				}
			}
			zero := 0
			unlimited, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: mode, MaxPages: &zero, MaxDurationSeconds: &zero})
			if err != nil {
				t.Fatal(err)
			}
			finished = collectScheduledAttempt(t, ctx, manager, db, unlimited.ID, model.StatusSucceeded)
			if requests.Load() != wantRequests+3 || finished.DistinctRecords != 3 || finished.Pages != 3 {
				t.Fatalf("explicit zero inherited a page limit instead of completing: %+v requests=%d", finished, requests.Load())
			}
		})
	}
}

func TestManualKnownPageOverrideRejectsIncompatibleTraversal(t *testing.T) {
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid collection issued a source request")
	}, func(p *model.Provider) {
		p.Options = map[string]any{"allow_total_growth": true}
	})
	knownPages := 1
	if _, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, KnownPages: &knownPages}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("incompatible known-page override was admitted: %v", err)
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || runs.Total != 0 {
		t.Fatalf("invalid override left durable work: %+v %v", runs, err)
	}
}

func TestTorznabIncrementalKnownPagesPerCategorySurviveResume(t *testing.T) {
	for _, scope := range []string{"each", "advertised"} {
		t.Run(scope, func(t *testing.T) {
			var incremental atomic.Bool
			requests := make(chan string, 16)
			baseline := map[string][]string{
				"2000": {"old-a0", "old-a1", "old-a2"},
				"5000": {"old-b0", "old-b1", "old-b2"},
			}
			updates := map[string][]string{
				"2000": {"old-a0", "new-shared", "old-a1", "old-a2", "beyond-a"},
				"5000": {"old-b0", "new-shared", "old-b1", "old-b2", "beyond-b"},
			}
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				if r.URL.Query().Get("t") == "caps" {
					_, _ = fmt.Fprint(w, `<caps><limits default="1" max="1"/><searching><search available="yes" supportedParams="q"/></searching><categories><category id="2000" name="Films"/><category id="5000" name="Series"/></categories></caps>`)
					return
				}
				category := r.URL.Query().Get("cat")
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				ids, title := baseline[category], "Baseline"
				if incremental.Load() {
					requests <- category + ":" + strconv.Itoa(offset)
					ids, title = updates[category], "Updated"
				}
				if err != nil || offset < 0 || offset >= len(ids) {
					t.Errorf("unexpected category or offset: %s:%d", category, offset)
					http.Error(w, "unexpected page", http.StatusBadRequest)
					return
				}
				_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%d" total="%d"/><item><guid>%s</guid><title>%s</title><x:attr name="infohash" value="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"/></item></channel></rss>`, offset, len(ids), ids[offset], title)
			}, func(p *model.Provider) {
				p.Adapter, p.HTTP.ItemsPath = "torznab", ""
				p.Mapping, p.Pagination = model.Mapping{}, model.Pagination{}
				p.Options = map[string]any{"category_scope": scope}
				if scope == "each" {
					p.Search.Categories = []int{2000, 5000}
				}
			})
			seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
			incremental.Store(true)
			maxPages, knownPages := 1, 2
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, MaxPages: &maxPages, KnownPages: &knownPages})
			if err != nil {
				t.Fatal(err)
			}
			wantRequests := []string{"2000:0", "2000:1", "2000:2", "2000:3", "5000:0", "5000:1", "5000:2", "5000:3"}
			for page := 1; page <= len(wantRequests); page++ {
				if page > 1 {
					if _, err := manager.Resume(ctx, run.ID); err != nil {
						t.Fatal(err)
					}
				}
				status := model.StatusPaused
				if page == len(wantRequests) {
					status = model.StatusSucceeded
				}
				run = collectScheduledAttempt(t, ctx, manager, db, run.ID, status)
				if run.Pages != page {
					t.Fatalf("category resume replayed or skipped a page: %+v", run)
				}
			}
			var gotRequests []string
			for len(requests) > 0 {
				gotRequests = append(gotRequests, <-requests)
			}
			if !reflect.DeepEqual(gotRequests, wantRequests) {
				t.Fatalf("known-page boundary skipped a category, lost its streak, or accepted a current-run identity: got %v want %v", gotRequests, wantRequests)
			}
			published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil || published.Total != 7 || run.DistinctRecords != 7 {
				t.Fatalf("incremental publication lost native identities: %+v run=%+v err=%v", published, run, err)
			}
			for _, record := range published.Items {
				if record.SourceID == "beyond-a" || record.SourceID == "beyond-b" || record.Fields["title"] != "Updated" {
					t.Fatalf("category boundary fetched beyond its stop or left an earlier record unvisited: %+v", record)
				}
			}
		})
	}
}

func TestJSONIncrementalOrderingAndBaselineSurviveResume(t *testing.T) {
	var incremental atomic.Bool
	var count atomic.Int32
	count.Store(8)
	requests := make(chan int, 10)
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		offset, err := strconv.Atoi(query.Get("offset"))
		if err != nil || offset < 0 || query.Get("state") != "published" || query.Get("sort_by") != "added_date" {
			t.Error("pagination or catalogue filters changed")
			http.Error(w, "invalid query", http.StatusBadRequest)
			return
		}
		wantOrder := "asc"
		if incremental.Load() {
			wantOrder = "desc"
			requests <- offset
			if offset == 2 {
				count.CompareAndSwap(12, 13)
			}
		}
		if query.Get("order") != wantOrder {
			t.Errorf("wrong listing order: got %q want %q", query.Get("order"), wantOrder)
			http.Error(w, "wrong order", http.StatusBadRequest)
			return
		}
		total := int(count.Load())
		first, second := offset+1, offset+2
		if incremental.Load() {
			first, second = total-offset, total-offset-1
		}
		advertised := total
		if incremental.Load() && offset >= 6 {
			// Cached pages can advertise an older count without changing their positions.
			advertised--
		}
		_, _ = fmt.Fprintf(w, `{"items":[{"id":%d,"title":"Release %d"},{"id":%d,"title":"Release %d"}],"total":%d}`, first, first, second, second, advertised)
	}, func(p *model.Provider) {
		p.PageSize = 2
		p.HTTP.Query = map[string]any{"state": "published", "sort_by": "added_date", "order": "asc"}
		p.HTTP.IncrementalQuery = map[string]any{"order": "desc"}
		p.Options = map[string]any{"allow_total_growth": true}
	})
	seed, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, db, seed.ID, model.StatusSucceeded)
	incremental.Store(true)
	count.Store(12)
	maxPages, knownPages := 1, 2
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeIncremental, MaxPages: &maxPages, KnownPages: &knownPages})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := manager.registry.Get(provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider.HTTP.IncrementalQuery["order"] = "asc"
	saveScheduledProvider(t, manager.registry, provider, doc.Revision)
	for page := 1; page <= 5; page++ {
		if page > 1 {
			if _, err := manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
		}
		status := model.StatusPaused
		if page == 5 {
			status = model.StatusSucceeded
		}
		run = collectScheduledAttempt(t, ctx, manager, db, run.ID, status)
		if run.Pages != page {
			t.Fatalf("resume skipped or replayed a committed page: %+v", run)
		}
	}
	for _, want := range []int{0, 2, 4, 6, 8} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("incremental stopped on old rows or crossed its boundary: got %d want %d", got, want)
			}
		default:
			t.Fatalf("missing incremental request at offset %d", want)
		}
	}
	published, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 12 {
		t.Fatalf("incremental failed to preserve old records and discover every initial new identity: %+v %v", published, err)
	}
	seen := make(map[string]bool, len(published.Items))
	for _, record := range published.Items {
		seen[record.SourceID] = true
	}
	for id := 1; id <= 12; id++ {
		if !seen[strconv.Itoa(id)] {
			t.Fatalf("native identity %d was omitted by the ordering boundary", id)
		}
	}
}
