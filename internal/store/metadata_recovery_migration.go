package store

const metadataRecoverySchema = `
ALTER TABLE aiosources.aio_state ADD COLUMN recovery_started_at TIMESTAMPTZ;
ALTER TABLE aiosources.aio_state ADD COLUMN applied_through TIMESTAMPTZ;
ALTER TABLE aiosources.aio_state ADD COLUMN provider_retry_at TIMESTAMPTZ;
CREATE INDEX ol_change_jobs_next_attempt ON aiosources.ol_change_jobs(next_attempt);
DROP INDEX aiosources.aio_works_refresh;
CREATE INDEX aio_works_refresh ON aiosources.aio_works(updated_at,media_type,tmdb_id) WHERE media_type IN ('movie','tv');
UPDATE aiosources.aio_state SET applied_through=changes_through
 WHERE last_sync IS NOT NULL AND NOT EXISTS(SELECT 1 FROM aiosources.aio_metadata_jobs);
`
