package jobs

import (
	"context"
	"errors"
	"time"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (m *Manager) scheduler() {
	defer m.workers.Done()
	for {
		if m.ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		connected := m.connected
		m.mu.Unlock()
		var next time.Time
		if connected {
			var err error
			next, err = m.scheduleDue(m.ctx, time.Now())
			if err != nil {
				if m.ctx.Err() == nil {
					m.fail(err)
				}
				return
			}
		}
		var timer *time.Timer
		var due <-chan time.Time
		if !next.IsZero() {
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			timer = time.NewTimer(delay)
			due = timer.C
		}
		select {
		case <-m.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-m.schedule:
			if timer != nil {
				timer.Stop()
			}
		case <-due:
		}
	}
}

// A single registry snapshot stays locked until reconciliation and due attempts
// commit. Both the API and dispatcher read exactly these persistent clocks.
func (m *Manager) withSchedules(ctx context.Context, now time.Time, fn func([]scheduling.Source, map[string]store.ScheduleClock) error) error {
	return m.registry.WithDefinitions(func(documents map[string]model.ProviderDocument) error {
		sources := scheduling.Sources(documents)
		clocks, err := m.db.ReconcileSchedules(ctx, sources, now)
		if err != nil {
			return errPersistence
		}
		return fn(sources, clocks)
	})
}

// Every wake comes from a persisted change, an editable-profile notice, or the
// next actual deadline. No queue polling or process-local retry state is needed.
func (m *Manager) scheduleDue(ctx context.Context, now time.Time) (time.Time, error) {
	var next time.Time
	err := m.withSchedules(ctx, now, func(sources []scheduling.Source, clocks map[string]store.ScheduleClock) error {
		for _, source := range sources {
			clock := clocks[source.ID]
			if !clock.Active || clock.NextRunAt == nil {
				continue
			}
			if !clock.NextRunAt.After(now) {
				preparationErr := connectors.Validate(source.Provider)
				if preparationErr == nil {
					_, preparationErr = m.secrets(ctx, source.Provider)
				}
				updated, run, err := m.db.AttemptSchedule(ctx, source, now, preparationErr)
				if err != nil {
					return errPersistence
				}
				clock = updated
				if run != nil {
					m.signalWork()
					m.Notify("run", run.ID)
				}
			}
			if clock.NextRunAt != nil && (next.IsZero() || clock.NextRunAt.Before(next)) {
				next = *clock.NextRunAt
			}
		}
		return nil
	})
	if err != nil {
		return time.Time{}, errors.New("scheduled collection state is unavailable")
	}
	return next, nil
}

// Schedules includes invalid and manual sources and uses batched run queries.
// Reading does not dispatch a job; newly configured clocks wait their first
// interval or the next cron instant, just as they do at manager startup.
func (m *Manager) Schedules(ctx context.Context) ([]model.ScheduleSummary, error) {
	return m.schedules(ctx, false)
}

func (m *Manager) CatalogSynchronizations(ctx context.Context) ([]model.ScheduleSummary, error) {
	return m.schedules(ctx, true)
}

func (m *Manager) schedules(ctx context.Context, catalogs bool) ([]model.ScheduleSummary, error) {
	items := []model.ScheduleSummary{}
	err := m.withSchedules(ctx, time.Now(), func(sources []scheduling.Source, clocks map[string]store.ScheduleClock) error {
		ids := make([]string, len(sources))
		for index, source := range sources {
			ids[index] = source.ID
		}
		latest, err := m.db.LatestRuns(ctx, ids)
		if err != nil {
			return errPersistence
		}
		runs, err := m.db.ScheduleRuns(ctx, ids)
		if err != nil {
			return errPersistence
		}
		for _, source := range sources {
			if source.Provider.IsRemoteCatalog() != catalogs {
				continue
			}
			clock := clocks[source.ID]
			state, _, message := source.RunState(runs[source.ID])
			if message == "" {
				message = clock.LastError
			}
			name := source.Provider.Name
			if name == "" {
				name = source.ID
			}
			settings, _ := scheduling.Settings(source.Provider)
			item := model.ScheduleSummary{ProviderID: source.ID, ProviderName: name,
				ProviderEnabled: source.Provider.Enabled, Valid: source.Valid, Revision: source.Revision,
				Schedule: settings, Enabled: source.Plan.Enabled, State: state,
				NextRunAt: clock.NextRunAt, NextFullAt: clock.FullDueAt, LastError: message}
			if last, ok := latest[source.ID]; ok {
				item.LastRun = &last
			}
			items = append(items, item)
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("scheduled collection state is unavailable")
	}
	return items, nil
}

func (m *Manager) observeDefinitions(changes <-chan struct{}, unsubscribe func()) {
	defer m.workers.Done()
	defer stopSubscription(changes, unsubscribe)
	for {
		select {
		case <-m.ctx.Done():
			return
		case _, ok := <-changes:
			if !ok {
				if m.ctx.Err() == nil {
					m.fail(errors.New("provider definition change subscription was interrupted"))
				}
				return
			}
			m.Notify("providers", "")
		}
	}
}
