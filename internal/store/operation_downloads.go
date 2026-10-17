package store

import (
	"context"
	"encoding/json"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Download continuations contain only the selected local record and a provider
// object ID. Keys, announce URLs and generated download URLs are never retained.
// Their lifetime follows the seven-day operation history via the foreign key.
const operationDownloadsSchema = `CREATE TABLE ingest.operation_downloads (
 run_id TEXT PRIMARY KEY REFERENCES ingest.operation_runs(id) ON DELETE CASCADE,
 work_id BIGINT NOT NULL CHECK(work_id>0), input JSONB NOT NULL CHECK(jsonb_typeof(input)='object'),account TEXT NOT NULL
);`

func (s *Store) SaveOperationDownload(ctx context.Context, id string, download model.OperationDownload) error {
	download.Input.AllowUncached = false
	data, err := json.Marshal(download.Input)
	if err != nil {
		return model.ErrInvalid
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ingest.operation_downloads(run_id,work_id,input,account) VALUES($1,$2,$3,$4)
 ON CONFLICT(run_id) DO UPDATE SET input=EXCLUDED.input`, id, download.WorkID, data, download.Account)
	return databaseError("save download continuation", err)
}
func (s *Store) OperationDownload(ctx context.Context, id string) (model.OperationDownload, error) {
	var result model.OperationDownload
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT work_id,input,account FROM ingest.operation_downloads WHERE run_id=$1`, id).Scan(&result.WorkID, &data, &result.Account)
	if err == nil {
		err = json.Unmarshal(data, &result.Input)
	}
	return result, databaseError("read download continuation", err)
}

// Called with the same occurrence lock as the original preparation.
func (s *Store) ResumeDownloadOperation(ctx context.Context, id string) (model.OperationRun, error) {
	run, err := scanOperation(s.pool.QueryRow(ctx, `UPDATE ingest.operation_runs SET status='running',phase='configuration',error='',finished_at=NULL,updated_at=now()
 WHERE id=$1 AND kind='download' AND status<>'running' RETURNING `+operationColumns, id))
	return run, databaseError("continue download preparation", err)
}

// A stale selection cannot remain labelled Preparing forever. Do not race a
// live readiness check: it owns the same advisory lock as the saved operation.
func (s *Store) UnavailableOperationDownload(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `WITH stopped AS (
 UPDATE ingest.operation_runs SET status='failed',phase='failed',errors=GREATEST(errors,1),updated_at=now(),finished_at=now(),
 error='This saved preparation no longer matches the source or debrid account. Start again from Search.'
 WHERE id=$1 AND kind='download' AND status='succeeded' AND phase='waiting' AND pg_try_advisory_xact_lock(lock_key)
 RETURNING id,processed,errors)
 INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT id,'failed',processed,errors FROM stopped`, id)
	return databaseError("expire unavailable download preparation", err)
}
