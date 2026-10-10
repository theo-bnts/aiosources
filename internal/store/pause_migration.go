package store

const pauseSchema = `
ALTER TABLE ingest.runs ADD COLUMN pause_requested BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.runs ADD CONSTRAINT runs_manual_pause_state
    CHECK (NOT pause_requested OR (status IN ('running','paused','failed') AND NOT cancel_requested));
CREATE INDEX runs_manual_holds ON ingest.runs(provider_id) WHERE pause_requested;

CREATE OR REPLACE FUNCTION ingest.activity_kind(input TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN input IN ('queued','started','resumed','succeeded','failed','paused','pause_requested','cancelled','cancel_requested','page_saved','page_error','source_error') THEN 'run.'||input ELSE 'run.activity' END;
$$;
CREATE OR REPLACE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'run.queued' THEN 'Collection queued'
 WHEN 'run.started' THEN 'Collection attempt started'
 WHEN 'run.resumed' THEN 'Collection queued for resume'
 WHEN 'run.succeeded' THEN 'Collection succeeded'
 WHEN 'run.failed' THEN 'Collection failed; review its retained records and source configuration'
 WHEN 'run.paused' THEN 'Collection paused with a resumable checkpoint'
 WHEN 'run.pause_requested' THEN 'Collection pause requested; the current response will be retained before stopping'
 WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'run.cancel_requested' THEN 'Collection cancellation requested'
 WHEN 'run.page_saved' THEN 'Response page retained'
 WHEN 'run.page_error' THEN 'Response page retained with an error'
 WHEN 'run.source_error' THEN 'Source collection could not continue; review run events and system health'
 WHEN 'schedule.failed' THEN 'Scheduled collection could not be queued; review source configuration and service status'
 WHEN 'schedule.queued' THEN 'Scheduled collection queued'
 WHEN 'schedule.skipped' THEN 'Scheduled collection skipped because a collection is already active'
 WHEN 'service.started' THEN 'Administration service started'
 WHEN 'service.stopped' THEN 'Administration service stopped'
 WHEN 'service.failed' THEN 'Administration service failed'
 WHEN 'scheduler.started' THEN 'Collection scheduler started'
 WHEN 'scheduler.stopped' THEN 'Collection scheduler stopped'
 WHEN 'scheduler.failed' THEN 'Collection scheduler failed'
 WHEN 'backup.succeeded' THEN 'Encrypted backup completed'
 WHEN 'backup.failed' THEN 'Encrypted backup failed; review backup status and available storage'
 WHEN 'backup.restore_succeeded' THEN 'Isolated backup restoration verified'
 WHEN 'backup.restore_failed' THEN 'Backup restoration verification failed; review the backup and recovery key'
 WHEN 'health.warning' THEN 'System health needs attention; review the health dashboard'
 WHEN 'health.recovered' THEN 'A system health warning has recovered'
 ELSE 'Collection activity recorded' END;
$$;
`
