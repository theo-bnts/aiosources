package store

import (
	"context"
	"time"
)

// Provider-requested waits survive abrupt restarts as well as ordinary errors.
func (s *Store) RetainAIOMetadataCooldown(ctx context.Context, until time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.aio_state SET provider_retry_at=GREATEST(provider_retry_at,$1),sync_retry_at=GREATEST(sync_retry_at,$1)`, until)
	return databaseError("retain information provider cooldown", err)
}

// A lost provider change window requires fresh details, not erasing the local
// catalog. Persist the refresh boundary before starting a new discovery window.
func (s *Store) BeginAIOMetadataRecovery(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.aio_state SET recovery_started_at=$1,changes_through=$2`, at, at.Add(-24*time.Hour))
	return databaseError("begin information recovery", err)
}

// The existing freshness index bounds each refill. Already queued work retains
// its revision and retry clock; refreshed records fall out of the recovery set.
func (s *Store) QueueAIOMetadataRecovery(ctx context.Context) error {
	limit, err := s.RuntimeValue(ctx, "metadata", "queue_batch")
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id)
 SELECT w.media_type,w.tmdb_id FROM ingest.aio_works w CROSS JOIN ingest.aio_state s
 WHERE s.recovery_started_at IS NOT NULL AND w.updated_at<s.recovery_started_at
 AND w.media_type IN ('movie','tv')
 AND NOT EXISTS(SELECT 1 FROM ingest.aio_metadata_jobs j WHERE j.media_type=w.media_type AND j.tmdb_id=w.tmdb_id)
 ORDER BY w.updated_at,w.media_type,w.tmdb_id LIMIT $1 ON CONFLICT DO NOTHING`, limit)
	return databaseError("queue information recovery", err)
}

// Discovery and application have different checkpoints. Deferred details must
// not make a partially applied catalog look current, or lose the retry cadence.
func (s *Store) AIOCompletedSync(ctx context.Context, next time.Time) error {
	_, err := s.pool.Exec(ctx, `WITH progress AS MATERIALIZED (
 SELECT NOT EXISTS(SELECT 1 FROM ingest.aio_metadata_jobs)
 AND NOT EXISTS(SELECT 1 FROM ingest.aio_works w CROSS JOIN ingest.aio_state s
  WHERE s.recovery_started_at IS NOT NULL AND w.media_type IN ('movie','tv') AND w.updated_at<s.recovery_started_at) AS complete,
 (SELECT min(next_attempt) FROM ingest.aio_metadata_jobs) AS retry
 ) UPDATE ingest.aio_state SET
 last_sync=CASE WHEN p.complete THEN now() ELSE last_sync END,
 applied_through=CASE WHEN p.complete THEN changes_through ELSE applied_through END,
 recovery_started_at=CASE WHEN p.complete THEN NULL ELSE recovery_started_at END,
 sync_retry_at=GREATEST(CASE WHEN provider_retry_at>now() THEN provider_retry_at END,CASE WHEN p.complete THEN NULL ELSE LEAST(p.retry,$1) END),
 provider_retry_at=CASE WHEN provider_retry_at>now() THEN provider_retry_at END,
 sync_failures=0,sync_error='' FROM progress p`, next)
	return databaseError("finish metadata synchronization", err)
}
