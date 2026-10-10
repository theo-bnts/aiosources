package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

// The two-int namespace is disjoint from provider worker locks (one bigint)
// and the schema migration namespace. Hash collisions only serialize sources;
// their request histories remain separate.
const providerRequestLockNamespace int32 = 1768843110

// ReserveProviderRequest records an admitted attempt before network I/O. A zero
// deadline means the reservation is durable; a nonzero deadline means no budget
// was consumed and the caller must wait, then try again. Limits must be validated
// by the source configuration. History is shared across runs and retained for a
// full day even when only a shorter window is currently enabled.
func (s *Store) ReserveProviderRequest(ctx context.Context, providerID string, limits model.RequestLimits) (time.Time, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return time.Time{}, databaseError("begin provider request reservation", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", providerRequestLockNamespace, int32(providerLockKey(providerID))); err != nil {
		return time.Time{}, databaseError("lock provider request reservation", err)
	}
	// Read the database wall clock after acquiring the lock, not the transaction
	// start time, which may precede another caller's committed admission.
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return time.Time{}, databaseError("read provider request clock", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ingest.provider_requests
WHERE provider_id=$1 AND admitted_at <= $2::timestamptz - INTERVAL '24 hours'`, providerID, now); err != nil {
		return time.Time{}, databaseError("prune provider request history", err)
	}
	// Each index scan stops at the Nth most recent admission inside its window.
	// When a limit is lowered, the oldest admission is not necessarily the one
	// whose expiry makes space. The latest deadline must satisfy every window.
	var retryAt *time.Time
	if err := tx.QueryRow(ctx, `
SELECT MAX(request.admitted_at + quota.duration)
FROM (VALUES
    ($3::integer, INTERVAL '60 seconds'),
    ($4::integer, INTERVAL '3600 seconds'),
    ($5::integer, INTERVAL '86400 seconds')
) AS quota(request_limit, duration)
CROSS JOIN LATERAL (
    SELECT admitted_at FROM ingest.provider_requests
    WHERE provider_id=$1 AND admitted_at > $2::timestamptz - quota.duration
    ORDER BY admitted_at DESC
    LIMIT 1 OFFSET GREATEST(quota.request_limit - 1, 0)
) AS request
WHERE quota.request_limit > 0`, providerID, now, limits.PerMinute, limits.PerHour, limits.PerDay).Scan(&retryAt); err != nil {
		return time.Time{}, databaseError("check provider request limits", err)
	}
	if retryAt == nil {
		if _, err := tx.Exec(ctx, "INSERT INTO ingest.provider_requests(provider_id,admitted_at) VALUES($1,$2)", providerID, now); err != nil {
			return time.Time{}, databaseError("reserve provider request", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, databaseError("commit provider request reservation", err)
	}
	if retryAt != nil {
		return *retryAt, nil
	}
	return time.Time{}, nil
}
