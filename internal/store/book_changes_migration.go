package store

const bookChangesSchema = `
ALTER TABLE ingest.aio_works ADD COLUMN ol_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.aio_works ADD COLUMN ol_seen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.aio_works ADD COLUMN ol_modified_at TIMESTAMPTZ;
ALTER TABLE ingest.ol_editions ADD COLUMN ol_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_editions ADD COLUMN ol_seen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_editions ADD COLUMN ol_modified_at TIMESTAMPTZ;
ALTER TABLE ingest.ol_authors ADD COLUMN ol_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_authors ADD COLUMN ol_seen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_authors ADD COLUMN ol_modified_at TIMESTAMPTZ;
ALTER TABLE ingest.ol_redirects ADD COLUMN ol_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_redirects ADD COLUMN ol_seen BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_redirects ADD COLUMN ol_modified_at TIMESTAMPTZ;
ALTER TABLE ingest.ol_import_state ADD COLUMN generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_import_state ADD COLUMN snapshot_at TIMESTAMPTZ;
ALTER TABLE ingest.ol_import_state ADD COLUMN prune_table INT NOT NULL DEFAULT 0;
ALTER TABLE ingest.ol_import_state ADD COLUMN prune_after TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.ol_import_state ADD COLUMN data_complete BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE ingest.ol_import_state SET data_complete=complete;
CREATE TABLE ingest.ol_deleted(
 id TEXT PRIMARY KEY,ol_revision BIGINT NOT NULL,ol_modified_at TIMESTAMPTZ
);
CREATE TABLE ingest.ol_change_jobs(
 id TEXT PRIMARY KEY,revision BIGINT NOT NULL CHECK(revision>=0),changed_at TIMESTAMPTZ NOT NULL,
 version BIGINT NOT NULL DEFAULT 1,attempts INT NOT NULL DEFAULT 0,
 next_attempt TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ol_change_jobs_due ON ingest.ol_change_jobs(changed_at DESC,id DESC,next_attempt);
CREATE TABLE ingest.ol_changes_state(
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 anchor_id TEXT NOT NULL DEFAULT '',anchor_at TIMESTAMPTZ,tail_since TIMESTAMPTZ,tail_start_id TEXT NOT NULL DEFAULT '',
 bootstrap_day DATE,bootstrap_complete BOOLEAN NOT NULL DEFAULT FALSE,
 changes_through TIMESTAMPTZ,last_sync TIMESTAMPTZ,checked_at TIMESTAMPTZ,
 retry_at TIMESTAMPTZ,needs_dump_after TIMESTAMPTZ,dump_checked_at TIMESTAMPTZ,
 error TEXT NOT NULL DEFAULT ''
);
INSERT INTO ingest.ol_changes_state(singleton) VALUES(TRUE);
UPDATE ingest.settings SET value=((value::jsonb-'openlibrary_days') || jsonb_build_object(
 'openlibrary_minutes',15,'revision',COALESCE((value::jsonb->>'revision')::bigint,1)+1))::text
WHERE key='aiosource.config.v2' AND value::jsonb ? 'openlibrary_days';
` + nativeBookIdentifiersSchema
