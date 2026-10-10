package store

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func seedCollectionBaseline(t *testing.T, ctx context.Context, db *Store, provider model.Provider) {
	t.Helper()
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	saved, err := db.SavePage(ctx, run, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, saved, model.StatusSucceeded)
}

func TestInitialFullRequiresSuccessfulDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name        string
		priorMode   model.RunMode
		priorStatus model.RunStatus
		requested   model.RunMode
		want        model.RunMode
	}{
		{name: "first Incremental", requested: model.ModeIncremental, want: model.ModeFull},
		{name: "Preview is not a baseline", priorMode: model.ModePreview, priorStatus: model.StatusSucceeded, requested: model.ModeIncremental, want: model.ModeFull},
		{name: "Metadata is not a baseline", priorMode: model.ModeMetadata, priorStatus: model.StatusSucceeded, requested: model.ModeIncremental, want: model.ModeFull},
		{name: "failed Full", priorMode: model.ModeFull, priorStatus: model.StatusFailed, requested: model.ModeIncremental, want: model.ModeFull},
		{name: "cancelled Full", priorMode: model.ModeFull, priorStatus: model.StatusCancelled, requested: model.ModeIncremental, want: model.ModeFull},
		{name: "paused Full", priorMode: model.ModeFull, priorStatus: model.StatusPaused, requested: model.ModeIncremental, want: model.ModeFull},
		{name: "successful empty Full", priorMode: model.ModeFull, priorStatus: model.StatusSucceeded, requested: model.ModeIncremental, want: model.ModeIncremental},
		{name: "legacy successful Incremental", priorMode: model.ModeIncremental, priorStatus: model.StatusSucceeded, requested: model.ModeIncremental, want: model.ModeIncremental},
		{name: "explicit Full", requested: model.ModeFull, want: model.ModeFull},
		{name: "explicit Preview", requested: model.ModePreview, want: model.ModePreview},
		{name: "explicit Metadata", requested: model.ModeMetadata, want: model.ModeMetadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			provider := metadataSource("initial")
			provider.Schedule.KnownPages = 7
			if tc.priorMode != "" {
				prior := mirrorClaim(t, ctx, db, provider, tc.priorMode)
				var err error
				prior, err = db.SavePage(ctx, prior, model.Page{Done: true}, "")
				if err != nil {
					t.Fatal(err)
				}
				reason := model.PauseReason("")
				if tc.priorStatus == model.StatusPaused {
					reason = model.PauseBudget
				}
				if _, err := db.FinishRun(ctx, prior.ID, tc.priorStatus, "", reason); err != nil {
					t.Fatal(err)
				}
			}
			run, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: tc.requested, MaxPages: 2, Revision: "snapshot"})
			wantInitial := tc.requested == model.ModeIncremental && tc.want == model.ModeFull
			if err != nil || run.Mode != tc.want || run.InitialFull != wantInitial || run.MaxPages != 2 || run.Config.Schedule.KnownPages != 7 || run.Revision != "snapshot" {
				t.Fatalf("incorrect initial collection or changed attempt settings: %+v %v", run, err)
			}
			events, err := db.Events(ctx, run.ID, model.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			initialEvents := 0
			for _, event := range events.Items {
				if event.Kind == "initial_full" {
					initialEvents++
				}
			}
			if (initialEvents == 1) != wantInitial {
				t.Fatalf("initial Full explanation missing or unexpected: %+v", events)
			}
		})
	}
}

func TestInitialFullManualResumeSurvivesRestartAndPublishesAtomically(t *testing.T) {
	ctx, endpoint, db := scheduleDatabase(t)
	provider := model.Provider{ID: "initial-resume", Adapter: "http_json", Schedule: model.Schedule{KnownPages: 3}}
	queued, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental, MaxPages: 1, Revision: "frozen"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != queued.ID {
		t.Fatalf("claim initial Full: %+v %v", run, err)
	}
	cursor := []byte(`{"page":2}`)
	saved, err := db.SavePage(ctx, *run, model.Page{Items: []model.Record{coverageRecord("first")}, Next: cursor}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, saved.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	if rows := mirrorRows(t, ctx, db, provider.ID); len(rows) != 0 {
		t.Fatal("initial Full published a partial catalogue")
	}
	db.Close()
	restarted, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if _, err := restarted.ResumeRun(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	run, err = restarted.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != queued.ID || !run.InitialFull || run.Mode != model.ModeFull || !reflect.DeepEqual(run.Cursor, saved.Cursor) || !reflect.DeepEqual(run.Config, saved.Config) || !reflect.DeepEqual(run.Policy, saved.Policy) {
		t.Fatalf("restart changed initial Full continuation: %+v %v", run, err)
	}
	saved, err = restarted.SavePage(ctx, *run, model.Page{Items: []model.Record{coverageRecord("second")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, restarted, saved, model.StatusSucceeded)
	if rows := mirrorRows(t, ctx, restarted, provider.ID); len(rows) != 2 {
		t.Fatalf("completed Full did not publish all pages: %+v", rows)
	}
	next, err := restarted.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || next.Mode != model.ModeIncremental || next.InitialFull {
		t.Fatalf("successful Full did not enable Incremental: %+v %v", next, err)
	}
}

func TestInitialFullConcurrentAdmissionAndSecretRollback(t *testing.T) {
	ctx, endpoint, db := scheduleDatabase(t)
	provider := model.Provider{ID: "concurrent-initial", Adapter: "http_json", Auth: model.Auth{Type: "bearer", SecretRef: "missing"}}
	if _, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental}); err == nil {
		t.Fatal("missing secret accepted")
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{})
	if err != nil || runs.Total != 0 {
		t.Fatalf("failed preparation left queued work: %+v %v", runs, err)
	}
	provider.Auth = model.Auth{}
	second, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	var wg sync.WaitGroup
	type result struct {
		run model.Run
		err error
	}
	results := make(chan result, 12)
	for i := range 12 {
		instance := db
		if i%2 == 0 {
			instance = second
		}
		wg.Go(func() {
			run, err := instance.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
			results <- result{run, err}
		})
	}
	wg.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result.err == nil {
			accepted++
			if result.run.Mode != model.ModeFull || !result.run.InitialFull {
				t.Fatalf("race bypassed initial Full: %+v", result.run)
			}
		} else if !errors.Is(result.err, model.ErrBusy) {
			t.Fatal(result.err)
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent requests created %d initial collections", accepted)
	}
}

func TestInitialFullRemoteUsesExactEndpointCheckpoint(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	provider := mirrorSource("remote-initial")
	initial, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || initial.Mode != model.ModeFull || !initial.InitialFull {
		t.Fatalf("remote without checkpoint did not select Full: %+v %v", initial, err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != initial.ID {
		t.Fatalf("claim remote: %+v %v", run, err)
	}
	saved := mirrorPage(t, ctx, db, *run, "full", "checkpoint", true)
	mirrorFinish(t, ctx, db, saved, model.StatusSucceeded)
	next, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || next.Mode != model.ModeIncremental || next.InitialFull {
		t.Fatalf("valid checkpoint ignored: %+v %v", next, err)
	}
	if _, err := db.RequestCancel(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	provider.URL += "/other"
	next, err = db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental})
	if err != nil || next.Mode != model.ModeFull || !next.InitialFull {
		t.Fatalf("changed remote endpoint reused old baseline: %+v %v", next, err)
	}
}

func TestInitialFullScheduledContinuationWithoutRoutine(t *testing.T) {
	ctx, endpoint, db := scheduleDatabase(t)
	source := routineSource(t, "")
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, queued, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || queued == nil || !queued.InitialFull || queued.Mode != model.ModeFull {
		t.Fatalf("first scheduled Incremental was not Full: %+v %v", queued, err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != queued.ID {
		t.Fatalf("claim scheduled initial Full: %+v %v", run, err)
	}
	saved, err := db.SavePage(ctx, *run, model.Page{Items: []model.Record{coverageRecord("initial")}, Next: []byte(`{"page":2}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, saved.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	restarted, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	_, continued, err := restarted.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil)
	if err != nil || continued == nil || continued.ID != queued.ID || !continued.InitialFull || !reflect.DeepEqual(continued.Cursor, saved.Cursor) {
		t.Fatalf("scheduler replaced or blocked initial Full: %+v %v", continued, err)
	}
	run, err = restarted.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != queued.ID {
		t.Fatalf("claim resumed initial Full: %+v %v", run, err)
	}
	saved, err = restarted.SavePage(ctx, *run, model.Page{Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, restarted, saved, model.StatusSucceeded)
	_, next, err := restarted.AttemptSchedule(ctx, source, start.Add(3*time.Minute), nil)
	if err != nil || next == nil || next.ID == queued.ID || next.Mode != model.ModeIncremental || next.InitialFull {
		t.Fatalf("scheduler did not transition to Incremental: %+v %v", next, err)
	}
}

func TestInitialFullPreservesIncrementalMetadataFollowup(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	provider := metadataSource("initial-metadata")
	provider.Traversal.MetadataAfterIncremental = true
	initial, err := db.EnqueueRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeIncremental, MaxPages: 2, Revision: "frozen"})
	if err != nil || !initial.InitialFull {
		t.Fatalf("initial Full missing: %+v %v", initial, err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != initial.ID {
		t.Fatalf("claim initial Full: %+v %v", run, err)
	}
	saved, err := db.SavePage(ctx, *run, model.Page{Items: []model.Record{coverageRecord("10"), coverageRecord("20")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, saved, model.StatusSucceeded)
	children := metadataFollowups(t, ctx, db, saved)
	if len(children) != 1 || children[0].Mode != model.ModeMetadata || children[0].InitialFull || children[0].Revision != initial.Revision || children[0].MaxPages != initial.MaxPages || !reflect.DeepEqual(children[0].Config, initial.Config) || !reflect.DeepEqual(children[0].Policy, initial.Policy) {
		t.Fatalf("initial Full lost or duplicated the configured follow-up: %+v", children)
	}
	candidates, err := db.MetadataCandidates(ctx, children[0], "", 100)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("initial torrents omitted from enrichment: %+v %v", candidates, err)
	}
}
