package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

const runColumns = `id,provider_id,provider_name,mode,status,cancel_requested,max_pages,pages,records,errors,revision,cursor,config,created_at,started_at,finished_at,error,traversal_done,trigger,distinct_records,pause_requested,policy,pause_reason,requests_without_new_ids,progress_warning_sent,known_page_streak,COALESCE(metadata_parent_run_id,''),initial_full`

type scanner interface{ Scan(...any) error }

func scanRun(row scanner) (model.Run, error) {
	var run model.Run
	var config, cursor, policy []byte
	err := row.Scan(&run.ID, &run.ProviderID, &run.ProviderName, &run.Mode, &run.Status, &run.CancelRequested, &run.MaxPages, &run.Pages, &run.Records, &run.Errors, &run.Revision, &cursor, &config, &run.CreatedAt, &run.StartedAt, &run.FinishedAt, &run.Error, &run.TraversalDone, &run.Trigger, &run.DistinctRecords, &run.PauseRequested, &policy, &run.PauseReason, &run.RequestsWithoutNewIDs, &run.ProgressWarningSent, &run.KnownPageStreak, &run.MetadataParentRunID, &run.InitialFull)
	if err != nil {
		return run, err
	}
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	if err := decoder.Decode(&run.Config); err != nil {
		return run, errors.New("stored provider snapshot is invalid")
	}
	if len(policy) != 0 {
		if err := json.Unmarshal(policy, &run.Policy); err != nil || run.Policy == nil || !run.Policy.Valid() {
			return run, errors.New("stored collection policy is invalid")
		}
	}
	run.Cursor = cursor
	return run, nil
}

func (s *Store) CreateRun(ctx context.Context, run model.Run) (model.Run, error) {
	return s.createRun(ctx, run, false)
}

// EnqueueRun resolves a requested Incremental to an initial Full atomically
// with queue admission. CreateRun persists an already-selected execution mode.
func (s *Store) EnqueueRun(ctx context.Context, run model.Run) (model.Run, error) {
	return s.createRun(ctx, run, true)
}

func (s *Store) createRun(ctx context.Context, run model.Run, resolveInitial bool) (model.Run, error) {
	if run.ProviderID == "" || run.Config.ID != run.ProviderID || run.InitialFull || run.MetadataParentRunID != "" || run.MaxPages < 0 || run.MaxPages > 10000 || (run.Mode != model.ModePreview && run.Mode != model.ModeIncremental && run.Mode != model.ModeFull && run.Mode != model.ModeMetadata) || (run.Mode == model.ModeMetadata && !run.Config.SupportsMetadata()) || (run.Policy != nil && !run.Policy.Valid()) {
		return model.Run{}, model.ErrInvalid
	}
	id, err := newRunID()
	if err != nil {
		return model.Run{}, err
	}
	if run.ProviderName == "" {
		run.ProviderName = run.Config.Name
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin collection", err)
	}
	defer rollback(tx)
	if run.Policy == nil || run.Config.RequestTimeout == "" {
		settings, err := scanCollectionSettings(tx.QueryRow(ctx, "SELECT "+collectionSettingsColumns+" FROM ingest.collection_settings WHERE singleton"))
		if err != nil {
			return model.Run{}, err
		}
		settings.ApplyDefaults(&run)
	}
	config, err := json.Marshal(run.Config)
	if err != nil {
		return model.Run{}, errors.New("cannot encode provider snapshot")
	}
	policy, err := json.Marshal(run.Policy)
	if err != nil {
		return model.Run{}, errors.New("cannot encode collection policy")
	}
	if err := lockProviderQueue(ctx, tx, run.ProviderID); err != nil {
		return model.Run{}, databaseError("lock collection queue", err)
	}
	if resolveInitial {
		run.Mode, run.InitialFull, err = initialCollectionMode(ctx, tx, run.Config, run.Mode)
		if err != nil {
			return model.Run{}, err
		}
	}
	if err := lockProviderSecrets(ctx, tx, run.Config); err != nil {
		return model.Run{}, err
	}
	created, err := scanRun(tx.QueryRow(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,max_pages,revision,config,policy,initial_full)
VALUES($1,$2,$3,$4,'queued',$5,$6,$7,$8,$9) RETURNING `+runColumns, id, run.ProviderID, run.ProviderName, run.Mode, run.MaxPages, run.Revision, config, policy, run.InitialFull))
	if err != nil {
		return model.Run{}, databaseError("create collection", err)
	}
	if err := lifecycleEvent(ctx, tx, id, "queued", "Collection queued"); err != nil {
		return model.Run{}, databaseError("record collection event", err)
	}
	if err := initialFullEvent(ctx, tx, created); err != nil {
		return model.Run{}, err
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify collection", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit collection", err)
	}
	return created, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (model.Run, error) {
	run, err := scanRun(s.pool.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1", id))
	return run, databaseError("read collection", err)
}

func (s *Store) HasActiveRun(ctx context.Context, providerID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ingest.runs WHERE provider_id=$1 AND status IN ('queued','running'))", providerID).Scan(&active)
	return active, databaseError("check active provider collection", err)
}

func (s *Store) LatestRuns(ctx context.Context, providerIDs []string) (map[string]model.Run, error) {
	result := make(map[string]model.Run, len(providerIDs))
	if len(providerIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT DISTINCT ON (provider_id) "+runColumns+" FROM ingest.runs WHERE provider_id=ANY($1::text[]) ORDER BY provider_id,created_at DESC,id DESC", providerIDs)
	if err != nil {
		return nil, databaseError("list latest provider collections", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, databaseError("read latest provider collection", err)
		}
		result[run.ProviderID] = run
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("list latest provider collections", err)
	}
	return result, nil
}

func (s *Store) ListRuns(ctx context.Context, opts model.ListOptions) (model.List[model.Run], error) {
	opts = pageOptions(opts)
	result := model.List[model.Run]{Items: []model.Run{}, Limit: opts.Limit, Offset: opts.Offset}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin collection listing", err)
	}
	defer rollback(tx)
	const filter = ` FROM ingest.runs WHERE ($1='' OR provider_id=$1) AND ($2='' OR status=$2) AND ($3='' OR id=$3)`
	if err := tx.QueryRow(ctx, "SELECT count(*)"+filter, opts.ProviderID, opts.Status, opts.RunID).Scan(&result.Total); err != nil {
		return result, databaseError("count collections", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+runColumns+filter+" ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5", opts.ProviderID, opts.Status, opts.RunID, opts.Limit, opts.Offset)
	if err != nil {
		return result, databaseError("list collections", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return result, databaseError("read collection", err)
		}
		result.Items = append(result.Items, run)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list collections", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, databaseError("finish collection listing", err)
	}
	return result, nil
}

func (s *Store) ClaimNext(ctx context.Context) (*model.Run, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, errors.New("database store is closed")
	}
	select {
	case s.claimSlots <- struct{}{}:
	default:
		return nil, nil
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		<-s.claimSlots
		return nil, databaseError("acquire collection session", err)
	}
	retained := false
	var heldKey *int64
	defer func() {
		if !retained {
			if heldKey != nil {
				releaseConnection(conn, *heldKey)
			} else {
				conn.Release()
			}
			<-s.claimSlots
		}
	}()
	excluded := []string{}
	for {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return nil, databaseError("begin collection claim", err)
		}
		// Serialize admission with other claimers and settings updates. A lower
		// limit holds queued work; it never interrupts already running attempts.
		var limit, running int
		if err := tx.QueryRow(ctx, "SELECT COALESCE(workers,$1) FROM ingest.collection_settings WHERE singleton FOR UPDATE", model.DefaultCollectionWorkers).Scan(&limit); err != nil {
			rollback(tx)
			return nil, databaseError("lock collection capacity", err)
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM ingest.runs WHERE status='running'").Scan(&running); err != nil {
			rollback(tx)
			return nil, databaseError("read active collection count", err)
		}
		if running >= limit {
			rollback(tx)
			return nil, nil
		}
		var candidateID, providerID string
		err = tx.QueryRow(ctx, `SELECT id,provider_id FROM ingest.runs AS candidate
WHERE status='queued' AND NOT pause_requested AND NOT (id=ANY($1::text[]))
AND (trigger<>'scheduled' OR NOT EXISTS (
    SELECT 1 FROM ingest.runs AS held WHERE held.provider_id=candidate.provider_id
    AND (held.pause_requested OR held.pause_reason IN ('manual','quota','no_progress'))))
ORDER BY created_at,id LIMIT 1`, excluded).Scan(&candidateID, &providerID)
		if errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return nil, nil
		}
		if err != nil {
			rollback(tx)
			return nil, databaseError("select queued collection", err)
		}
		// Match enqueue/resume/pause/scheduler ordering: provider queue before
		// run row. Re-read after this lock so an accepted older hold cannot be
		// bypassed by a scheduled candidate selected from an earlier snapshot.
		if err := lockProviderQueue(ctx, tx, providerID); err != nil {
			rollback(tx)
			return nil, databaseError("lock collection queue", err)
		}
		run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+` FROM ingest.runs AS candidate
WHERE id=$1 AND status='queued' AND NOT pause_requested
AND (trigger<>'scheduled' OR NOT EXISTS (
    SELECT 1 FROM ingest.runs AS held WHERE held.provider_id=candidate.provider_id
    AND (held.pause_requested OR held.pause_reason IN ('manual','quota','no_progress'))))
FOR UPDATE SKIP LOCKED`, candidateID))
		if errors.Is(err, pgx.ErrNoRows) {
			excluded = append(excluded, candidateID)
			rollback(tx)
			continue
		}
		if err != nil {
			rollback(tx)
			return nil, databaseError("lock queued collection", err)
		}
		key := providerLockKey(run.ProviderID)
		var locked bool
		if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
			rollback(tx)
			// The server may have acquired the session lock before the response
			// was interrupted. Destroy this uncertain session rather than pool it.
			heldKey = &key
			return nil, databaseError("lock collection provider", err)
		}
		if !locked {
			excluded = append(excluded, run.ID)
			rollback(tx)
			continue
		}
		heldKey = &key
		if err := initializeRemoteCursor(ctx, tx, run); err != nil {
			rollback(tx)
			return nil, err
		}
		run, err = scanRun(tx.QueryRow(ctx, "UPDATE ingest.runs SET status='running',started_at=COALESCE(started_at,NOW()) WHERE id=$1 RETURNING "+runColumns, run.ID))
		if err != nil {
			rollback(tx)
			return nil, databaseError("claim collection", err)
		}
		if err := notify(ctx, tx); err != nil {
			rollback(tx)
			return nil, databaseError("notify collection claim", err)
		}
		if err := tx.Commit(ctx); err != nil {
			rollback(tx)
			return nil, databaseError("commit collection claim", err)
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, errors.New("database store is closed")
		}
		s.leases[run.ID] = &runLease{conn: conn, key: key}
		retained = true
		s.mu.Unlock()
		return &run, nil
	}
}

// RecoverInterrupted classifies orphaned attempts without admitting sources that
// are no longer eligible. The caller holds the registry lock through this call.
func (s *Store) RecoverInterrupted(ctx context.Context, eligibleProviders map[string]bool) error {
	// Reading candidates does not classify them: only a transaction that owns
	// the provider lock may transition an orphaned running row.
	rows, err := s.pool.Query(ctx, "SELECT id FROM ingest.runs WHERE status='running' OR (status='paused' AND pause_reason='interrupted') ORDER BY created_at,id")
	if err != nil {
		return databaseError("list interrupted collections", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return databaseError("read interrupted collection", err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return databaseError("list interrupted collections", err)
	}
	for _, id := range ids {
		if err := s.recoverRun(ctx, id, eligibleProviders); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) recoverRun(ctx context.Context, id string, eligibleProviders map[string]bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin collection recovery", err)
	}
	defer rollback(tx)
	var providerID string
	if err := tx.QueryRow(ctx, "SELECT provider_id FROM ingest.runs WHERE id=$1", id).Scan(&providerID); err != nil {
		return databaseError("read recovery provider", err)
	}
	// Recognize another process's live worker before waiting for its queue
	// transaction. Full publication may hold that queue for a long time, but
	// recovery has no work to do while the session lease is still owned.
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", providerLockKey(providerID)).Scan(&locked); err != nil {
		return databaseError("lock collection recovery", err)
	}
	if !locked {
		return nil
	}
	if err := lockProviderQueue(ctx, tx, providerID); err != nil {
		return databaseError("lock recovery queue", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 AND (status='running' OR (status='paused' AND pause_reason='interrupted')) FOR UPDATE SKIP LOCKED", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return databaseError("read collection recovery", err)
	}
	if _, recovered, err := recoverRunTx(ctx, tx, run, eligibleProviders[providerID]); err != nil || !recovered {
		return err
	}
	if err := notify(ctx, tx); err != nil {
		return databaseError("notify recovery", err)
	}
	return databaseError("commit collection recovery", tx.Commit(ctx))
}

// The caller owns the queue and run row locks. Check the provider lease here
// too: scheduled recovery calls this directly, without the startup preflight.
func recoverRunTx(ctx context.Context, tx pgx.Tx, run model.Run, eligible bool) (model.Run, bool, error) {
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", providerLockKey(run.ProviderID)).Scan(&locked); err != nil {
		return run, false, databaseError("lock collection recovery", err)
	}
	if !locked {
		return run, false, nil
	}
	status, reason := model.StatusPaused, model.PauseInterrupted
	message := "Collection interrupted; resume from the last committed checkpoint"
	var unsafeFailure bool
	// Only the latest durable outcome counts. A successfully retried earlier
	// authentication failure must not poison a later genuine interruption.
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT data->>'failure_code' IN ('authentication','certificate','configuration')
FROM ingest.events WHERE run_id=$1 AND kind IN ('page_error','source_error','page_saved')
ORDER BY id DESC LIMIT 1),FALSE)`, run.ID).Scan(&unsafeFailure); err != nil {
		return run, false, databaseError("read recovery safety", err)
	}
	if run.RequiresManualResume() {
		reason = run.PauseReason
		if run.PauseRequested {
			reason = model.PauseManual
		}
		message = "Collection paused; explicit resume is required"
	} else if unsafeFailure {
		status, reason = model.StatusFailed, ""
		message = "Source configuration needs attention before this collection can resume"
	} else if eligible {
		status, reason = model.StatusQueued, ""
		var active bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ingest.runs WHERE provider_id=$1 AND id<>$2 AND status IN ('queued','running'))", run.ProviderID, run.ID).Scan(&active); err != nil {
			return run, false, databaseError("check recovery queue", err)
		}
		if active {
			return run, false, nil
		}
		message = "Interrupted collection queued from its last committed checkpoint"
	}
	if run.CancelRequested {
		status, reason = model.StatusCancelled, ""
		message = "Collection cancelled"
	}
	if run.Status == model.StatusPaused && status == model.StatusPaused {
		return run, false, nil
	}
	recovered, err := scanRun(tx.QueryRow(ctx, "UPDATE ingest.runs SET status=$2,cancel_requested=FALSE,pause_requested=(pause_requested AND $2='paused'),pause_reason=$4,finished_at=CASE WHEN $2='queued' THEN NULL ELSE NOW() END,error=CASE WHEN $2='queued' THEN '' ELSE $3 END WHERE id=$1 RETURNING "+runColumns, run.ID, status, message, reason))
	if err != nil {
		return run, false, databaseError("recover collection", err)
	}
	if err := lifecycleEvent(ctx, tx, run.ID, string(status), message); err != nil {
		return run, false, databaseError("record recovery event", err)
	}
	return recovered, true, nil
}

func (s *Store) ResumeRun(ctx context.Context, id string) (model.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin collection resume", err)
	}
	defer rollback(tx)
	var providerID string
	if err := tx.QueryRow(ctx, "SELECT provider_id FROM ingest.runs WHERE id=$1", id).Scan(&providerID); err != nil {
		return model.Run{}, databaseError("read collection provider", err)
	}
	if err := lockProviderQueue(ctx, tx, providerID); err != nil {
		return model.Run{}, databaseError("lock collection queue", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return model.Run{}, databaseError("read collection resume", err)
	}
	if run.Status != model.StatusPaused && run.Status != model.StatusFailed && run.Status != model.StatusCancelled {
		return model.Run{}, model.ErrConflict
	}
	if err := lockProviderSecrets(ctx, tx, run.Config); err != nil {
		return model.Run{}, err
	}
	previousStatus := run.Status
	run, err = scanRun(tx.QueryRow(ctx, `UPDATE ingest.runs SET status='queued',cancel_requested=FALSE,pause_requested=FALSE,
requests_without_new_ids=CASE WHEN pause_reason='no_progress' THEN 0 ELSE requests_without_new_ids END,
progress_warning_sent=CASE WHEN pause_reason='no_progress' THEN FALSE ELSE progress_warning_sent END,
pause_reason='',finished_at=NULL,error='' WHERE id=$1 RETURNING `+runColumns, id))
	if err != nil {
		return model.Run{}, databaseError("resume collection", err)
	}
	var eventID string
	if err := tx.QueryRow(ctx, "INSERT INTO ingest.events(run_id,kind,message,data) VALUES($1,'resumed','Collection queued for resume',jsonb_build_object('from_status',$2::text)) RETURNING id::text", id, previousStatus).Scan(&eventID); err != nil {
		return model.Run{}, databaseError("record resume event", err)
	}
	if previousStatus == model.StatusFailed {
		if _, err := tx.Exec(ctx, "UPDATE ingest.runs SET coverage_attempt=$2 WHERE id=$1", id, eventID); err != nil {
			return model.Run{}, databaseError("retain coverage attempt", err)
		}
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify resume", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit collection resume", err)
	}
	return run, nil
}

// CoverageAttempt changes only after an explicit retry of a failed collection,
// not after automatic budget continuation or interrupted-worker recovery.
func (s *Store) CoverageAttempt(ctx context.Context, id string) (string, error) {
	var attempt string
	err := s.pool.QueryRow(ctx, `SELECT coverage_attempt FROM ingest.runs WHERE id=$1`, id).Scan(&attempt)
	return attempt, databaseError("read collection coverage attempt", err)
}

// RequestPause shares the finalization row lock: whichever transaction commits
// first determines whether a traversal publishes or remains held for Resume.
// Running requests remain running until their in-flight page has committed.
func (s *Store) RequestPause(ctx context.Context, id string) (model.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin collection pause", err)
	}
	defer rollback(tx)
	var providerID string
	if err := tx.QueryRow(ctx, "SELECT provider_id FROM ingest.runs WHERE id=$1", id).Scan(&providerID); err != nil {
		return model.Run{}, databaseError("read collection provider", err)
	}
	if err := lockProviderQueue(ctx, tx, providerID); err != nil {
		return model.Run{}, databaseError("lock collection queue", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return model.Run{}, databaseError("read collection pause", err)
	}
	if run.CancelRequested {
		return model.Run{}, model.ErrConflict
	}
	if run.PauseRequested {
		return run, nil
	}
	if run.Status != model.StatusQueued && run.Status != model.StatusRunning && run.Status != model.StatusPaused {
		return model.Run{}, model.ErrConflict
	}
	kind, message := "pause_requested", "Pause requested; the current response checkpoint will be saved before stopping"
	statement := "UPDATE ingest.runs SET pause_requested=TRUE,pause_reason='manual' WHERE id=$1 RETURNING "
	if run.Status != model.StatusRunning {
		kind, message = "paused", "Collection manually paused; resume from the last committed checkpoint"
		statement = "UPDATE ingest.runs SET status='paused',pause_requested=TRUE,pause_reason='manual',finished_at=NOW(),error='' WHERE id=$1 RETURNING "
	}
	run, err = scanRun(tx.QueryRow(ctx, statement+runColumns, id))
	if err != nil {
		return model.Run{}, databaseError("pause collection", err)
	}
	if err := lifecycleEvent(ctx, tx, id, kind, message); err != nil {
		return model.Run{}, databaseError("record pause event", err)
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify pause", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit collection pause", err)
	}
	return run, nil
}

func (s *Store) RequestCancel(ctx context.Context, id string) (model.Run, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin collection cancellation", err)
	}
	defer rollback(tx)
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return model.Run{}, databaseError("read collection cancellation", err)
	}
	var kind, message, statement string
	switch run.Status {
	case model.StatusQueued, model.StatusPaused, model.StatusFailed:
		if run.Status == model.StatusFailed && !run.PauseRequested {
			return run, nil
		}
		kind, message = "cancelled", "Collection cancelled"
		statement = "UPDATE ingest.runs SET status='cancelled',cancel_requested=FALSE,pause_requested=FALSE,pause_reason='',finished_at=NOW(),error='' WHERE id=$1 RETURNING "
	case model.StatusRunning:
		if run.CancelRequested {
			return run, nil
		}
		kind, message = "cancel_requested", "Cancellation requested"
		statement = "UPDATE ingest.runs SET cancel_requested=TRUE,pause_requested=FALSE,pause_reason='' WHERE id=$1 RETURNING "
	default:
		return run, nil
	}
	run, err = scanRun(tx.QueryRow(ctx, statement+runColumns, id))
	if err != nil {
		return model.Run{}, databaseError("cancel collection", err)
	}
	if err := lifecycleEvent(ctx, tx, id, kind, message); err != nil {
		return model.Run{}, databaseError("record cancellation event", err)
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify cancellation", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit cancellation", err)
	}
	return run, nil
}

func (s *Store) FinishRun(ctx context.Context, id string, status model.RunStatus, errorMessage string, reason model.PauseReason) (model.Run, error) {
	lease, err := s.ownedLease(id)
	if err != nil {
		if _, readErr := s.GetRun(ctx, id); readErr != nil {
			return model.Run{}, readErr
		}
		return model.Run{}, err
	}
	defer lease.mu.Unlock()
	defer s.releaseLease(id, lease)
	if status != model.StatusSucceeded && status != model.StatusPaused && status != model.StatusFailed && status != model.StatusCancelled {
		return model.Run{}, model.ErrConflict
	}
	tx, err := lease.conn.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin collection finish", err)
	}
	defer rollback(tx)
	// Finalization may enqueue Metadata. Keep the same queue-before-row order
	// as claim, pause, resume and scheduling, using the already-owned session.
	var providerID string
	if err := tx.QueryRow(ctx, "SELECT provider_id FROM ingest.runs WHERE id=$1", id).Scan(&providerID); err != nil {
		return model.Run{}, databaseError("read finishing provider", err)
	}
	if err := lockProviderQueue(ctx, tx, providerID); err != nil {
		return model.Run{}, databaseError("lock collection finish queue", err)
	}
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 FOR UPDATE", id))
	if err != nil {
		return model.Run{}, databaseError("read collection finish", err)
	}
	if run.Status != model.StatusRunning {
		return model.Run{}, model.ErrConflict
	}
	if run.CancelRequested {
		status, errorMessage, reason = model.StatusCancelled, "", ""
	} else if run.PauseRequested {
		reason = model.PauseManual
		if status == model.StatusSucceeded || status == model.StatusPaused {
			status, errorMessage = model.StatusPaused, "collection manually paused; resume from the last committed checkpoint"
		}
	} else if run.PauseReason == model.PauseNoProgress && !run.TraversalDone && (status == model.StatusSucceeded || status == model.StatusPaused) {
		status, reason = model.StatusPaused, model.PauseNoProgress
		errorMessage = "Collection paused after responses without a new source identity"
	}
	if status != model.StatusPaused && !(status == model.StatusFailed && run.PauseRequested) {
		reason = ""
	}
	switch reason {
	case "", model.PauseManual, model.PauseBudget, model.PauseQuota, model.PauseNoProgress, model.PauseInterrupted:
	default:
		return model.Run{}, model.ErrInvalid
	}
	if status == model.StatusSucceeded && run.Mode != model.ModePreview && !run.TraversalDone {
		return model.Run{}, model.ErrConflict
	}
	if status == model.StatusSucceeded && run.Mode != model.ModePreview && remoteCatalog(run.Config) {
		// Match the journal's table/statement lock order before checking ownership.
		// Native publication cannot arrive between this check and replacement.
		if _, err := tx.Exec(ctx, "LOCK TABLE ingest.torrents IN ROW EXCLUSIVE MODE; SELECT pg_advisory_xact_lock(1768843109,3)"); err != nil {
			return model.Run{}, databaseError("lock remote publication", err)
		}
		var native bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ingest.torrents WHERE provider_id=$1 AND origin IS NULL)", run.ProviderID).Scan(&native); err != nil {
			return model.Run{}, databaseError("check remote destination ownership", err)
		}
		if native {
			status, errorMessage = model.StatusFailed, "Remote destination contains local records; use a new source identifier"
		} else {
			matches, err := remoteBaselineMatches(ctx, tx, run)
			if err != nil {
				return model.Run{}, err
			}
			if !matches {
				status, errorMessage = model.StatusFailed, "Remote catalogue baseline changed; start a new sync"
			}
		}
	}
	if status == model.StatusSucceeded && run.Mode != model.ModePreview {
		if _, err := tx.Exec(ctx, "SELECT set_config('ingest.publication_run_id',$1,true)", run.ID); err != nil {
			return model.Run{}, databaseError("set final publication context", err)
		}
		if err := publishStaged(ctx, tx, run); err != nil {
			return model.Run{}, err
		}
	}
	run, err = scanRun(tx.QueryRow(ctx, "UPDATE ingest.runs SET status=$2,cancel_requested=FALSE,pause_requested=(pause_requested AND $2 IN ('paused','failed')),pause_reason=$4,finished_at=NOW(),error=$3 WHERE id=$1 RETURNING "+runColumns, id, status, errorMessage, reason))
	if err != nil {
		return model.Run{}, databaseError("finish collection", err)
	}
	if err := lifecycleEvent(ctx, tx, id, string(status), "Collection "+string(status)); err != nil {
		return model.Run{}, databaseError("record completion event", err)
	}
	if run.Status == model.StatusSucceeded && (run.Mode == model.ModeFull || run.Mode == model.ModeIncremental) {
		if err := retainCollectionBaseline(ctx, tx, run); err != nil {
			return model.Run{}, err
		}
	}
	if run.Status == model.StatusSucceeded && (run.Mode == model.ModeIncremental || run.InitialFull) && run.Config.SupportsMetadata() && run.Config.Traversal.MetadataAfterIncremental {
		if err := queueMetadataAfterDiscovery(ctx, tx, run); err != nil {
			return model.Run{}, err
		}
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify completion", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit collection finish", err)
	}
	return run, nil
}

func lifecycleEvent(ctx context.Context, tx pgx.Tx, id, kind, message string) error {
	_, err := tx.Exec(ctx, "INSERT INTO ingest.events(run_id,kind,message) VALUES($1,$2,$3)", id, kind, message)
	return err
}

// RunRetryAt retains an explicit upstream cooldown across Pause and restart.
// A later source response observation supersedes the previous response deadline.
func (s *Store) RunRetryAt(ctx context.Context, id string) (time.Time, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(metadata->>'retry_at','')
FROM (SELECT metadata FROM ingest.pages WHERE run_id=$1 ORDER BY id DESC LIMIT 1) latest`, id).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, databaseError("read collection cooldown", err)
	}
	if value == "" {
		return time.Time{}, nil
	}
	until, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("stored collection cooldown is invalid")
	}
	return until, nil
}
