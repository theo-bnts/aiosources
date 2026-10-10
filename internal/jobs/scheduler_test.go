package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/scheduling"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
)

func schedulerDatabase(t *testing.T) (context.Context, string, *store.Store) {
	t.Helper()
	ctx, url := testutil.NewDatabase(t)
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, url, db
}

func schedulerProvider(id, endpoint string) model.Provider {
	return model.Provider{Version: 1, ID: id, Name: id, Adapter: "http_json", URL: endpoint, Enabled: true,
		Auth: model.Auth{Type: "none"}, RequestInterval: "1ms", RequestTimeout: "30s", RateLimitReset: "epoch", PageSize: 1,
		HTTP:       model.HTTPConfig{Method: "GET", ItemsPath: "/items"},
		Pagination: model.Pagination{Type: "offset", In: "query", OffsetParam: "offset", SizeParam: "limit", TotalPath: "/total"},
		Mapping:    model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title"}},
		Output:     model.Output{Fields: []string{"title"}},
		Schedule:   model.Schedule{Every: "1m", Mode: model.ModeFull, MaxPages: 1}}
}

func saveScheduledProvider(t *testing.T, registry *providers.Registry, p model.Provider, revision string) model.ProviderDocument {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := registry.Save(p.ID, string(raw), revision)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func collectScheduledAttempt(t *testing.T, ctx context.Context, manager *Manager, db *store.Store, id string, want model.RunStatus) model.Run {
	t.Helper()
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != id {
		t.Fatalf("claim scheduled attempt: %+v %v", run, err)
	}
	status, message, reason := manager.collect(ctx, *run)
	finished, err := db.FinishRun(ctx, id, status, message, reason)
	if err != nil || finished.Status != want {
		t.Fatalf("scheduled attempt finished as %s, want %s: %s %v", finished.Status, want, message, err)
	}
	return finished
}

func TestScheduledBudgetContinuesSnapshotAfterRestart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edited  bool
		routine bool
	}{
		{name: "automatic continuation"},
		{name: "changed revision blocks until manual resume", edited: true},
		{name: "routine Full continuation", routine: true},
		{name: "routine revision blocks Full continuation", edited: true, routine: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, url, db := schedulerDatabase(t)
			requests := make(chan string, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset := r.URL.Query().Get("offset")
				requests <- offset
				w.Header().Set("Content-Type", "application/json")
				switch offset {
				case "0":
					_, _ = w.Write([]byte(`{"items":[{"id":"first","title":"First"}],"total":2}`))
				case "1":
					_, _ = w.Write([]byte(`{"items":[{"id":"second","title":"Second"}],"total":2}`))
				default:
					http.Error(w, "unexpected offset", http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("budget-source", server.URL)
			if tc.routine {
				p.Schedule.Mode = model.ModeIncremental
				p.Schedule.FullEvery = "1m"
			}
			doc := saveScheduledProvider(t, registry, p, "")
			manager := New(db, registry, nil, 1)
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			firstDue, err := manager.scheduleDue(ctx, start)
			if err != nil || !firstDue.Equal(start.Add(time.Minute)) {
				t.Fatalf("initial tick did not wait one interval: %s %v", firstDue, err)
			}
			if _, err := manager.scheduleDue(ctx, firstDue); err != nil {
				t.Fatal(err)
			}
			latest, err := db.LatestRuns(ctx, []string{p.ID})
			if err != nil {
				t.Fatal(err)
			}
			first := latest[p.ID]
			paused := collectScheduledAttempt(t, ctx, manager, db, first.ID, model.StatusPaused)
			if paused.Pages != 1 || paused.TraversalDone || paused.Trigger != model.TriggerScheduled {
				t.Fatalf("page budget was treated as traversal completion: %+v", paused)
			}
			live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
			if err != nil || live.Total != 0 {
				t.Fatalf("incomplete full traversal published rows: %+v %v", live, err)
			}
			settings, err := db.CollectionSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			settings.MaxQuotaRetries = (settings.MaxQuotaRetries + 1) % 11
			if err := db.UpdateCollectionSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			db.Close()
			restarted, err := store.Open(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			manager = New(restarted, registry, nil, 1)
			if tc.edited {
				p.Name = "Edited source"
				p.Schedule.MaxPages = 2
				saveScheduledProvider(t, registry, p, doc.Revision)
				summaries, err := manager.Schedules(ctx)
				if err != nil || len(summaries) != 1 || summaries[0].State != "blocked" || summaries[0].NextRunAt == nil {
					t.Fatalf("old paused snapshot was not actionable: %+v %v", summaries, err)
				}
				if _, err := manager.scheduleDue(ctx, *summaries[0].NextRunAt); err != nil {
					t.Fatal(err)
				}
				stillPaused, err := restarted.GetRun(ctx, first.ID)
				if err != nil || stillPaused.Status != model.StatusPaused {
					t.Fatalf("edited snapshot was automatically resumed: %+v %v", stillPaused, err)
				}
				if _, err := manager.Resume(ctx, first.ID); err != nil {
					t.Fatalf("manual recovery of immutable old snapshot failed: %v", err)
				}
			} else {
				next, err := manager.scheduleDue(ctx, firstDue.Add(30*time.Second))
				if err != nil || !next.Equal(firstDue.Add(time.Minute)) {
					t.Fatalf("restart moved the next continuation: %s %v", next, err)
				}
				if _, err := manager.scheduleDue(ctx, next); err != nil {
					t.Fatal(err)
				}
			}
			finished := collectScheduledAttempt(t, ctx, manager, restarted, first.ID, model.StatusSucceeded)
			if finished.Pages != 2 || finished.MaxPages != 1 || finished.Mode != model.ModeFull || finished.Config.Name != doc.Provider.Name || finished.Revision != doc.Revision || finished.EffectivePolicy() != paused.EffectivePolicy() {
				t.Fatalf("continuation replaced its immutable snapshot or checkpoint: %+v", finished)
			}
			live, err = restarted.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
			if err != nil || live.Total != 2 {
				t.Fatalf("completed full traversal was not published: %+v %v", live, err)
			}
			if firstOffset, secondOffset := <-requests, <-requests; firstOffset != "0" || secondOffset != "1" {
				t.Fatalf("continuation refetched committed pages: %s %s", firstOffset, secondOffset)
			}
			select {
			case offset := <-requests:
				t.Fatalf("unexpected extra request at offset %s", offset)
			default:
			}
			runs, err := restarted.ListRuns(ctx, model.ListOptions{ProviderID: p.ID})
			if err != nil || runs.Total != 1 {
				t.Fatalf("continuation created another traversal: %+v %v", runs, err)
			}
		})
	}
}

func TestScheduleSummariesReconcileDisabledManualAndInvalidSources(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	dir := t.TempDir()
	registry, err := providers.New(dir, connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("configured", "https://source.example.invalid/api")
	doc := saveScheduledProvider(t, registry, p, "")
	manual := schedulerProvider("manual", p.URL)
	manual.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, manual, "")
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"invalid":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := New(db, registry, nil, 1)
	start := time.Now().UTC().Truncate(time.Second)
	if _, err := manager.scheduleDue(ctx, start); err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	doc = saveScheduledProvider(t, registry, p, doc.Revision)
	summaries, err := manager.Schedules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]model.ScheduleSummary{}
	for _, summary := range summaries {
		states[summary.ProviderID] = summary
	}
	if len(states) != 3 || states["manual"].State != "manual" || states["broken"].State != "invalid" {
		t.Fatalf("manual/invalid definitions were omitted: %+v", states)
	}
	configured := states[p.ID]
	if configured.ProviderEnabled || !configured.Enabled || configured.State != "disabled" || configured.NextRunAt != nil {
		t.Fatalf("provider activation was conflated with schedule activation: %+v", configured)
	}
	p.Enabled = true
	disabled := false
	p.Schedule.Enabled = &disabled
	doc = saveScheduledProvider(t, registry, p, doc.Revision)
	if _, err := manager.scheduleDue(ctx, start.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || runs.Total != 0 {
		t.Fatalf("disabled schedule queued a collection: %+v %v", runs, err)
	}
	p.Schedule.Enabled = nil
	saveScheduledProvider(t, registry, p, doc.Revision)
	reenabled := start.Add(48 * time.Hour)
	next, err := manager.scheduleDue(ctx, reenabled)
	if err != nil || !next.Equal(reenabled.Add(time.Minute)) {
		t.Fatalf("reenabling did not establish a fresh clock: %s %v", next, err)
	}
}

func TestExternalDefinitionChangesWakeAnIdleScheduler(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	dir := t.TempDir()
	registry, err := providers.New(dir, connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
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
	changes, stop, err := db.SubscribeChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stopSubscription(changes, stop)
	notices, stopNotices := manager.Subscribe()
	defer stopNotices()
	// There is initially no schedule and therefore no timer deadline. Only
	// a real filesystem event can cause the database clock to appear.
	p := schedulerProvider("external", "https://source.example.invalid/api")
	awaitDefinitionNotice := func() {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case notice, ok := <-notices:
				if !ok {
					t.Fatal("manager stopped before observing the external edit")
				}
				if notice.Kind == "providers" {
					return
				}
			case <-deadline.C:
				t.Fatal("external edit did not emit a definition-change notice")
			}
		}
	}
	replace := func() model.ProviderDocument {
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		temporary := filepath.Join(dir, "operator-update.tmp")
		if err := os.WriteFile(temporary, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporary, filepath.Join(dir, "arbitrary-filename.json")); err != nil {
			t.Fatal(err)
		}
		doc, err := registry.Get(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		awaitDefinitionNotice()
		return doc
	}
	awaitClock := func(doc model.ProviderDocument, active bool) {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		source := scheduling.Sources(map[string]model.ProviderDocument{p.ID: doc})[0]
		for {
			select {
			case _, ok := <-changes:
				if !ok {
					t.Fatal("database notification stream closed")
				}
				// Unlike GET /api/schedules, AttemptSchedule does not create or
				// reconcile clocks, so this cannot mask a missing file watcher.
				clock, run, err := db.AttemptSchedule(ctx, source, time.Now(), nil)
				if err == nil && clock.Revision == doc.Revision && clock.Active == active {
					if run != nil || (clock.NextRunAt != nil) != active {
						t.Fatalf("external edit left an invalid clock: %+v %+v", clock, run)
					}
					return
				}
			case <-manager.Done():
				t.Fatal("manager stopped while observing an external edit")
			case <-deadline.C:
				t.Fatal("external edit did not reconcile the durable clock")
			}
		}
	}
	awaitClock(replace(), true)
	p.Enabled = false
	awaitClock(replace(), false)
}

func TestScheduledSecretsResolveWithOneAvailableConnection(t *testing.T) {
	ctx, databaseURL, initial := schedulerDatabase(t)
	initial.Close()
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Set("pool_max_conns", "3")
		parsed.RawQuery = query.Encode()
		databaseURL = parsed.String()
	} else {
		databaseURL += " pool_max_conns=3"
	}
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	secrets, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.Put(ctx, "source-token", "local-test-token"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"busy-one", "busy-two"} {
		p := schedulerProvider(id, "https://source.example.invalid/api")
		if _, err := db.CreateRun(ctx, model.Run{ProviderID: id, Config: p, Mode: model.ModePreview}); err != nil {
			t.Fatal(err)
		}
		if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ProviderID != id {
			t.Fatalf("claim concurrent collection: %+v %v", claimed, err)
		}
	}
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("authenticated", "https://source.example.invalid/api")
	p.Auth = model.Auth{Type: "bearer", SecretRef: "source-token"}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, secrets.Resolve, 1)
	start := time.Now().UTC()
	due, err := manager.scheduleDue(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := manager.scheduleDue(bounded, due); err != nil {
		t.Fatalf("secret resolution deadlocked the shared connection pool: %v", err)
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || runs.Total != 1 || runs.Items[0].Status != model.StatusQueued {
		t.Fatalf("authenticated schedule did not queue: %+v %v", runs, err)
	}
}

func TestManualAdmissionSerializesProviderDeletion(t *testing.T) {
	for _, resume := range []bool{false, true} {
		name := "enqueue"
		if resume {
			name = "resume"
		}
		t.Run(name, func(t *testing.T) {
			ctx, _, db := schedulerDatabase(t)
			dir := t.TempDir()
			registry, err := providers.New(dir, connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			provider := schedulerProvider("admission", "https://source.example.invalid/api")
			provider.Schedule = model.Schedule{}
			provider.Auth = model.Auth{Type: "bearer", SecretRef: "source-token"}
			doc := saveScheduledProvider(t, registry, provider, "")
			if err := os.WriteFile(filepath.Join(dir, "unrelated-broken.json"), []byte(`{"invalid":[`), 0o600); err != nil {
				t.Fatal(err)
			}
			secrets, err := vault.New(ctx, db, make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			if err := secrets.Put(ctx, "source-token", "local-test-token"); err != nil {
				t.Fatal(err)
			}
			manager := New(db, registry, secrets.Resolve, 1)
			var paused model.Run
			if resume {
				paused, err = manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := manager.Pause(ctx, paused.ID); err != nil {
					t.Fatal(err)
				}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			manager.resolver = func(ctx context.Context, ref string) (string, error) {
				close(entered)
				select {
				case <-release:
					return secrets.Resolve(ctx, ref)
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			type admission struct {
				run model.Run
				err error
			}
			admitted := make(chan admission, 1)
			go func() {
				var run model.Run
				var err error
				if resume {
					run, err = manager.Resume(ctx, paused.ID)
				} else {
					run, err = manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
				}
				admitted <- admission{run: run, err: err}
			}()
			awaitJobSignal(t, ctx, entered)
			deleted := make(chan error, 1)
			go func() {
				deleted <- registry.Delete(provider.ID, doc.Revision, func() error {
					active, err := db.HasActiveRun(ctx, provider.ID)
					if err != nil {
						return err
					}
					if active {
						return model.ErrBusy
					}
					return nil
				})
			}()
			// Admission deliberately holds at secret resolution, before any
			// queued row exists. Deletion must not pass its guard in that gap.
			select {
			case err := <-deleted:
				unblock()
				<-admitted
				t.Fatalf("deletion passed an unfinished admission: %v", err)
			case <-time.After(250 * time.Millisecond):
			}
			unblock()
			select {
			case result := <-admitted:
				if result.err != nil || result.run.Status != model.StatusQueued {
					t.Fatalf("admission failed behind unrelated invalid JSON: %+v %v", result.run, result.err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case err := <-deleted:
				if !errors.Is(err, model.ErrBusy) {
					t.Fatalf("guarded deletion removed an admitted source: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := registry.Get(provider.ID); err != nil {
				t.Fatalf("admitted run lost its source definition: %v", err)
			}
		})
	}
}

func TestIncrementalFullRoutineReconcilesOneSourceIdentity(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		phase := r.URL.Query().Get("phase")
		requests <- phase + ":" + offset
		w.Header().Set("Content-Type", "application/json")
		switch {
		case phase == "incremental" && offset == "0":
			_, _ = w.Write([]byte(`{"items":[{"id":"shared","title":"Shared"}],"total":1}`))
		case phase == "" && offset == "0":
			_, _ = w.Write([]byte(`{"items":[{"id":"shared","title":"Reconciled"}],"total":2}`))
		case phase == "" && offset == "1":
			_, _ = w.Write([]byte(`{"items":[{"id":"full-only","title":"Full only"}],"total":2}`))
		default:
			http.Error(w, "unexpected traversal", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("routine-source", server.URL)
	p.Schedule.Mode, p.Schedule.FullEvery = model.ModeIncremental, "3m"
	p.HTTP.IncrementalQuery = map[string]any{"phase": "incremental"}
	seedCollectionBaseline(t, ctx, db, p)
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := manager.scheduleDue(ctx, start); err != nil {
		t.Fatal(err)
	}
	var fullID, sharedOccurrence string
	for _, step := range []struct {
		tick   int
		mode   model.RunMode
		status model.RunStatus
		total  int64
	}{
		{1, model.ModeIncremental, model.StatusSucceeded, 1},
		{3, model.ModeFull, model.StatusPaused, 1},
		{4, model.ModeFull, model.StatusSucceeded, 2},
		{5, model.ModeIncremental, model.StatusSucceeded, 2},
	} {
		if _, err := manager.scheduleDue(ctx, start.Add(time.Duration(step.tick)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		latest, err := db.LatestRuns(ctx, []string{p.ID})
		if err != nil {
			t.Fatal(err)
		}
		run := latest[p.ID]
		if run.Mode != step.mode {
			t.Fatalf("tick %d selected %s instead of %s", step.tick, run.Mode, step.mode)
		}
		if step.tick == 3 {
			fullID = run.ID
		} else if step.tick == 4 && run.ID != fullID {
			t.Fatal("Full reconciliation lost its budget continuation")
		}
		collectScheduledAttempt(t, ctx, manager, db, run.ID, step.status)
		live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
		if err != nil || live.Total != step.total {
			t.Fatalf("tick %d published an incomplete or duplicate catalogue: %+v %v", step.tick, live, err)
		}
		foundShared := false
		for _, item := range live.Items {
			if item.ProviderID != p.ID {
				t.Fatalf("routine introduced a second source identity: %+v", item)
			}
			if item.SourceID == "shared" {
				if sharedOccurrence == "" {
					sharedOccurrence = item.OccurrenceID
				} else if item.OccurrenceID != sharedOccurrence || foundShared {
					t.Fatal("Full routine replaced or duplicated the stable native occurrence")
				}
				foundShared = true
			}
		}
		if !foundShared {
			t.Fatal("routine lost its established native identity")
		}
	}
	summaries, err := manager.Schedules(ctx)
	if err != nil || len(summaries) != 1 || summaries[0].NextFullAt == nil || !summaries[0].NextFullAt.Equal(start.Add(6*time.Minute)) {
		t.Fatalf("summary lost the Full enqueue deadline: %+v %v", summaries, err)
	}
	for _, want := range []string{"incremental:0", ":0", ":1", "incremental:0"} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("routine requested %q instead of %q", got, want)
			}
		default:
			t.Fatalf("routine omitted request %q", want)
		}
	}
}
