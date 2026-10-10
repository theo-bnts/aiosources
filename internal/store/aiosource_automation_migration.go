package store

const aioAutomationSchema = `
ALTER TABLE ingest.aio_state
 ADD COLUMN sync_retry_at TIMESTAMPTZ,
 ADD COLUMN sync_failures INTEGER NOT NULL DEFAULT 0 CHECK(sync_failures>=0),
 ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';
`
