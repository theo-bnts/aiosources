// Package scheduling defines the shared JSON schedule and run eligibility rules.
package scheduling

import (
	"errors"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // Named zones must also work in distroless containers.

	"github.com/robfig/cron/v3"

	"github.com/theo-bnts/aiosources/internal/model"
)

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type Plan struct {
	Configured bool
	Enabled    bool
	Mode       model.RunMode
	FullEvery  time.Duration
	every      time.Duration
	cron       cron.Schedule
}

// Parse never includes configuration values in errors: malformed input can
// contain credentials pasted into the wrong field.
func Parse(settings model.Schedule) (Plan, error) {
	plan := Plan{Configured: settings.Every != "" || settings.Cron != "", Mode: settings.Mode}
	plan.Enabled = plan.Configured && (settings.Enabled == nil || *settings.Enabled)
	if plan.Mode == "" {
		plan.Mode = model.ModeIncremental
	}
	if plan.Mode != model.ModeIncremental && plan.Mode != model.ModeFull && plan.Mode != model.ModeMetadata {
		return plan, errors.New("schedule.mode must be incremental, full or metadata")
	}
	if settings.MaxPages < 0 || settings.MaxPages > 10000 {
		return plan, errors.New("schedule.max_pages must be between 0 and 10000")
	}
	if settings.KnownPages < 0 || settings.KnownPages > 10000 {
		return plan, errors.New("schedule.known_pages must be between 0 and 10000")
	}
	if settings.Every != "" && settings.Cron != "" {
		return plan, errors.New("schedule.every and schedule.cron are mutually exclusive")
	}
	zone := settings.Timezone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "Local" {
		return plan, errors.New("schedule.timezone must be an IANA time zone")
	}
	if settings.Every != "" {
		plan.every, err = time.ParseDuration(settings.Every)
		if err != nil || plan.every < time.Minute {
			return plan, errors.New("schedule.every must be a Go duration of at least 1m")
		}
	}
	if settings.FullEvery != "" {
		if !plan.Configured || plan.Mode != model.ModeIncremental {
			return plan, errors.New("schedule.full_every requires a configured incremental schedule")
		}
		plan.FullEvery, err = time.ParseDuration(settings.FullEvery)
		if err != nil || plan.FullEvery < time.Minute {
			return plan, errors.New("schedule.full_every must be a Go duration of at least 1m")
		}
	}
	if settings.Cron != "" {
		if len(strings.Fields(settings.Cron)) != 5 {
			return plan, errors.New("schedule.cron must be a standard five-field expression")
		}
		parsed, err := parser.Parse(settings.Cron)
		if err != nil {
			return plan, errors.New("schedule.cron must be a valid five-field expression")
		}
		spec := parsed.(*cron.SpecSchedule)
		spec.Location = location
		plan.cron = spec
		if plan.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
			return plan, errors.New("schedule.cron has no matching calendar time")
		}
	}
	return plan, nil
}

// Next is strictly after the supplied instant. Advancing from the current time
// rather than the old due slot prevents catch-up bursts after downtime.
func (p Plan) Next(after time.Time) time.Time {
	if p.cron != nil {
		return p.cron.Next(after).UTC()
	}
	if p.every > 0 {
		return after.Add(p.every).UTC()
	}
	return time.Time{}
}

type Source struct {
	ID       string
	Revision string
	Provider model.Provider
	Valid    bool
	Plan     Plan
}

func (s Source) Active() bool { return s.Valid && s.Provider.Enabled && s.Plan.Enabled }

func (s Source) Profile() string {
	if s.Provider.IsRemoteCatalog() {
		return "catalog"
	}
	return "source"
}

// RunState is used both by the scheduler transaction and the read API. Manual
// holds and paused scheduled runs participate regardless of their creation order.
func (s Source) RunState(runs []model.Run) (state string, resume *model.Run, message string) {
	if !s.Valid {
		return "invalid", nil, "Repair the source configuration before scheduling collections"
	}
	if !s.Plan.Configured {
		return "manual", nil, ""
	}
	if !s.Provider.Enabled || !s.Plan.Enabled {
		return "disabled", nil, ""
	}
	for _, run := range runs {
		if run.RequiresManualResume() {
			return "blocked", nil, "A collection requires explicit resume or cancellation in run history before scheduled work can continue"
		}
	}
	for _, run := range runs {
		if run.Status == model.StatusQueued || run.Status == model.StatusRunning {
			return "running", nil, ""
		}
	}
	for _, run := range runs {
		if run.Status != model.StatusPaused || run.Trigger != model.TriggerScheduled {
			continue
		}
		if run.PauseReason != model.PauseBudget {
			return "blocked", nil, "A paused scheduled collection requires explicit resume; only budget pauses continue on a scheduled tick"
		}
		metadataFollowup := run.Mode == model.ModeMetadata && run.MetadataParentRunID != "" && s.Plan.Mode == model.ModeIncremental
		initialFull := run.Mode == model.ModeFull && run.InitialFull && s.Plan.Mode == model.ModeIncremental
		modeMatches := run.Mode == s.Plan.Mode || (s.Plan.FullEvery > 0 && run.Mode == model.ModeFull) || initialFull || metadataFollowup ||
			(s.Provider.IsRemoteCatalog() && run.Mode == model.ModeFull)
		if !s.matchesSnapshot(run) || !modeMatches {
			return "blocked", nil, "A paused scheduled collection uses an older configuration or different mode; resume or cancel it manually in run history"
		}
		if resume == nil {
			copy := run
			resume = &copy
		}
	}
	if resume != nil {
		return "paused", resume, ""
	}
	return "waiting", nil, ""
}

func (s Source) matchesSnapshot(run model.Run) bool {
	if run.Revision == s.Revision {
		return true
	}
	if s.Provider.Adapter != "json" || run.Config.Adapter != "http_json" {
		return false
	}
	// An automatic adapter rename changes file revisions, but must not strand
	// a budgeted traversal. All functional settings still have to match; the
	// original snapshot, revision and checkpoint continue to own that run.
	normalize := func(p model.Provider) (model.Provider, error) {
		p.Adapter = "json"
		var err error
		p.Schedule, err = Settings(p)
		if p.RequestTimeout == "" {
			p.RequestTimeout = model.DefaultRequestTimeout
		}
		return p, err
	}
	current, currentErr := normalize(s.Provider)
	snapshot, snapshotErr := normalize(run.Config)
	return currentErr == nil && snapshotErr == nil && equalSnapshotJSON(current, snapshot)
}

// Sources orders a complete registry snapshot for deterministic locking and
// presentation. Invalid definitions retain their repair IDs.
func Sources(documents map[string]model.ProviderDocument) []Source {
	sources := make([]Source, 0, len(documents))
	for id, document := range documents {
		settings, err := Settings(document.Provider)
		plan, parseErr := Parse(settings)
		sources = append(sources, Source{ID: id, Revision: document.Revision, Provider: document.Provider,
			Valid: len(document.Issues) == 0 && document.Revision != "" && document.Provider.ID == id && err == nil && parseErr == nil, Plan: plan})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	return sources
}
