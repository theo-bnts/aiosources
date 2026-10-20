package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const operationColumns = `id,kind,service,trigger,resource,status,phase,processed,errors,results,created_at,updated_at,finished_at,error`

func scanOperation(row scanner) (model.OperationRun, error) {
	var run model.OperationRun
	var results []byte
	err := row.Scan(&run.ID, &run.Kind, &run.Service, &run.Trigger, &run.Resource, &run.Status, &run.Phase, &run.Processed, &run.Errors, &results, &run.CreatedAt, &run.UpdatedAt, &run.FinishedAt, &run.Error)
	if err == nil {
		err = json.Unmarshal(results, &run.Results)
	}
	return run, err
}

// The same lock that protects the actual worker protects recovery. A live
// worker in another instance must never be marked interrupted by this console.
func (s *Store) RecoverOperations(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `WITH interrupted AS (
 UPDATE ingest.operation_runs SET status='interrupted',finished_at=now(),updated_at=now(),error='The worker was interrupted. Saved data and pending work are preserved.'
 WHERE status='running' AND pg_try_advisory_xact_lock(lock_key) RETURNING id,processed,errors
) INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT id,'interrupted',processed,errors FROM interrupted`)
	return databaseError("recover operation history", err)
}

// BeginOperation is called only after acquiring the existing AIOLock for
// lockName. It adds no extra long-lived connection or independent worker lease.
func (s *Store) BeginOperation(ctx context.Context, run model.OperationRun, lockName string) (model.OperationRun, error) {
	if lockName == "" || !validOperation(run) {
		return run, model.ErrInvalid
	}
	id, err := newRunID()
	if err != nil {
		return run, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return run, databaseError("begin operation history", err)
	}
	defer rollback(tx)
	key := providerLockKey("aiosource:" + lockName)
	_, err = tx.Exec(ctx, `WITH interrupted AS (
 UPDATE ingest.operation_runs SET status='interrupted',finished_at=now(),updated_at=now(),error='The worker was interrupted. Saved data and pending work are preserved.'
 WHERE status='running' AND lock_key=$1 RETURNING id,processed,errors
) INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT id,'interrupted',processed,errors FROM interrupted`, key)
	if err != nil {
		return run, databaseError("retain interrupted operation", err)
	}
	run, err = scanOperation(tx.QueryRow(ctx, `INSERT INTO ingest.operation_runs(id,kind,service,trigger,resource,lock_key,status) VALUES($1,$2,$3,$4,$5,$6,'running') RETURNING `+operationColumns, id, run.Kind, run.Service, run.Trigger, run.Resource, key))
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) VALUES($1,'configuration',0,0)`, id)
	}
	if err != nil {
		return run, databaseError("record operation start", err)
	}
	return run, databaseError("commit operation start", tx.Commit(ctx))
}

func validOperation(run model.OperationRun) bool {
	serviceOK := (run.Kind == "metadata" && run.Service == "tmdb") || (run.Kind == "matching" && run.Service == "local") || ((run.Kind == "cache" || run.Kind == "download") && (run.Service == "alldebrid" || run.Service == "torbox")) || (run.Kind == "cleanup" && run.Service == "alldebrid")
	return serviceOK && (run.Trigger == "manual" || run.Trigger == "automatic" || run.Trigger == "playback" || (run.Trigger == "public" && run.Kind == "download")) && len(run.Resource) <= 256
}

func (s *Store) SaveOperation(ctx context.Context, run model.OperationRun) error {
	if run.Processed < 0 || run.Errors < 0 {
		return model.ErrInvalid
	}
	for key, value := range run.Results {
		if value < 0 || (key != "matched" && key != "ambiguous" && key != "unmatched" && key != "cached" && key != "uncached" && key != "cleaned") {
			return model.ErrInvalid
		}
	}
	results, err := json.Marshal(run.Results)
	if err != nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin operation checkpoint", err)
	}
	defer rollback(tx)
	var previous string
	err = tx.QueryRow(ctx, `SELECT phase FROM ingest.operation_runs WHERE id=$1 AND status='running' FOR UPDATE`, run.ID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrConflict
	}
	if err != nil {
		return databaseError("read operation checkpoint", err)
	}
	_, err = tx.Exec(ctx, `UPDATE ingest.operation_runs SET phase=$2,processed=$3,errors=$4,results=$5,status=$6,error=$7,updated_at=now(),finished_at=CASE WHEN $6='running' THEN NULL ELSE now() END WHERE id=$1`, run.ID, run.Phase, run.Processed, run.Errors, results, run.Status, run.Error)
	if err == nil && (previous != run.Phase || run.Status != "running") {
		phase := run.Phase
		if run.Status != "running" && !(run.Kind == "download" && run.Status == "succeeded" && run.Phase == "waiting") {
			phase = run.Status
		}
		_, err = tx.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) VALUES($1,$2,$3,$4)`, run.ID, phase, run.Processed, run.Errors)
	}
	if err != nil {
		return databaseError("save operation checkpoint", err)
	}
	return databaseError("commit operation checkpoint", tx.Commit(ctx))
}

func (s *Store) Operation(ctx context.Context, id string) (model.OperationRun, error) {
	run, err := scanOperation(s.pool.QueryRow(ctx, `SELECT `+operationColumns+` FROM ingest.operation_runs WHERE id=$1`, id))
	return run, databaseError("read operation history", err)
}

func (s *Store) ListOperations(ctx context.Context, opts model.OperationFilter) (model.List[model.OperationRun], error) {
	opts.ListOptions = pageOptions(opts.ListOptions)
	result := model.List[model.OperationRun]{Items: []model.OperationRun{}, Limit: opts.Limit, Offset: opts.Offset}
	where := ` WHERE ($1='' OR kind=$1) AND ($2='' OR service=$2) AND ($3='' OR status=$3) AND ($4<>'metadata' OR kind IN ('metadata','matching')) AND ($4<>'debrid' OR kind IN ('cache','cleanup','download'))`
	args := []any{opts.Kind, opts.Service, opts.Status, opts.Group}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.operation_runs`+where, args...).Scan(&result.Total); err != nil {
		return result, databaseError("count operation history", err)
	}
	args = append(args, opts.Limit, opts.Offset)
	rows, err := s.pool.Query(ctx, `SELECT `+operationColumns+` FROM ingest.operation_runs`+where+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`, args...)
	if err != nil {
		return result, databaseError("list operation history", err)
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanOperation(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, run)
	}
	return result, databaseError("read operation history", rows.Err())
}

func (s *Store) OperationEvents(ctx context.Context, id string, opts model.ListOptions) (model.List[model.OperationEvent], error) {
	opts = pageOptions(opts)
	result := model.List[model.OperationEvent]{Items: []model.OperationEvent{}, Limit: opts.Limit, Offset: opts.Offset}
	if _, err := s.Operation(ctx, id); err != nil {
		return result, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.operation_events WHERE run_id=$1`, id).Scan(&result.Total); err != nil {
		return result, databaseError("count operation events", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT id,run_id,phase,processed,errors,created_at,duration_ms,http_status,error_code FROM ingest.operation_events WHERE run_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, id, opts.Limit, opts.Offset)
	if err != nil {
		return result, databaseError("list operation events", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event model.OperationEvent
		if err := rows.Scan(&event.ID, &event.RunID, &event.Phase, &event.Processed, &event.Errors, &event.CreatedAt, &event.DurationMS, &event.HTTPStatus, &event.ErrorCode); err != nil {
			return result, err
		}
		result.Items = append(result.Items, event)
	}
	return result, databaseError("read operation events", rows.Err())
}

func (s *Store) RecordProviderCall(ctx context.Context, runID, phase string, status int, duration int64, code string) error {
	if !(model.ProviderCall{Phase: phase, DurationMS: duration, HTTPStatus: status, ErrorCode: code}).Valid() {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin provider diagnostics", err)
	}
	defer rollback(tx)
	// Serialize the cap across concurrent checks without counting unbounded
	// history. The row lock is released before the next external request.
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM ingest.operation_runs WHERE id=$1 FOR UPDATE`, runID).Scan(&id); err != nil {
		return databaseError("lock provider diagnostics", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors,duration_ms,http_status,error_code) SELECT $1,$2,0,CASE WHEN $5='' THEN 0 ELSE 1 END,$3,$4,$5 WHERE NOT EXISTS(SELECT 1 FROM ingest.operation_events WHERE run_id=$1 OFFSET 999 LIMIT 1)`, runID, phase, duration, status, code)
	if err != nil {
		return databaseError("record provider call", err)
	}
	return databaseError("commit provider diagnostics", tx.Commit(ctx))
}
