package store

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func TestFailedScheduledImportRetriesItsImmutableCheckpointOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "retry"}[enabled], func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			source := scheduledSource(t)
			source.Provider.Schedule.RetryFailed = enabled
			start := time.Now().UTC()
			if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
				t.Fatal(err)
			}
			if _, run, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil); err != nil || run == nil {
				t.Fatal("initial scheduled run", err)
			}
			claimed, err := db.ClaimNext(ctx)
			if err != nil || claimed == nil {
				t.Fatal(err)
			}
			checkpoint := json.RawMessage(`{"page":41}`)
			saved, err := db.SavePage(ctx, *claimed, model.Page{Body: []byte("synthetic listing"), Next: checkpoint}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.FinishRun(ctx, saved.ID, model.StatusFailed, "Temporary upstream failure", ""); err != nil {
				t.Fatal(err)
			}
			listed, err := db.ScheduleRuns(ctx, []string{source.ID})
			if err != nil || (len(listed[source.ID]) != 0) != enabled {
				t.Fatal("failed retry eligibility is inconsistent", err)
			}
			var queued atomic.Int32
			var wait sync.WaitGroup
			for range 8 {
				wait.Go(func() {
					_, run, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil)
					if err != nil {
						t.Error(err)
						return
					}
					if run == nil {
						return
					}
					queued.Add(1)
					if enabled && (run.ID != saved.ID || !bytes.Equal(run.Cursor, saved.Cursor) || run.Pages != saved.Pages || run.EffectivePolicy() != saved.EffectivePolicy() || run.Revision != saved.Revision) {
						t.Error("retry changed the run snapshot, policy or committed progress")
					}
					if !enabled && run.ID == saved.ID {
						t.Error("default policy resumed a failed run")
					}
				})
			}
			wait.Wait()
			if queued.Load() != 1 {
				t.Fatal("simultaneous scheduled ticks did not queue exactly once", queued.Load())
			}
			if enabled {
				attempt, err := db.CoverageAttempt(ctx, saved.ID)
				if err != nil || attempt == "" {
					t.Fatal("failed retry did not renew bounded reconciliation", err)
				}
			}
		})
	}
}

func TestFailedScheduledRetryKeepsRecoveryEvidenceAndHonorsHoldAndCancellation(t *testing.T) {
	for _, hold := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "hold"}[hold], func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			source := scheduledSource(t)
			source.Provider.Schedule.RetryFailed = true
			start := time.Now().UTC()
			if _, err := db.ReconcileSchedules(ctx, []scheduling.Source{source}, start); err != nil {
				t.Fatal(err)
			}
			if _, run, err := db.AttemptSchedule(ctx, source, start.Add(time.Minute), nil); err != nil || run == nil {
				t.Fatal(err)
			}
			run, err := db.ClaimNext(ctx)
			if err != nil || run == nil {
				t.Fatal(err)
			}
			if _, err = db.SavePage(ctx, *run, model.Page{Next: json.RawMessage(`{"page":41}`)}, ""); err != nil {
				t.Fatal(err)
			}
			failed, err := db.FinishRun(ctx, run.ID, model.StatusFailed, "Temporary upstream failure", "")
			if err != nil || !failed.RetryFailed {
				t.Fatal("scheduled retry permission unavailable", err)
			}
			if _, err = db.pool.Exec(ctx, `INSERT INTO aiosources.run_record_identities(run_id,source_id) VALUES($1,'synthetic-record');`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `UPDATE aiosources.runs SET finished_at=now()-interval '30 days' WHERE id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.PruneHistory(ctx); err != nil {
				t.Fatal(err)
			}
			var kept bool
			if err = db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.run_record_identities WHERE run_id=$1)`, run.ID).Scan(&kept); err != nil || !kept {
				t.Fatal("cleanup discarded retry evidence", err)
			}
			if hold {
				paused, err := db.RequestPause(ctx, run.ID)
				if err != nil || paused.Status != model.StatusPaused || !paused.PauseRequested {
					t.Fatal("failed retry could not be held", err)
				}
				if _, queued, err := db.AttemptSchedule(ctx, source, start.Add(2*time.Minute), nil); err != nil || queued != nil {
					t.Fatal("schedule bypassed the manual hold", err)
				}
			}
			cancelled, err := db.RequestCancel(ctx, run.ID)
			if err != nil || cancelled.Status != model.StatusCancelled {
				t.Fatal("failed retry could not be cancelled", err)
			}
			if _, queued, err := db.AttemptSchedule(ctx, source, start.Add(3*time.Minute), nil); err != nil || queued == nil || queued.ID == run.ID {
				t.Fatal("cancellation did not release the old scheduled retry", err)
			}
		})
	}
}
