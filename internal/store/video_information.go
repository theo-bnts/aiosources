package store

import "context"

// A video-only reset preserves the book catalog, its checkpoints and reviews.
// Workers hold the shared reset locks before entering this transaction.
func (s *Store) ResetVideoInformation(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
SELECT ingest.lock_catalog_entity_writes();
INSERT INTO ingest.aio_match_queue(provider_id,source_id)
 SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN ingest.aio_works w ON w.id=m.work_id
 WHERE w.media_type IN ('movie','tv')
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1;
DELETE FROM ingest.aio_works WHERE media_type IN ('movie','tv');
TRUNCATE ingest.aio_metadata_jobs,ingest.aio_season_requests,ingest.aio_inventory;
DELETE FROM ingest.catalog_entity_changes WHERE kind='metadata';
UPDATE ingest.aio_state SET last_sync=NULL,changes_through=NULL,inventory_date='',
 sync_retry_at=CASE WHEN provider_retry_at>now() THEN provider_retry_at END,
 sync_failures=0,sync_error='',recovery_started_at=NULL,applied_through=NULL,
 metadata_generation=metadata_generation+1;`)
	return databaseError("reset video information", err)
}
