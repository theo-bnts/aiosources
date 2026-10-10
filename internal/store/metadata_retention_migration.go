package store

// Existing archives and immutable run snapshots remain untouched. PostgreSQL's
// constant defaults mark historical payloads without rewriting their bytes.
const metadataRetentionSchema = `
ALTER TABLE ingest.runs DROP CONSTRAINT runs_mode_check;
ALTER TABLE ingest.runs ADD CONSTRAINT runs_mode_check CHECK(mode IN ('preview','incremental','full','metadata'));
ALTER TABLE ingest.pages ADD COLUMN payload_retained BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE ingest.raw_records ADD COLUMN payload_retained BOOLEAN NOT NULL DEFAULT TRUE;

CREATE OR REPLACE FUNCTION ingest.safe_activity_data(input JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE AS $$
 SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(input)
 WHERE (key IN ('page_id','page','pages','records','auxiliary_records','errors','committed_pages','max_pages','attempts','status_code','duration_ms','workers','bytes') AND jsonb_typeof(value)='number')
    OR (key IN ('done','emitted','payload_retained') AND jsonb_typeof(value)='boolean')
    OR (key='mode' AND value IN ('"preview"'::jsonb,'"incremental"'::jsonb,'"full"'::jsonb,'"metadata"'::jsonb))
    OR (key='trigger' AND value IN ('"manual"'::jsonb,'"scheduled"'::jsonb))
    OR (key='status' AND value IN ('"queued"'::jsonb,'"running"'::jsonb,'"succeeded"'::jsonb,'"failed"'::jsonb,'"paused"'::jsonb,'"cancelled"'::jsonb))
    OR (key IN ('provider_id','run_id','backup_id') AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
    OR (key='failure_code' AND value IN ('"authentication"'::jsonb,'"certificate"'::jsonb,'"network"'::jsonb,'"timeout"'::jsonb,'"http"'::jsonb,'"parse"'::jsonb,'"stalled"'::jsonb,'"configuration"'::jsonb,'"unknown"'::jsonb))
    OR (key='health_code' AND value IN ('"storage_low"'::jsonb,'"source_stale"'::jsonb,'"authentication"'::jsonb,'"certificate"'::jsonb,'"run_stuck"'::jsonb,'"journal_growth"'::jsonb,'"database_unavailable"'::jsonb))
    OR (key='version' AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^(dev|v?[0-9]+[.][0-9]+[.][0-9]+([-+][A-Za-z0-9.-]+)?)$');
$$;
CREATE OR REPLACE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'run.queued' THEN 'Collection queued'
 WHEN 'run.started' THEN 'Collection attempt started'
 WHEN 'run.resumed' THEN 'Collection queued for resume'
 WHEN 'run.succeeded' THEN 'Collection succeeded'
 WHEN 'run.failed' THEN 'Collection failed; review its observations and source configuration'
 WHEN 'run.no_progress' THEN 'Collection has reached its useful-progress threshold without a new source identity; review the observations'
 WHEN 'run.paused' THEN 'Collection paused with a resumable checkpoint'
 WHEN 'run.pause_requested' THEN 'Collection pause requested; the current response checkpoint will be saved before stopping'
 WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'run.cancel_requested' THEN 'Collection cancellation requested'
 WHEN 'run.page_saved' THEN 'Response observations and checkpoint saved'
 WHEN 'run.page_error' THEN 'Response error recorded; checkpoint unchanged'
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
