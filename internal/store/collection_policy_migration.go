package store

const collectionPolicySchema = `
ALTER TABLE ingest.collection_settings
 ADD COLUMN max_quota_retries INTEGER NOT NULL DEFAULT 3 CHECK(max_quota_retries BETWEEN 0 AND 10),
 ADD COLUMN max_quota_wait_seconds INTEGER NOT NULL DEFAULT 0 CHECK(max_quota_wait_seconds BETWEEN 0 AND 86400),
 ADD COLUMN auto_resume_interrupted BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN no_progress_requests INTEGER NOT NULL DEFAULT 1000 CHECK(no_progress_requests BETWEEN 0 AND 100000),
 ADD COLUMN no_progress_action TEXT NOT NULL DEFAULT 'warn' CHECK(no_progress_action IN ('warn','pause')),
 ADD COLUMN default_request_timeout_seconds INTEGER NOT NULL DEFAULT 30 CHECK(default_request_timeout_seconds BETWEEN 1 AND 900),
 ADD COLUMN default_preview_pages INTEGER NOT NULL DEFAULT 3 CHECK(default_preview_pages BETWEEN 1 AND 10000),
 ADD COLUMN default_max_pages INTEGER NOT NULL DEFAULT 0 CHECK(default_max_pages BETWEEN 0 AND 10000),
 ADD COLUMN default_max_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK(default_max_duration_seconds BETWEEN 0 AND 604800);
ALTER TABLE ingest.runs
 ADD COLUMN policy JSONB CHECK(policy IS NULL OR jsonb_typeof(policy)='object'),
 ADD COLUMN pause_reason TEXT NOT NULL DEFAULT '' CHECK(pause_reason IN ('','manual','budget','quota','no_progress','interrupted')),
 ADD COLUMN requests_without_new_ids BIGINT NOT NULL DEFAULT 0 CHECK(requests_without_new_ids >= 0),
 ADD COLUMN progress_warning_sent BOOLEAN NOT NULL DEFAULT FALSE;
-- Classify only an explicit durable manual hold. Legacy policy and snapshots stay untouched.
UPDATE ingest.runs SET pause_reason='manual' WHERE pause_requested;
CREATE TABLE ingest.run_progress_identities (
 run_id TEXT NOT NULL REFERENCES ingest.runs(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL,
 PRIMARY KEY(run_id,source_id)
);

CREATE OR REPLACE FUNCTION ingest.activity_kind(input TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN input IN ('queued','started','resumed','succeeded','failed','paused','pause_requested','cancelled','cancel_requested','page_saved','page_error','source_error','no_progress') THEN 'run.'||input ELSE 'run.activity' END;
$$;
CREATE OR REPLACE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'run.queued' THEN 'Collection queued'
 WHEN 'run.started' THEN 'Collection attempt started'
 WHEN 'run.resumed' THEN 'Collection queued for resume'
 WHEN 'run.succeeded' THEN 'Collection succeeded'
 WHEN 'run.failed' THEN 'Collection failed; review its retained records and source configuration'
 WHEN 'run.no_progress' THEN 'Collection has reached its useful-progress threshold without a new source identity; review the retained responses'
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
CREATE OR REPLACE FUNCTION ingest.activity_title(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'run.succeeded' THEN 'Collection completed'
 WHEN 'run.failed' THEN 'Collection failed'
 WHEN 'run.no_progress' THEN 'Collection is not finding new records'
 WHEN 'run.paused' THEN 'Collection paused'
 WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'schedule.failed' THEN 'Schedule needs attention'
 WHEN 'backup.succeeded' THEN 'Backup completed'
 WHEN 'backup.failed' THEN 'Backup failed'
 WHEN 'backup.restore_succeeded' THEN 'Recovery verified'
 WHEN 'backup.restore_failed' THEN 'Recovery verification failed'
 WHEN 'health.warning' THEN 'System needs attention'
 WHEN 'health.recovered' THEN 'System recovered'
 ELSE 'Activity' END;
$$;
CREATE OR REPLACE FUNCTION ingest.materialize_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO ingest.activity_logs(source_event_id,event_id,level,kind,message,provider_id,run_id,data,created_at)
 SELECT NEW.id,'event-'||NEW.id,CASE WHEN NEW.kind='page_saved' THEN 'debug' WHEN NEW.kind IN ('failed','page_error','source_error') THEN 'error' WHEN NEW.kind IN ('paused','cancelled','cancel_requested','no_progress') THEN 'warn' ELSE 'info' END,
 ingest.activity_kind(NEW.kind),ingest.activity_message(ingest.activity_kind(NEW.kind)),provider_id,NEW.run_id,ingest.safe_activity_data(NEW.data),NEW.created_at
 FROM ingest.runs WHERE id=NEW.run_id;
 RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION ingest.fanout_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE envelope BYTEA;
BEGIN
 IF NEW.kind IN ('run.no_progress','run.succeeded','run.failed','run.paused','run.cancelled','schedule.failed',
                 'backup.succeeded','backup.failed','backup.restore_succeeded','backup.restore_failed',
                 'health.warning','health.recovered') THEN
  INSERT INTO ingest.notifications(activity_id,level,kind,title,message,provider_id,run_id,created_at)
  VALUES(NEW.id,NEW.level,NEW.kind,ingest.activity_title(NEW.kind),NEW.message,NEW.provider_id,NEW.run_id,NEW.created_at);
  envelope := convert_to((jsonb_build_object('version',1,'id',NEW.event_id,'type',NEW.kind,'occurred_at',NEW.created_at,'message',NEW.message,'data',NEW.data)
   || CASE WHEN NEW.provider_id<>'' THEN jsonb_build_object('provider_id',NEW.provider_id) ELSE '{}'::jsonb END
   || CASE WHEN NEW.run_id<>'' THEN jsonb_build_object('run_id',NEW.run_id) ELSE '{}'::jsonb END)::text,'UTF8');
  INSERT INTO ingest.webhook_deliveries(id,webhook_id,event_id,event_type,body)
  SELECT id||':'||NEW.event_id,id,NEW.event_id,NEW.kind,envelope FROM ingest.webhooks
  WHERE enabled AND deleted_at IS NULL AND NEW.kind=ANY(events);
 END IF;
 PERFORM pg_notify('ingest_run_changes','');
 RETURN NEW;
END;
$$;
`
