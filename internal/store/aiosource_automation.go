package store

import (
	"context"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Polling automation needs only indexed existence checks, not catalog-wide counts.
func (s *Store) AIOAutomationStatus(ctx context.Context) (model.AIOStatus, error) {
	var v model.AIOStatus
	err := s.pool.QueryRow(ctx, `SELECT
 CASE WHEN EXISTS(SELECT 1 FROM ingest.aio_works) THEN 1 ELSE 0 END,
 CASE WHEN EXISTS(SELECT 1 FROM ingest.aio_match_queue) THEN 1 ELSE 0 END,
 EXISTS(SELECT 1 FROM ingest.aio_metadata_jobs WHERE next_attempt<=now()),
 last_sync,changes_through,inventory_date,sync_retry_at,sync_failures,sync_error,last_match,recovery_started_at,applied_through,provider_retry_at FROM ingest.aio_state`).Scan(
		&v.Works, &v.MatchingPending, &v.MetadataDue, &v.LastSync, &v.ChangesThrough, &v.InventoryDate, &v.SyncRetryAt, &v.SyncFailures, &v.SyncError, &v.LastMatch, &v.RecoveryStartedAt, &v.AppliedThrough, &v.ProviderRetryAt)
	return v, databaseError("read metadata automation", err)
}

// Settings and debrid activity do not need catalog-wide identification counts.
// Keep the existence checks separate from the exact Statistics projection.
func (s *Store) AIOControlStatus(ctx context.Context) (model.AIOStatus, error) {
	v, err := s.AIOAutomationStatus(ctx)
	if err == nil {
		err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE NOT failed),count(*) FILTER (WHERE failed) FROM ingest.aio_cleanup`).Scan(&v.CleanupPending, &v.CleanupFailed)
	}
	return v, databaseError("read metadata controls", err)
}

// The metadata worker owns its advisory lock before advancing this durable retry
// clock. A killed process therefore retains a bounded retry delay and its queue.
func (s *Store) AIOBeginSync(ctx context.Context) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE ingest.aio_state SET sync_failures=LEAST(sync_failures+1,30),sync_retry_at=GREATEST(provider_retry_at,now()+INTERVAL '1 minute') RETURNING sync_failures`).Scan(&attempts)
	return attempts, databaseError("start information update", err)
}

func (s *Store) AIOFailedSync(ctx context.Context, retry time.Time, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.aio_state SET sync_retry_at=GREATEST(provider_retry_at,$1),sync_error=$2`, retry, message)
	return databaseError("retain information update retry", err)
}
