package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

const scheduleLockNamespace int32 = 1768843110
const queueLockNamespace int32 = 1768843111

// Queue mutations use a short transaction lock, separate from the worker's
// session-long provider lease. Hash collisions only serialize unrelated queues.
func lockProviderQueue(ctx context.Context, tx pgx.Tx, providerID string) error {
	if err := lockSourceData(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", queueLockNamespace, int32(providerLockKey(providerID)))
	return err
}

type ScheduleClock struct {
	ProviderID    string
	Revision      string
	Profile       string
	Active        bool
	NextRunAt     *time.Time
	FullDueAt     *time.Time
	LastAttemptAt *time.Time
	LastError     string
}

const clockColumns = `provider_id,revision,active,next_run_at,full_due_at,last_attempt_at,last_error,profile`

func scanClock(row scanner) (ScheduleClock, error) {
	var clock ScheduleClock
	err := row.Scan(&clock.ProviderID, &clock.Revision, &clock.Active, &clock.NextRunAt, &clock.FullDueAt, &clock.LastAttemptAt, &clock.LastError, &clock.Profile)
	return clock, err
}

// ReconcileSchedules must receive one locked, complete registry snapshot. A
// changed revision starts a fresh clock; unchanged revisions survive restarts.
func (s *Store) ReconcileSchedules(ctx context.Context, sources []scheduling.Source, now time.Time) (map[string]ScheduleClock, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, databaseError("begin schedule reconciliation", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", scheduleLockNamespace, int32(1)); err != nil {
		return nil, databaseError("lock schedule reconciliation", err)
	}
	ids := make([]string, 0, len(sources))
	changed := false
	for _, source := range sources {
		ids = append(ids, source.ID)
		var next, fullDue *time.Time
		if source.Active() {
			value := source.Plan.Next(now)
			if value.IsZero() {
				return nil, model.ErrInvalid
			}
			next = &value
			if source.Plan.FullEvery > 0 {
				deadline := now.Add(source.Plan.FullEvery).UTC()
				fullDue = &deadline
			}
		}
		result, err := tx.Exec(ctx, `INSERT INTO ingest.schedule_clocks(provider_id,revision,active,next_run_at,full_due_at,profile)
VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(provider_id) DO UPDATE
SET revision=EXCLUDED.revision,active=EXCLUDED.active,next_run_at=EXCLUDED.next_run_at,full_due_at=EXCLUDED.full_due_at,profile=EXCLUDED.profile,last_error=''
WHERE schedule_clocks.revision<>EXCLUDED.revision OR schedule_clocks.active<>EXCLUDED.active OR schedule_clocks.profile<>EXCLUDED.profile`, source.ID, source.Revision, source.Active(), next, fullDue, source.Profile())
		if err != nil {
			return nil, databaseError("reconcile schedule clock", err)
		}
		changed = changed || result.RowsAffected() != 0
	}
	result, err := tx.Exec(ctx, "DELETE FROM ingest.schedule_clocks WHERE NOT (provider_id=ANY($1::text[]))", ids)
	if err != nil {
		return nil, databaseError("remove deleted schedule clocks", err)
	}
	changed = changed || result.RowsAffected() != 0
	rows, err := tx.Query(ctx, "SELECT "+clockColumns+" FROM ingest.schedule_clocks")
	if err != nil {
		return nil, databaseError("read reconciled schedule clocks", err)
	}
	clocks := make(map[string]ScheduleClock, len(sources))
	for rows.Next() {
		clock, err := scanClock(rows)
		if err != nil {
			rows.Close()
			return nil, databaseError("read schedule clock", err)
		}
		clocks[clock.ProviderID] = clock
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, databaseError("read schedule clocks", err)
	}
	if changed {
		if err := notify(ctx, tx); err != nil {
			return nil, databaseError("notify schedule reconciliation", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, databaseError("commit schedule reconciliation", err)
	}
	return clocks, nil
}

// ScheduleRuns includes every manual hold, even on an older manually queued
// collection, so a newer scheduled continuation cannot bypass it.
func (s *Store) ScheduleRuns(ctx context.Context, providerIDs []string) (map[string][]model.Run, error) {
	result := make(map[string][]model.Run, len(providerIDs))
	if len(providerIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT "+runColumns+` FROM ingest.runs
WHERE provider_id=ANY($1::text[]) AND (status IN ('queued','running') OR pause_requested OR pause_reason IN ('manual','quota','no_progress') OR (status='paused' AND trigger='scheduled'))
ORDER BY provider_id,created_at,id`, providerIDs)
	if err != nil {
		return nil, databaseError("list schedule collections", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, databaseError("read schedule collection", err)
		}
		result[run.ProviderID] = append(result[run.ProviderID], run)
	}
	return result, databaseError("list schedule collections", rows.Err())
}

// AttemptSchedule advances one due slot and queues or resumes its immutable run
// in the SAME transaction. The row lock makes duplicate ticks across processes
// harmless. Preparation failures consume the slot with a durable error instead
// of a hot retry loop. Persistence failures roll back the entire slot.
// Preparation must finish before this transaction begins. A continuation can
// uses its immutable snapshot, including after an adapter-only file rename.
func (s *Store) AttemptSchedule(ctx context.Context, source scheduling.Source, now time.Time, preparationErr error) (ScheduleClock, *model.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ScheduleClock{}, nil, databaseError("begin scheduled collection", err)
	}
	defer rollback(tx)
	clock, err := scanClock(tx.QueryRow(ctx, "SELECT "+clockColumns+" FROM ingest.schedule_clocks WHERE provider_id=$1 FOR UPDATE", source.ID))
	if err != nil {
		return clock, nil, databaseError("lock due schedule", err)
	}
	if !source.Active() || !clock.Active || clock.Revision != source.Revision || clock.NextRunAt == nil || clock.NextRunAt.After(now) {
		return clock, nil, nil
	}
	if err := lockProviderQueue(ctx, tx, source.ID); err != nil {
		return clock, nil, databaseError("lock scheduled collection queue", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+runColumns+` FROM ingest.runs
WHERE provider_id=$1 AND (status IN ('queued','running') OR pause_requested OR pause_reason IN ('manual','quota','no_progress') OR (status='paused' AND trigger='scheduled'))
ORDER BY created_at,id FOR UPDATE`, source.ID)
	if err != nil {
		return clock, nil, databaseError("read scheduled continuation", err)
	}
	var runs []model.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return clock, nil, databaseError("read scheduled continuation", err)
		}
		runs = append(runs, run)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return clock, nil, databaseError("read scheduled continuations", err)
	}
	for index, run := range runs {
		if run.Status != model.StatusRunning {
			continue
		}
		recovered, _, err := recoverRunTx(ctx, tx, run, preparationErr == nil)
		if err != nil {
			return clock, nil, err
		}
		runs[index] = recovered
	}
	state, resume, message := source.RunState(runs)
	if state == "running" {
		message = "An active collection prevented this scheduled attempt; the next scheduled tick will retry"
	}
	var queued *model.Run
	if state == "waiting" || state == "paused" {
		config := source.Provider
		if resume != nil {
			config = resume.Config
		}
		if preparationErr != nil {
			message = "Required secrets or saved source settings are unavailable; repair them before the next scheduled tick"
		} else {
			var run model.Run
			if resume != nil {
				if err := lockProviderSecrets(ctx, tx, config); err != nil {
					return clock, nil, err
				}
				run, err = scanRun(tx.QueryRow(ctx, "UPDATE ingest.runs SET status='queued',cancel_requested=FALSE,pause_requested=FALSE,pause_reason='',finished_at=NULL,error='' WHERE id=$1 AND NOT pause_requested AND pause_reason='budget' RETURNING "+runColumns, resume.ID))
				if err == nil {
					err = lifecycleEvent(ctx, tx, run.ID, "resumed", "Scheduled collection queued for continuation")
				}
			} else {
				config.Schedule, err = scheduling.Settings(source.Provider)
				if err != nil {
					return clock, nil, err
				}
				mode := source.Plan.Mode
				mode, initialFull, modeErr := initialCollectionMode(ctx, tx, config, mode)
				if modeErr != nil {
					return clock, nil, modeErr
				}
				if source.Plan.FullEvery > 0 && clock.FullDueAt != nil && !clock.FullDueAt.After(now) {
					mode = model.ModeFull
				}
				if mode == model.ModeMetadata && !config.SupportsMetadata() {
					return clock, nil, model.ErrInvalid
				}
				if err := lockProviderSecrets(ctx, tx, config); err != nil {
					return clock, nil, err
				}
				settings, settingsErr := scanCollectionSettings(tx.QueryRow(ctx, "SELECT "+collectionSettingsColumns+" FROM ingest.collection_settings WHERE singleton"))
				if settingsErr != nil {
					return clock, nil, settingsErr
				}
				snapshot := model.Run{Config: config}
				settings.ApplyDefaults(&snapshot)
				config = snapshot.Config
				policy, encodeErr := json.Marshal(snapshot.Policy)
				if encodeErr != nil {
					return clock, nil, errors.New("scheduled collection policy cannot be encoded")
				}
				var encoded []byte
				encoded, err = json.Marshal(config)
				if err != nil {
					return clock, nil, errors.New("scheduled provider snapshot cannot be encoded")
				}
				var id string
				id, err = newRunID()
				if err != nil {
					return clock, nil, err
				}
				run, err = scanRun(tx.QueryRow(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,max_pages,revision,config,trigger,policy,initial_full,storage_mode)
VALUES($1,$2,$3,$4,'queued',$5,$6,$7,'scheduled',$8,$9,(SELECT mode FROM ingest.storage_settings WHERE singleton)) RETURNING `+runColumns, id, source.ID, config.Name, mode, config.Schedule.MaxPages, source.Revision, encoded, policy, initialFull))
				if err == nil {
					err = lifecycleEvent(ctx, tx, run.ID, "queued", "Scheduled collection queued")
				}
				if err == nil {
					err = initialFullEvent(ctx, tx, run)
				}
			}
			if err != nil {
				return clock, nil, databaseError("queue scheduled collection", err)
			}
			queued = &run
		}
	}
	if queued != nil && resume == nil && queued.Mode == model.ModeFull && source.Plan.FullEvery > 0 {
		deadline := now.Add(source.Plan.FullEvery).UTC()
		clock.FullDueAt = &deadline
	}
	next := source.Plan.Next(now)
	if next.IsZero() {
		return clock, nil, model.ErrInvalid
	}
	clock, err = scanClock(tx.QueryRow(ctx, `UPDATE ingest.schedule_clocks
SET next_run_at=$2,last_attempt_at=$3,last_error=$4,full_due_at=$5 WHERE provider_id=$1 RETURNING `+clockColumns, source.ID, next, now, message, clock.FullDueAt))
	if err != nil {
		return clock, nil, databaseError("advance schedule clock", err)
	}
	level, kind, runID := "error", "schedule.failed", ""
	if queued != nil {
		level, kind, runID = "info", "schedule.queued", queued.ID
	} else if state == "running" {
		level, kind = "info", "schedule.skipped"
	}
	if err := recordActivityTx(ctx, tx, level, kind, message, source.ID, runID, nil); err != nil {
		return clock, nil, databaseError("record scheduled collection activity", err)
	}
	if err := notify(ctx, tx); err != nil {
		return clock, nil, databaseError("notify scheduled collection", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return clock, nil, databaseError("commit scheduled collection", err)
	}
	return clock, queued, nil
}
