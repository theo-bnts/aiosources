package scheduling

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCronCalendarAndTimezoneBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		settings model.Schedule
		after    string
		want     string
	}{
		{"UTC default skips exact instant", model.Schedule{Cron: "0 12 * * *"}, "2026-09-22T12:00:00Z", "2026-09-23T12:00:00Z"},
		{"fractional UTC offset", model.Schedule{Cron: "0 9 * * *", Timezone: "Asia/Kathmandu"}, "2026-09-22T03:14:59Z", "2026-09-22T03:15:00Z"},
		{"spring gap is skipped", model.Schedule{Cron: "30 2 * * *", Timezone: "America/New_York"}, "2026-03-07T08:00:00Z", "2026-03-09T06:30:00Z"},
		{"fall repetition is a second instant", model.Schedule{Cron: "30 1 * * *", Timezone: "America/New_York"}, "2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"},
		{"leap day remains reachable", model.Schedule{Cron: "0 0 29 2 *"}, "2025-03-01T00:00:00Z", "2028-02-29T00:00:00Z"},
		{"interval waits from current time", model.Schedule{Every: "1h"}, "2026-09-22T12:23:45Z", "2026-09-22T13:23:45Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Parse(tc.settings)
			if err != nil {
				t.Fatal(err)
			}
			after, err := time.Parse(time.RFC3339, tc.after)
			if err != nil {
				t.Fatal(err)
			}
			if got := plan.Next(after).Format(time.RFC3339); got != tc.want {
				t.Fatalf("next instant = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestScheduleRejectsAmbiguousOrUnsafeTiming(t *testing.T) {
	const secret = "pasted-secret-value"
	cases := []model.Schedule{
		{Every: "1h", Cron: "* * * * *"},
		{Every: "59s"},
		{Every: "1m", FullEvery: "59s"},
		{Every: "1m", FullEvery: "0"},
		{Every: "1m", FullEvery: secret},
		{FullEvery: "24h"},
		{Every: "1m", FullEvery: "24h", Mode: model.ModeFull},
		{Cron: "* * * * *", FullEvery: "24h", Mode: model.ModeMetadata},
		{Cron: "0 * * * * *"},
		{Cron: "@daily"},
		{Cron: "CRON_TZ=UTC 0 0 * * *"},
		{Cron: "0 0 31 2 *"},
		{Cron: secret + " * * * *"},
		{Every: "1h", Timezone: secret},
		{Every: "1h", Timezone: "Local"},
		{Mode: model.ModePreview},
		{MaxPages: -1},
		{KnownPages: 10001},
	}
	for index, settings := range cases {
		if _, err := Parse(settings); err == nil {
			t.Errorf("invalid schedule %d accepted", index)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("schedule error disclosed input: %s", err)
		}
	}
}

func TestScheduleResolutionAndDisabledRetention(t *testing.T) {
	manual, err := Parse(model.Schedule{})
	if err != nil || manual.Enabled || manual.Configured || !manual.Next(time.Now()).IsZero() {
		t.Fatalf("manual schedule acquired a clock: %+v %v", manual, err)
	}
	disabled := false
	settings := model.Schedule{Enabled: &disabled, Every: "1h", Mode: model.ModeFull, MaxPages: 2}
	plan, err := Parse(settings)
	if err != nil || plan.Enabled || !plan.Configured || plan.Mode != model.ModeFull {
		t.Fatalf("disabled timing was not retained: %+v %v", plan, err)
	}
	source := Source{Valid: true, Provider: model.Provider{Enabled: true}, Plan: plan}
	state, _, _ := source.RunState(nil)
	if state != "disabled" {
		t.Fatalf("disabled schedule is %s", state)
	}
	settings.Enabled = nil
	plan, err = Parse(settings)
	if err != nil || !plan.Enabled {
		t.Fatalf("configured schedule did not default to enabled: %+v %v", plan, err)
	}
}

func TestContinuationRequiresPausedScheduledMatchingSnapshot(t *testing.T) {
	plan, err := Parse(model.Schedule{Every: "1m"})
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "source", Revision: "current", Provider: model.Provider{Enabled: true}, Valid: true, Plan: plan}
	base := model.Run{ID: "saved", Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget, Revision: "current", Mode: model.ModeIncremental}
	for _, status := range []model.RunStatus{model.StatusFailed, model.StatusCancelled, model.StatusSucceeded} {
		run := base
		run.Status = status
		state, resume, _ := source.RunState([]model.Run{run})
		if state != "waiting" || resume != nil {
			t.Fatalf("terminal %s run became an automatic continuation: %s %+v", status, state, resume)
		}
	}
	manual := base
	manual.Trigger = model.TriggerManual
	if state, resume, _ := source.RunState([]model.Run{manual}); state != "waiting" || resume != nil {
		t.Fatalf("manual paused run became scheduled work: %s %+v", state, resume)
	}
	if state, resume, _ := source.RunState([]model.Run{base}); state != "paused" || resume == nil || resume.ID != base.ID {
		t.Fatalf("eligible continuation was not selected: %s %+v", state, resume)
	}
	for _, reason := range []model.PauseReason{"", model.PauseManual, model.PauseQuota, model.PauseNoProgress, model.PauseInterrupted} {
		held := base
		held.PauseReason = reason
		if state, resume, _ := source.RunState([]model.Run{held}); state != "blocked" || resume != nil {
			t.Fatalf("non-budget pause %q automatically continued: %s %+v", reason, state, resume)
		}
	}
	for _, incompatible := range []model.Run{
		{ID: "older", Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget, Revision: "older", Mode: model.ModeIncremental},
		{ID: "full", Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget, Revision: "current", Mode: model.ModeFull},
	} {
		state, resume, message := source.RunState([]model.Run{base, incompatible})
		if state != "blocked" || resume != nil || message == "" {
			t.Fatalf("incompatible paused work did not block automatic continuation: %s %+v", state, resume)
		}
	}
	active := base
	active.Status = model.StatusRunning
	if state, resume, _ := source.RunState([]model.Run{base, active}); state != "running" || resume != nil {
		t.Fatalf("active run allowed an overlapping continuation: %s %+v", state, resume)
	}
}

func TestFullRoutineUsesBaseTicksAndRespectsContinuationHolds(t *testing.T) {
	plan, err := Parse(model.Schedule{Every: "10m", FullEvery: "1m"})
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err != nil || plan.Mode != model.ModeIncremental || plan.FullEvery != time.Minute || !plan.Next(start).Equal(start.Add(10*time.Minute)) {
		t.Fatalf("Full routine acquired an independent base tick: %+v %v", plan, err)
	}
	source := Source{ID: "source", Revision: "current", Provider: model.Provider{Enabled: true}, Valid: true, Plan: plan}
	full := model.Run{ID: "full", Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget, Revision: "current", Mode: model.ModeFull}
	if state, resumed, _ := source.RunState([]model.Run{full}); state != "paused" || resumed == nil || resumed.ID != full.ID || resumed.Mode != model.ModeFull {
		t.Fatalf("routine did not retain the Full continuation: %s %+v", state, resumed)
	}
	for _, held := range []model.Run{
		{Status: model.StatusPaused, PauseReason: model.PauseManual},
		{Status: model.StatusFailed, PauseReason: model.PauseQuota},
		{Status: model.StatusFailed, PauseReason: model.PauseNoProgress},
		{Status: model.StatusFailed, PauseRequested: true},
		{Status: model.StatusPaused, PauseReason: model.PauseBudget, Trigger: model.TriggerScheduled, Revision: "older", Mode: model.ModeFull},
	} {
		if state, resumed, _ := source.RunState([]model.Run{full, held}); state != "blocked" || resumed != nil {
			t.Fatalf("Full eligibility bypassed an existing hold: %s %+v", state, resumed)
		}
	}
}

func TestMetadataFollowupContinuationRequiresMatchingIncrementalSchedule(t *testing.T) {
	plan, err := Parse(model.Schedule{Every: "1m", FullEvery: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "source", Revision: "current", Provider: model.Provider{Enabled: true}, Valid: true, Plan: plan}
	base := model.Run{
		ID: "metadata", Mode: model.ModeMetadata, MetadataParentRunID: "incremental",
		Trigger: model.TriggerScheduled, Status: model.StatusPaused, PauseReason: model.PauseBudget, Revision: "current",
	}
	cases := []struct {
		name   string
		change func(*Source, *model.Run)
		state  string
	}{
		{name: "matching follow-up", state: "paused"},
		{name: "changed revision", state: "blocked", change: func(s *Source, _ *model.Run) { s.Revision = "changed" }},
		{name: "standalone Metadata", state: "blocked", change: func(_ *Source, r *model.Run) { r.MetadataParentRunID = "" }},
		{name: "Full schedule", state: "blocked", change: func(s *Source, _ *model.Run) { s.Plan.Mode, s.Plan.FullEvery = model.ModeFull, 0 }},
		{name: "manual budget", state: "waiting", change: func(_ *Source, r *model.Run) { r.Trigger = model.TriggerManual }},
		{name: "manual hold", state: "blocked", change: func(_ *Source, r *model.Run) { r.PauseReason = model.PauseManual }},
		{name: "requested pause", state: "blocked", change: func(_ *Source, r *model.Run) { r.PauseRequested = true }},
		{name: "quota hold", state: "blocked", change: func(_ *Source, r *model.Run) { r.PauseReason = model.PauseQuota }},
		{name: "no progress", state: "blocked", change: func(_ *Source, r *model.Run) { r.PauseReason = model.PauseNoProgress }},
		{name: "interrupted", state: "blocked", change: func(_ *Source, r *model.Run) { r.PauseReason = model.PauseInterrupted }},
		{name: "cancelled", state: "waiting", change: func(_ *Source, r *model.Run) { r.Status, r.PauseReason = model.StatusCancelled, "" }},
		{name: "failed", state: "waiting", change: func(_ *Source, r *model.Run) { r.Status, r.PauseReason = model.StatusFailed, "" }},
		{name: "completed", state: "waiting", change: func(_ *Source, r *model.Run) { r.Status, r.PauseReason = model.StatusSucceeded, "" }},
		{name: "disabled", state: "disabled", change: func(s *Source, _ *model.Run) { s.Provider.Enabled = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current, run := source, base
			if tc.change != nil {
				tc.change(&current, &run)
			}
			state, resume, _ := current.RunState([]model.Run{run})
			if state != tc.state || (resume != nil) != (tc.state == "paused") {
				t.Fatalf("follow-up continuation bypassed eligibility: state=%s resume=%+v", state, resume)
			}
			if resume != nil && (resume.ID != base.ID || resume.MetadataParentRunID != base.MetadataParentRunID) {
				t.Fatalf("continuation replaced the automatic traversal: %+v", resume)
			}
		})
	}
	for _, other := range []model.Run{
		{ID: "older-hold", PauseReason: model.PauseManual, Status: model.StatusPaused},
		{ID: "active", Status: model.StatusRunning},
	} {
		state, resume, _ := source.RunState([]model.Run{base, other})
		if resume != nil || (state != "blocked" && state != "running") {
			t.Fatalf("Metadata follow-up bypassed another source hold or active lease: %s %+v", state, resume)
		}
	}
}
