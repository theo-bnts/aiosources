package scheduling

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestFailedRetryRespectsManualHoldsCancellationAndSnapshotChanges(t *testing.T) {
	p := model.Provider{ID: "example", Enabled: true, Schedule: model.Schedule{Every: "1m", Mode: model.ModeIncremental, RetryFailed: true}}
	plan, err := Parse(p.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: p.ID, Revision: "same", Provider: p, Valid: true, Plan: plan}
	base := model.Run{ID: "failed", Revision: "same", Config: p, Status: model.StatusFailed, Mode: model.ModeFull, InitialFull: true, Trigger: model.TriggerScheduled}
	for _, test := range []struct {
		name   string
		change func(*model.Run)
		state  string
		resume bool
	}{
		{"eligible", func(*model.Run) {}, "waiting", true},
		{"manual hold", func(r *model.Run) { r.PauseRequested = true }, "blocked", false},
		{"manual quota", func(r *model.Run) { r.PauseReason = model.PauseQuota }, "blocked", false},
		{"manual no progress", func(r *model.Run) { r.PauseReason = model.PauseNoProgress }, "blocked", false},
		{"cancelled", func(r *model.Run) { r.Status = model.StatusCancelled }, "waiting", false},
		{"cancellation requested", func(r *model.Run) { r.CancelRequested = true }, "waiting", false},
		{"manual run", func(r *model.Run) { r.Trigger = model.TriggerManual }, "waiting", false},
		{"old revision", func(r *model.Run) { r.Revision = "old" }, "blocked", false},
		{"different mode", func(r *model.Run) { r.InitialFull = false }, "blocked", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := base
			test.change(&run)
			state, resume, _ := source.RunState([]model.Run{run})
			if state != test.state || (resume != nil) != test.resume {
				t.Fatal("unexpected retry decision", state, resume != nil)
			}
		})
	}
	source.Provider.Schedule.RetryFailed = false
	if _, resume, _ := source.RunState([]model.Run{base}); resume != nil {
		t.Fatal("turning off retries did not disable continuation")
	}
}
