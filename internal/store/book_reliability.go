package store

import (
	"context"
	"time"
)

func (s *Store) RetainBookProviderCooldown(ctx context.Context, until time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET provider_retry_at=GREATEST(provider_retry_at,$1) WHERE singleton`, until)
	return databaseError("retain book provider cooldown", err)
}

func (s *Store) BeginBookSync(ctx context.Context) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE ingest.ol_import_state SET sync_failures=LEAST(sync_failures+1,30),retry_at=now()+interval '1 minute' WHERE singleton RETURNING sync_failures`).Scan(&attempts)
	return attempts, databaseError("start book information update", err)
}
