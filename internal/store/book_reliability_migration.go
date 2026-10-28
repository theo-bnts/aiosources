package store

const bookReliabilitySchema = `
ALTER TABLE ingest.ol_changes_state ADD COLUMN provider_retry_at TIMESTAMPTZ;
UPDATE ingest.ol_changes_state SET provider_retry_at=retry_at WHERE retry_at>now();
ALTER TABLE ingest.ol_import_state ADD COLUMN sync_failures INTEGER NOT NULL DEFAULT 0;
`
