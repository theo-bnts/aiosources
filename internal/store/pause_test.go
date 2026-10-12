package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func TestManualPauseQueuedIsIdempotentAndCancellationWins(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	provider := model.Provider{ID: "queued-pause", Name: "Immutable name"}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		held, err := db.RequestPause(ctx, run.ID)
		if err != nil || held.Status != model.StatusPaused || !held.PauseRequested || held.StartedAt != nil {
			t.Fatalf("queued pause started work: %+v %v", held, err)
		}
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed != nil {
		t.Fatalf("claimed manually held work: %+v %v", claimed, err)
	}
	events, err := db.Events(ctx, run.ID, model.ListOptions{})
	if err != nil || events.Total != 2 || events.Items[0].Kind != "paused" {
		t.Fatalf("repeated pause produced duplicate transitions: %+v %v", events, err)
	}
	resumed, err := db.ResumeRun(ctx, run.ID)
	if err != nil || resumed.PauseRequested || resumed.Status != model.StatusQueued || resumed.Config.Name != provider.Name {
		t.Fatalf("explicit resume did not release immutable work: %+v %v", resumed, err)
	}
	if _, err := db.RequestPause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := db.RequestCancel(ctx, run.ID)
	if err != nil || cancelled.PauseRequested || cancelled.Status != model.StatusCancelled {
		t.Fatalf("cancel did not release manual hold: %+v %v", cancelled, err)
	}
	if _, err := db.RequestPause(ctx, run.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("late pause overwrote cancellation: %v", err)
	}
}

func TestManualPauseFinalPagePreventsPublicationUntilResume(t *testing.T) {
	for _, terminal := range []model.RunStatus{model.StatusSucceeded, model.StatusFailed, model.StatusCancelled} {
		t.Run(string(terminal), func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			// This scenario verifies the optional complete processing history.
			setStorageMode(t, ctx, db, model.StorageFull)
			provider := model.Provider{ID: "final-pause", Name: "Final pause"}
			publishSharingRow(t, ctx, db, provider.ID, "old", `{"title":"Old"}`, nil)
			run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
			body := []byte(`{"items":[{"id":"new","title":"New"}]}`)
			run, err := db.SavePage(ctx, run, model.Page{Body: body, ContentType: "application/json", Done: true,
				Next: json.RawMessage(`{"page":2}`), Items: []model.Record{{SourceID: "new", Raw: []byte(`{"id":"new","title":"New"}`), Fields: map[string]any{"title": "New"}}}}, "final")
			if err != nil {
				t.Fatal(err)
			}
			// The final response is committed, but FinishRun has not yet acquired
			// the publication transaction's row lock. Accepted pause wins here.
			if held, err := db.RequestPause(ctx, run.ID); err != nil || !held.PauseRequested || held.Status != model.StatusRunning {
				t.Fatalf("late pause not accepted: %+v %v", held, err)
			}
			if terminal == model.StatusCancelled {
				if cancelled, err := db.RequestCancel(ctx, run.ID); err != nil || cancelled.PauseRequested || !cancelled.CancelRequested {
					t.Fatalf("cancellation did not supersede pause: %+v %v", cancelled, err)
				}
				if _, err := db.RequestPause(ctx, run.ID); !errors.Is(err, model.ErrConflict) {
					t.Fatalf("pause revived cancellation: %v", err)
				}
			}
			workerStatus := model.StatusSucceeded
			want := model.StatusPaused
			if terminal == model.StatusFailed {
				workerStatus, want = model.StatusFailed, model.StatusFailed
			} else if terminal == model.StatusCancelled {
				want = model.StatusCancelled
			}
			finished, err := db.FinishRun(ctx, run.ID, workerStatus, "", "")
			if err != nil || finished.Status != want || finished.PauseRequested != (want != model.StatusCancelled) {
				t.Fatalf("finalization precedence: %+v %v", finished, err)
			}
			if finished.Pages != run.Pages || finished.Records != run.Records || finished.Errors != run.Errors || finished.DistinctRecords != run.DistinctRecords || !finished.TraversalDone || !bytes.Equal(finished.Cursor, run.Cursor) {
				t.Fatalf("finalization lost committed progress: before=%+v after=%+v", run, finished)
			}
			live := mirrorRows(t, ctx, db, provider.ID)
			if len(live) != 1 || live["old"].SourceID != "old" {
				t.Fatalf("unresumed final traversal published: %+v", live)
			}
			raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || raw.Total != 1 {
				t.Fatalf("original record was lost: %+v %v", raw, err)
			}
			retained, _, err := db.RawPage(ctx, raw.Items[0].PageID)
			if !errors.Is(err, model.ErrNotFound) || len(retained) != 0 || raw.Items[0].PayloadRetained {
				t.Fatalf("final response payload was archived: %q %v", retained, err)
			}
			if terminal != model.StatusSucceeded {
				return
			}
			if _, err := db.ResumeRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil || claimed.PauseRequested || !claimed.TraversalDone || !bytes.Equal(claimed.Cursor, run.Cursor) {
				t.Fatalf("resume lost final checkpoint: %+v %v", claimed, err)
			}
			if completed, err := db.FinishRun(ctx, run.ID, model.StatusSucceeded, "", ""); err != nil || completed.Status != model.StatusSucceeded || completed.PauseRequested {
				t.Fatalf("explicit resume did not publish: %+v %v", completed, err)
			}
			live = mirrorRows(t, ctx, db, provider.ID)
			if len(live) != 1 || live["new"].SourceID != "new" {
				t.Fatalf("completed resumed traversal not published: %+v", live)
			}
			if _, err := db.RequestPause(ctx, run.ID); !errors.Is(err, model.ErrConflict) {
				t.Fatalf("pause accepted after atomic publication: %v", err)
			}
		})
	}
}

func TestManualPauseSurvivesOrphanRecoveryAndScheduledTicks(t *testing.T) {
	ctx, endpoint, owner := scheduleDatabase(t)
	source := scheduledSource(t)
	start := time.Now().UTC()
	if _, err := owner.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, scheduled, err := owner.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || scheduled == nil {
		t.Fatalf("initial scheduled work: %+v %v", scheduled, err)
	}
	run, err := owner.ClaimNext(ctx)
	if err != nil || run == nil {
		t.Fatalf("claim: %+v %v", run, err)
	}
	committed, err := owner.SavePage(ctx, *run, model.Page{Body: []byte("original page"), Next: json.RawMessage(`{"page":2}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.RequestPause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	// Lose the worker's session before FinishRun: startup and tick recovery
	// must both keep the durable manual hold rather than resume this schedule.
	owner.Close()
	restarted, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if err := restarted.RecoverInterrupted(ctx, map[string]bool{source.ID: true}); err != nil {
		t.Fatal(err)
	}
	held, err := restarted.GetRun(ctx, run.ID)
	if err != nil || held.Status != model.StatusPaused || !held.PauseRequested || held.Pages != committed.Pages || !bytes.Equal(held.Cursor, committed.Cursor) {
		t.Fatalf("recovery lost hold/checkpoint: %+v %v", held, err)
	}
	for _, due := range []time.Time{start.Add(2 * time.Minute), start.Add(3 * time.Minute)} {
		if _, next, err := restarted.AttemptSchedule(ctx, source, due, nil); err != nil || next != nil {
			t.Fatalf("scheduler bypassed manual hold: %+v %v", next, err)
		}
	}
	if runs, err := restarted.ListRuns(ctx, model.ListOptions{ProviderID: source.ID}); err != nil || runs.Total != 1 {
		t.Fatalf("held schedule created another traversal: %+v %v", runs, err)
	}
	if _, err := restarted.RequestCancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, next, err := restarted.AttemptSchedule(ctx, source, start.Add(4*time.Minute), nil); err != nil || next == nil || next.ID == run.ID {
		t.Fatalf("explicit cancellation did not release future schedules: %+v %v", next, err)
	}
}

func TestOlderManualHoldBlocksLaterScheduledContinuationAndClaim(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := scheduledSource(t)
	older := mirrorClaim(t, ctx, db, source.Provider, model.ModeFull)
	if _, err := db.FinishRun(ctx, older.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC()
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, newer, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || newer == nil {
		t.Fatalf("schedule did not create newer work: %+v %v", newer, err)
	}
	if _, err := db.RequestPause(ctx, older.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed != nil {
		t.Fatalf("newer scheduled queue bypassed older hold: %+v %v", claimed, err)
	}
	runs, err := db.ScheduleRuns(ctx, []string{source.ID})
	if err != nil {
		t.Fatal(err)
	}
	if state, next, _ := source.RunState(runs[source.ID]); state != "blocked" || next != nil {
		t.Fatalf("older hold hidden by newer scheduled work: %s %+v", state, next)
	}
	if _, next, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil); err != nil || next != nil {
		t.Fatalf("tick bypassed older manual hold: %+v %v", next, err)
	}
	if _, err := db.RequestCancel(ctx, older.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != newer.ID {
		t.Fatalf("cancellation did not release later scheduled queue: %+v %v", claimed, err)
	}
	if _, err := db.FinishRun(ctx, newer.ID, model.StatusPaused, "budget", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	if _, next, err := db.AttemptSchedule(ctx, source, start.Add(3*time.Minute), nil); err != nil || next == nil || next.ID != newer.ID || next.PauseRequested {
		t.Fatalf("automatic budget continuation changed: %+v %v", next, err)
	}
}

func TestCoverageAttemptChangesOnlyAfterExplicitFailedRetry(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	source := scheduledSource(t)
	start := time.Now().UTC()
	if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
		t.Fatal(err)
	}
	_, run, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
	if err != nil || run == nil {
		t.Fatalf("initial schedule: %+v %v", run, err)
	}
	if attempt, err := db.CoverageAttempt(ctx, run.ID); err != nil || attempt != "" {
		t.Fatalf("initial traversal acquired a retry budget: %q %v", attempt, err)
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("initial claim: %+v %v", claimed, err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusFailed, "incomplete", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := db.CoverageAttempt(ctx, run.ID)
	if err != nil || retry == "" {
		t.Fatalf("manual failed retry has no durable generation: %q %v", retry, err)
	}
	for _, manual := range []bool{false, true} {
		if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != run.ID {
			t.Fatalf("continuation claim: %+v %v", claimed, err)
		}
		if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "budget or shutdown", model.PauseBudget); err != nil {
			t.Fatal(err)
		}
		if manual {
			if _, err := db.ResumeRun(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
		} else if _, queued, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil); err != nil || queued == nil || queued.ID != run.ID {
			t.Fatalf("automatic continuation: %+v %v", queued, err)
		}
		if continued, err := db.CoverageAttempt(ctx, run.ID); err != nil || continued != retry {
			t.Fatalf("budget/shutdown continuation reset retry budget: %q != %q (%v)", continued, retry, err)
		}
	}
	if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("second failure claim: %+v %v", claimed, err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusFailed, "still incomplete", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := db.CoverageAttempt(ctx, run.ID); err != nil || next == "" || next == retry {
		t.Fatalf("second explicit retry reused exhausted budget: %q %v", next, err)
	}
}

func TestManualPauseFailureBlocksScheduleUntilExplicitRelease(t *testing.T) {
	for _, action := range []string{"resume", "cancel"} {
		t.Run(action, func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			source := scheduledSource(t)
			start := time.Now().UTC()
			if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
				t.Fatal(err)
			}
			_, run, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil)
			if err != nil || run == nil {
				t.Fatalf("initial schedule: %+v %v", run, err)
			}
			if claimed, err := db.ClaimNext(ctx); err != nil || claimed == nil || claimed.ID != run.ID {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			if _, err := db.RequestPause(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			evidence, err := db.AddEvent(ctx, run.ID, "source_error", "Source coverage differs", map[string]any{"failure_code": "stalled"})
			if err != nil {
				t.Fatal(err)
			}
			failed, err := db.FinishRun(ctx, run.ID, model.StatusFailed, "source coverage differs", "")
			if err != nil || failed.Status != model.StatusFailed || !failed.PauseRequested || failed.Error == "" {
				t.Fatalf("failure hid the error or discarded accepted hold: %+v %v", failed, err)
			}
			if repeated, err := db.RequestPause(ctx, run.ID); err != nil || repeated.Status != model.StatusFailed || !repeated.PauseRequested || repeated.Error != failed.Error {
				t.Fatalf("repeated pause replaced held failure evidence: %+v %v", repeated, err)
			}
			runs, err := db.ScheduleRuns(ctx, []string{source.ID})
			if err != nil {
				t.Fatal(err)
			}
			if state, resume, _ := source.RunState(runs[source.ID]); state != "blocked" || resume != nil {
				t.Fatalf("failed manual hold was omitted from scheduling: %s %+v", state, resume)
			}
			if _, next, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil); err != nil || next != nil {
				t.Fatalf("failed held run was replaced automatically: %+v %v", next, err)
			}
			if action == "resume" {
				resumed, err := db.ResumeRun(ctx, run.ID)
				if err != nil || resumed.Status != model.StatusQueued || resumed.PauseRequested {
					t.Fatalf("explicit failed resume did not clear hold: %+v %v", resumed, err)
				}
				if attempt, err := db.CoverageAttempt(ctx, run.ID); err != nil || attempt == "" {
					t.Fatalf("explicit held failure retry did not reset coverage attempt: %q %v", attempt, err)
				}
			} else {
				cancelled, err := db.RequestCancel(ctx, run.ID)
				if err != nil || cancelled.Status != model.StatusCancelled || cancelled.PauseRequested {
					t.Fatalf("explicit failed cancellation did not clear hold: %+v %v", cancelled, err)
				}
				if _, next, err := db.AttemptSchedule(ctx, source, start.Add(3*time.Minute), nil); err != nil || next == nil || next.ID == run.ID {
					t.Fatalf("released failure still suppressed later schedules: %+v %v", next, err)
				}
			}
			events, err := db.Events(ctx, run.ID, model.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events.Items {
				if event.ID == evidence.ID {
					found = event.Kind == evidence.Kind && event.Message == evidence.Message && event.Data["failure_code"] == "stalled"
				}
			}
			if !found {
				t.Fatal("explicit release discarded the original failure event")
			}
		})
	}
}
