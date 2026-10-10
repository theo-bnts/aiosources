package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func awaitJobSignal(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-deadline.Done():
		t.Fatal("worker boundary did not complete:", deadline.Err())
	}
}

func TestManualPauseCommitsInFlightResponseAndResumesWithoutRefetch(t *testing.T) {
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("final=%t", final), func(t *testing.T) {
			ctx, _, db := schedulerDatabase(t)
			entered, release, interrupted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			requests := make(chan string, 8)
			first := `{"items":[{"id":"first","title":"Original first"}],"total":2}`
			if final {
				first = `{"items":[{"id":"first","title":"Original first"}],"total":1}`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset := r.URL.Query().Get("offset")
				requests <- offset
				w.Header().Set("Content-Type", "application/json")
				if offset == "0" {
					close(entered)
					select {
					case <-release:
						_, _ = w.Write([]byte(first))
					case <-r.Context().Done():
						close(interrupted)
					}
					return
				}
				_, _ = w.Write([]byte(`{"items":[{"id":"second","title":"Original second"}],"total":2}`))
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			provider := schedulerProvider("manual-pause", server.URL)
			provider.Schedule = model.Schedule{}
			doc := saveScheduledProvider(t, registry, provider, "")
			manager := New(db, registry, nil, 1)
			manager.ctx, manager.connected = ctx, true
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			finished := make(chan struct{})
			go func() { manager.runClaim(*claimed); close(finished) }()
			awaitJobSignal(t, ctx, entered)
			for range 2 {
				held, err := manager.Pause(ctx, run.ID)
				if err != nil || held.Status != model.StatusRunning || !held.PauseRequested || held.CancelRequested {
					t.Fatalf("running pause did not remain pending: %+v %v", held, err)
				}
			}
			select {
			case <-interrupted:
				t.Fatal("manual pause cancelled the in-flight HTTP response")
			default:
			}
			releaseOnce.Do(func() { close(release) })
			awaitJobSignal(t, ctx, finished)
			held, err := db.GetRun(ctx, run.ID)
			if err != nil || held.Status != model.StatusPaused || held.PauseReason != model.PauseManual || !held.PauseRequested || held.Pages != 1 || held.Records != 1 || held.Errors != 0 || held.TraversalDone != final {
				t.Fatalf("in-flight response did not commit before pause: %+v %v", held, err)
			}
			if firstOffset := <-requests; firstOffset != "0" {
				t.Fatalf("initial offset=%q", firstOffset)
			}
			select {
			case offset := <-requests:
				t.Fatalf("pause allowed another fetch at offset %s", offset)
			default:
			}
			raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || raw.Total != 1 {
				t.Fatalf("pause lost original record: %+v %v", raw, err)
			}
			body, _, err := db.RawPage(ctx, raw.Items[0].PageID)
			if !errors.Is(err, model.ErrNotFound) || len(body) != 0 || raw.Items[0].PayloadRetained {
				t.Fatalf("pause retained an unwanted source payload: %v", err)
			}
			if live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID}); err != nil || live.Total != 0 {
				t.Fatalf("held traversal published early: %+v %v", live, err)
			}
			provider.Name = "Edited while paused"
			saveScheduledProvider(t, registry, provider, doc.Revision)
			resumed, err := manager.Resume(ctx, run.ID)
			if err != nil || resumed.PauseRequested || resumed.Config.Name != doc.Provider.Name || !bytes.Equal(resumed.Cursor, held.Cursor) {
				t.Fatalf("resume replaced the saved snapshot/checkpoint: %+v %v", resumed, err)
			}
			claimed, err = db.ClaimNext(ctx)
			if err != nil || claimed == nil {
				t.Fatalf("resume claim: %+v %v", claimed, err)
			}
			manager.runClaim(*claimed)
			completed, err := db.GetRun(ctx, run.ID)
			wantPages := 2
			if final {
				wantPages = 1
			}
			if err != nil || completed.Status != model.StatusSucceeded || completed.PauseRequested || completed.Pages != wantPages || completed.Records != wantPages {
				t.Fatalf("resume did not complete exact remaining work: %+v %v", completed, err)
			}
			if !final {
				if offset := <-requests; offset != "1" {
					t.Fatalf("resume refetched committed page at offset %s", offset)
				}
			}
			select {
			case offset := <-requests:
				t.Fatalf("completed checkpoint refetched at offset %s", offset)
			default:
			}
			if live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID}); err != nil || live.Total != int64(wantPages) {
				t.Fatalf("resumed completion did not publish: %+v %v", live, err)
			}
		})
	}
}

func TestCancellationInterruptsHTTPAfterAcceptedManualPause(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	entered, interrupted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			close(interrupted)
		case <-release:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("pause-then-cancel", server.URL)
	provider.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, provider, "")
	manager := New(db, registry, nil, 1)
	manager.ctx, manager.connected = ctx, true
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	finished := make(chan struct{})
	go func() { manager.runClaim(*claimed); close(finished) }()
	awaitJobSignal(t, ctx, entered)
	if _, err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if cancelled, err := manager.Cancel(ctx, run.ID); err != nil || cancelled.PauseRequested || !cancelled.CancelRequested {
		t.Fatalf("cancel did not supersede pending pause: %+v %v", cancelled, err)
	}
	awaitJobSignal(t, ctx, interrupted)
	awaitJobSignal(t, ctx, finished)
	cancelled, err := db.GetRun(ctx, run.ID)
	if err != nil || cancelled.Status != model.StatusCancelled || cancelled.PauseRequested || cancelled.Pages != 0 || len(cancelled.Cursor) != 0 {
		t.Fatalf("cancel committed interrupted work: %+v %v", cancelled, err)
	}
	if _, err := manager.Pause(ctx, run.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("late pause overwrote cancelled run: %v", err)
	}
}

func TestBodylessCoverageFailureRetainsSafeScopeEvidence(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	provider := model.Provider{ID: "coverage-evidence", Traversal: &model.JSONTraversal{MinimumTotal: 67590, Scopes: []model.JSONScope{{ID: "subcat_2"}, {ID: "subcat_6"}}}}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, nil, nil, 1)
	page := model.Page{Metadata: map[string]any{
		"coverage_incomplete": true,
		"coverage_expected":   map[string]int64{"subcat_2": 14343, "subcat_6": 53247, "private-secret": 7},
		"coverage_observed":   map[string]int64{"subcat_2": 14344, "subcat_6": 53206},
		"upstream_error":      "private-secret", "Authorization": "private-secret",
	}}
	if err := manager.archiveFailure(ctx, run, page, "upstream private-secret", "stalled"); err != nil {
		t.Fatal(err)
	}
	events, err := db.Events(ctx, run.ID, model.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var failure *model.Event
	for i := range events.Items {
		if events.Items[i].Kind == "source_error" {
			failure = &events.Items[i]
		}
	}
	if failure == nil || failure.Data["coverage_incomplete"] != true || failure.Data["failure_code"] != "stalled" || failure.Data["minimum_total"] != json.Number("67590") {
		t.Fatalf("bodyless completeness failure became generic: %+v", failure)
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-secret") || strings.Contains(string(encoded), "Authorization") || strings.Contains(string(encoded), "upstream_error") {
		t.Fatalf("private metadata escaped: %s", encoded)
	}
	for key, want := range map[string]map[string]json.Number{
		"coverage_expected": {"subcat_2": "14343", "subcat_6": "53247"},
		"coverage_observed": {"subcat_2": "14344", "subcat_6": "53206"},
	} {
		counts, ok := failure.Data[key].(map[string]any)
		if !ok || len(counts) != len(want) {
			t.Fatalf("missing structured counts: %+v", failure.Data)
		}
		for scope, expected := range want {
			if counts[scope] != expected {
				t.Fatalf("%s[%s]=%v, want %v", key, scope, counts[scope], expected)
			}
		}
	}
}

func TestShutdownAndRestartPreserveAcceptedManualHold(t *testing.T) {
	ctx, endpoint, db := schedulerDatabase(t)
	entered, retry, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	requests := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		requests <- offset
		w.Header().Set("Content-Type", "application/json")
		if offset == "0" {
			_, _ = w.Write([]byte(`{"items":[{"id":"first"}],"total":2}`))
			return
		}
		select {
		case <-retry:
			_, _ = w.Write([]byte(`{"items":[{"id":"second"}],"total":2}`))
		default:
			close(entered)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("shutdown-hold", server.URL)
	provider.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, provider, "")
	firstManager := New(db, registry, nil, 1)
	if err := firstManager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := firstManager.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	run, err := firstManager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	awaitJobSignal(t, ctx, entered)
	if _, err := firstManager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	shutdown, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := firstManager.Close(shutdown); err != nil {
		t.Fatal(err)
	}
	held, err := db.GetRun(ctx, run.ID)
	if err != nil || held.Status != model.StatusPaused || !held.PauseRequested || held.Pages != 1 || held.Records != 1 {
		t.Fatalf("shutdown cleared the manual hold or committed interrupted work: %+v %v", held, err)
	}
	if first, second := <-requests, <-requests; first != "0" || second != "1" {
		t.Fatalf("unexpected pre-shutdown traversal: %s %s", first, second)
	}
	db.Close()
	restarted, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	close(retry)
	restartedManager := New(restarted, registry, nil, 1)
	if err := restartedManager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restartedManager.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	recovered, err := restarted.GetRun(ctx, run.ID)
	if err != nil || recovered.Status != model.StatusPaused || !recovered.PauseRequested || !bytes.Equal(recovered.Cursor, held.Cursor) {
		t.Fatalf("manager restart released held checkpoint: %+v %v", recovered, err)
	}
	notices, unsubscribe := restartedManager.Subscribe()
	defer unsubscribe()
	if _, err := restartedManager.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completion, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for {
		finished, err := restarted.GetRun(completion, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if finished.Status == model.StatusSucceeded {
			if finished.PauseRequested || finished.Pages != 2 || finished.Records != 2 {
				t.Fatalf("resumed shutdown checkpoint was not continued exactly: %+v", finished)
			}
			break
		}
		if finished.Status != model.StatusQueued && finished.Status != model.StatusRunning {
			t.Fatalf("resumed work stopped unexpectedly: %+v", finished)
		}
		select {
		case <-notices:
		case <-completion.Done():
			t.Fatal("resumed work did not finish:", completion.Err())
		}
	}
	if offset := <-requests; offset != "1" {
		t.Fatalf("shutdown/resume refetched committed page at offset %s", offset)
	}
	select {
	case offset := <-requests:
		t.Fatalf("unexpected extra request after shutdown/resume: %s", offset)
	default:
	}
}

func TestNotificationRecoveryRequeuesAfterLateAttemptCleanup(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"retained","title":"Recovered"}],"total":1}`))
	}, nil)
	settings, err := db.CollectionSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateCollectionSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	manager.ctx, manager.cancel = context.WithCancelCause(ctx)
	manager.connected = true
	changes, unsubscribe, err := db.SubscribeChanges(manager.ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager.workers.Add(1)
	go manager.observeChanges(changes, unsubscribe)
	t.Cleanup(func() {
		manager.cancel(errShutdown)
		manager.workers.Wait()
	})
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	finished := make(chan struct{})
	manager.workers.Add(1)
	go func() {
		defer manager.workers.Done()
		manager.runClaim(*claimed)
		close(finished)
	}()
	awaitJobSignal(t, ctx, entered)
	// The reconnect pass can finish while the interrupted attempt still owns
	// its provider lease. Its later durable finish must trigger another pass.
	if err := manager.recoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	attempt := manager.active[run.ID]
	manager.mu.Unlock()
	if attempt == nil {
		t.Fatal("source request lost its active attempt")
	}
	attempt.cancel(errNotifications)
	awaitJobSignal(t, ctx, finished)
	notices, stopNotices := manager.Subscribe()
	defer stopNotices()
	recovered, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	for {
		saved, err := db.GetRun(recovered, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Status == model.StatusQueued {
			if saved.Pages != 0 || len(saved.Cursor) != 0 || saved.TraversalDone {
				t.Fatalf("technical recovery advanced interrupted work: %+v", saved)
			}
			break
		}
		select {
		case <-notices:
		case <-recovered.Done():
			t.Fatalf("late interrupted cleanup stranded the run: %+v", saved)
		}
	}
	completed := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
	if completed.Pages != 1 || completed.DistinctRecords != 1 || requests.Load() != 2 {
		t.Fatalf("technical recovery lost or replayed work: %+v requests=%d", completed, requests.Load())
	}
}

func TestAutomaticRecoveryCannotReadmitDeletedOrDisabledProvider(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%t", deleted), func(t *testing.T) {
			var requests atomic.Int32
			ctx, db, manager, provider := policyManager(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
			}, nil)
			settings, err := db.CollectionSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateCollectionSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			committed, err := db.SavePage(ctx, *claimed, model.Page{Next: json.RawMessage(`{"offset":1}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "Interrupted", model.PauseInterrupted); err != nil {
				t.Fatal(err)
			}
			doc, err := manager.registry.Get(provider.ID)
			if err != nil {
				t.Fatal(err)
			}
			if deleted {
				if err := manager.registry.Delete(provider.ID, doc.Revision, func() error {
					active, err := db.HasActiveRun(ctx, provider.ID)
					if err != nil {
						return err
					}
					if active {
						return model.ErrBusy
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				provider.Enabled = false
				saveScheduledProvider(t, manager.registry, provider, doc.Revision)
			}
			if err := manager.Start(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := manager.Close(shutdown); err != nil {
					t.Error(err)
				}
			})
			saved, err := db.GetRun(ctx, run.ID)
			if err != nil || saved.Status != model.StatusPaused || saved.PauseReason != model.PauseInterrupted || !bytes.Equal(saved.Cursor, committed.Cursor) || saved.Revision != run.Revision {
				t.Fatalf("recovery admitted unavailable source or lost its checkpoint: %+v %v", saved, err)
			}
			if requests.Load() != 0 {
				t.Fatalf("unavailable source was requested %d times", requests.Load())
			}
		})
	}
}
