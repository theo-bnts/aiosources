package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMatchingClockIsDurableAndInterruptedWorkDoesNotPostponeIt(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, status := range []string{"running", "interrupted", "succeeded", "cancelled", "failed"} {
		if _, err := db.pool.Exec(ctx, `UPDATE aiosources.aio_state SET last_match=NULL`); err != nil {
			t.Fatal(err)
		}
		run, err := db.BeginOperation(ctx, model.OperationRun{Kind: "matching", Service: "local", Trigger: "manual"}, "matcher")
		if err != nil {
			t.Fatal(err)
		}
		run.Status, run.Phase, run.Results = status, "identification", map[string]int64{}
		if err = db.SaveOperation(ctx, run); err != nil {
			t.Fatal(err)
		}
		var clock *time.Time
		if err = db.pool.QueryRow(ctx, `SELECT last_match FROM aiosources.aio_state`).Scan(&clock); err != nil {
			t.Fatal(err)
		}
		if (clock != nil) != (status != "running" && status != "interrupted") {
			t.Fatalf("wrong clock for %s: %v", status, clock)
		}
		if clock != nil {
			// History pruning is independent of the saved automatic schedule.
			if _, err = db.pool.Exec(ctx, `DELETE FROM aiosources.operation_events WHERE run_id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.pool.Exec(ctx, `DELETE FROM aiosources.operation_runs WHERE id=$1`, run.ID); err != nil {
				t.Fatal(err)
			}
			current, err := db.AIOAutomationStatus(ctx)
			if err != nil || current.LastMatch == nil || !current.LastMatch.Equal(*clock) {
				t.Fatal("history pruning changed cadence", err)
			}
		}
	}
}

func TestIdentificationIntervalDefaultsForExistingSavedConfiguration(t *testing.T) {
	ctx, db := sharingDatabase(t)
	c := DefaultAIOConfig()
	if err := db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.settings SET value=(value::jsonb-'match_seconds')::text WHERE key='aiosource.config.v2'`); err != nil {
		t.Fatal(err)
	}
	c, err := db.AIOConfig(ctx)
	if err != nil || c.MatchSeconds != 30 {
		t.Fatal("old configuration lost interval default", c.MatchSeconds, err)
	}
}
