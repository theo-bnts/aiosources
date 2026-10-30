package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"time"
)

func (s *Store) AIOResource(ctx context.Context, service, account, key string) (model.AIOResource, bool, error) {
	v := model.AIOResource{Service: service, Account: account, Key: key}
	err := s.pool.QueryRow(ctx, `SELECT hashes,checked_at,last_attempt,retry FROM ingest.aio_resources WHERE service=$1 AND account=$2 AND resource=$3`, service, account, key).Scan(&v.Hashes, &v.CheckedAt, &v.LastAttempt, &v.Retry)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, databaseError("read availability resource", err)
}
func (s *Store) SaveAIOResource(ctx context.Context, v model.AIOResource) error {
	_, err := s.pool.Exec(ctx, `WITH generation AS MATERIALIZED(SELECT 1 FROM ingest.aio_state WHERE cache_generation=$8 FOR SHARE)
INSERT INTO ingest.aio_resources(service,account,resource,hashes,checked_at,last_attempt,retry) SELECT $1,$2,$3,$4,$5,$6,$7 FROM generation ON CONFLICT(service,account,resource) DO UPDATE SET hashes=EXCLUDED.hashes,checked_at=EXCLUDED.checked_at,last_attempt=EXCLUDED.last_attempt,retry=EXCLUDED.retry`, v.Service, v.Account, v.Key, v.Hashes, v.CheckedAt, v.LastAttempt, v.Retry, v.Generation)
	return databaseError("save availability resource", err)
}
func (s *Store) AIOAvailability(ctx context.Context, service, account string, hashes []string, after time.Time) ([]model.AIOAvailability, error) {
	rows, err := s.pool.Query(ctx, `SELECT hash,cached,checked_at,files FROM ingest.aio_availability WHERE service=$1 AND account=$2 AND hash=ANY($3) AND checked_at>$4 AND checked_at<=now()`, service, account, hashes, after)
	if err != nil {
		return nil, databaseError("read availability", err)
	}
	defer rows.Close()
	out := []model.AIOAvailability{}
	for rows.Next() {
		var v model.AIOAvailability
		var files []byte
		if err = rows.Scan(&v.Hash, &v.Cached, &v.CheckedAt, &files); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(files, &v.Files); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, databaseError("read availability", rows.Err())
}
func (s *Store) SaveAIOAvailability(ctx context.Context, service, account string, v model.AIOAvailability) error {
	files := v.Files
	if files == nil {
		files = []model.AIOFile{}
	}
	b, err := json.Marshal(files)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `WITH generation AS MATERIALIZED(SELECT 1 FROM ingest.aio_state WHERE cache_generation=$7 FOR SHARE)
INSERT INTO ingest.aio_availability(service,account,hash,cached,checked_at,files) SELECT $1,$2,$3,$4,$5,$6 FROM generation ON CONFLICT(service,account,hash) DO UPDATE SET cached=EXCLUDED.cached,checked_at=EXCLUDED.checked_at,files=EXCLUDED.files WHERE ingest.aio_availability.checked_at<=EXCLUDED.checked_at`, service, account, v.Hash, v.Cached, v.CheckedAt, b, v.Generation)
	if err != nil {
		return databaseError("save availability", err)
	}
	if v.Shareable {
		return s.SaveSharedAvailability(ctx, service, v)
	}
	return nil
}

func (s *Store) ForgetAIOAvailability(ctx context.Context, service, account, hash string, observed time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ingest.aio_availability WHERE service=$1 AND account=$2 AND hash=$3 AND checked_at<=$4`, service, account, hash, observed)
	return databaseError("invalidate account availability", err)
}

func (s *Store) AddAIOCleanup(ctx context.Context, v model.AIOCleanup) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ingest.aio_cleanup(account,hash,magnet_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.Account, v.Hash, v.ID)
	return databaseError("persist temporary magnet ownership", err)
}
func (s *Store) AIOCleanup(ctx context.Context, account string) ([]model.AIOCleanup, error) {
	rows, err := s.pool.Query(ctx, `SELECT account,hash,magnet_id,failed FROM ingest.aio_cleanup WHERE account=$1 ORDER BY created_at`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AIOCleanup{}
	for rows.Next() {
		var v model.AIOCleanup
		if err = rows.Scan(&v.Account, &v.Hash, &v.ID, &v.Failed); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) FinishAIOCleanup(ctx context.Context, v model.AIOCleanup, success bool) error {
	var err error
	if success {
		_, err = s.pool.Exec(ctx, `DELETE FROM ingest.aio_cleanup WHERE account=$1 AND magnet_id=$2`, v.Account, v.ID)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE ingest.aio_cleanup SET failed=TRUE WHERE account=$1 AND magnet_id=$2`, v.Account, v.ID)
	}
	return databaseError("finish temporary magnet cleanup", err)
}
func (s *Store) ResetAIOCache(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT cache_generation FROM ingest.aio_state FOR UPDATE;DELETE FROM ingest.aio_availability;DELETE FROM ingest.aio_shared_availability;DELETE FROM ingest.aio_resources;UPDATE ingest.aio_state SET cache_generation=cache_generation+1`)
	return databaseError("reset availability cache", err)
}

// Incident transitions and logs commit together, so restarts cannot
// forget an unresolved provider failure or repeatedly announce recovery.
func (s *Store) AIOIncident(ctx context.Context, service string, active bool) error {
	name := model.ServiceName(service)
	if name == "" {
		return model.ErrInvalid
	}
	logService := service
	if service == "cleanup" {
		logService = "alldebrid"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, providerLockKey("aiosource-incident:"+service)); err != nil {
		return err
	}
	var previous bool
	err = tx.QueryRow(ctx, `SELECT active FROM ingest.aio_incidents WHERE service=$1`, service).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if previous == active {
		return nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_incidents(service,active) VALUES($1,$2) ON CONFLICT(service) DO UPDATE SET active=EXCLUDED.active,updated_at=now()`, service, active); err != nil {
		return err
	}
	event, err := newRunID()
	if err != nil {
		return err
	}
	level, title, message := "info", name+" recovered", name+" is responding again."
	if active {
		level = "error"
		title = name + " unavailable"
		message = name + " could not complete the operation. Check its credentials and status."
	}
	if service == "cleanup" {
		title = "AllDebrid cleanup recovered"
		message = "Temporary AllDebrid magnet cleanup is working again."
		if active {
			level = "warn"
			title = "AllDebrid cleanup needs attention"
			message = "A temporary AllDebrid magnet could not be deleted. Inspect the account before retrying."
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,title,message,service) VALUES($1,$2,'aiosource.incident',$3,$4,$5)`, "activity-"+event, level, title, message, logService); err != nil {
		return err
	}
	return databaseError("record provider incident", tx.Commit(ctx))
}

func (s *Store) AIOCacheGeneration(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT cache_generation FROM ingest.aio_state`).Scan(&n)
	return n, databaseError("read cache generation", err)
}
