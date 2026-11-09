package store

const bookReliabilitySchema = `
ALTER TABLE aiosources.ol_changes_state ADD COLUMN provider_retry_at TIMESTAMPTZ;
UPDATE aiosources.ol_changes_state SET provider_retry_at=retry_at WHERE retry_at>now();
ALTER TABLE aiosources.ol_import_state ADD COLUMN sync_failures INTEGER NOT NULL DEFAULT 0;
`
