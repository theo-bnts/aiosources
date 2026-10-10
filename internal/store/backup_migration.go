package store

const backupSchema = `
CREATE TABLE ingest.backup_settings (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 recipient TEXT NOT NULL DEFAULT '',
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 time_utc TEXT NOT NULL DEFAULT '03:00' CHECK(time_utc ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 revision BIGINT NOT NULL DEFAULT 1,
 next_run_at TIMESTAMPTZ,
 last_success_at TIMESTAMPTZ,
 last_failure_at TIMESTAMPTZ,
 CHECK(NOT enabled OR (recipient <> '' AND next_run_at IS NOT NULL))
);
INSERT INTO ingest.backup_settings(singleton) VALUES(TRUE);
CREATE TABLE ingest.backup_jobs (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('backup','verify')),
 backup_id TEXT NOT NULL DEFAULT '',
 trigger TEXT NOT NULL CHECK(trigger IN ('manual','scheduled')),
 status TEXT NOT NULL CHECK(status IN ('queued','running','succeeded','failed')),
 phase TEXT NOT NULL DEFAULT 'queued',
 failure_code TEXT NOT NULL DEFAULT '',
 bytes BIGINT NOT NULL DEFAULT 0,
 sha256 TEXT NOT NULL DEFAULT '',
 recipient TEXT NOT NULL DEFAULT '',
 cleanup_pending BOOLEAN NOT NULL DEFAULT FALSE,
 owner TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 report JSONB NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX backup_one_active ON ingest.backup_jobs((TRUE)) WHERE status IN ('queued','running');
CREATE INDEX backup_jobs_recent ON ingest.backup_jobs(created_at DESC,id DESC);
`
