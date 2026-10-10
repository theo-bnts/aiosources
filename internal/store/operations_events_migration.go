package store

// operationsEventsSchema extends the existing sanitized activity pipeline. New
// producers share notifications and webhook delivery; retained events are not rewritten.
const operationsEventsSchema = `
CREATE OR REPLACE FUNCTION ingest.safe_activity_data(input JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE AS $$
 SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(input)
 WHERE (key IN ('page_id','page','pages','records','auxiliary_records','errors','committed_pages','max_pages','attempts','status_code','duration_ms','workers','bytes') AND jsonb_typeof(value)='number')
    OR (key IN ('done','emitted') AND jsonb_typeof(value)='boolean')
    OR (key='mode' AND value IN ('"preview"'::jsonb,'"incremental"'::jsonb,'"full"'::jsonb))
    OR (key='trigger' AND value IN ('"manual"'::jsonb,'"scheduled"'::jsonb))
    OR (key='status' AND value IN ('"queued"'::jsonb,'"running"'::jsonb,'"succeeded"'::jsonb,'"failed"'::jsonb,'"paused"'::jsonb,'"cancelled"'::jsonb))
    OR (key IN ('provider_id','run_id','backup_id') AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
    OR (key='failure_code' AND value IN ('"authentication"'::jsonb,'"certificate"'::jsonb,'"network"'::jsonb,'"timeout"'::jsonb,'"http"'::jsonb,'"parse"'::jsonb,'"stalled"'::jsonb,'"configuration"'::jsonb,'"unknown"'::jsonb))
    OR (key='health_code' AND value IN ('"storage_low"'::jsonb,'"source_stale"'::jsonb,'"authentication"'::jsonb,'"certificate"'::jsonb,'"run_stuck"'::jsonb,'"journal_growth"'::jsonb,'"database_unavailable"'::jsonb))
    OR (key='version' AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^(dev|v?[0-9]+[.][0-9]+[.][0-9]+([-+][A-Za-z0-9.-]+)?)$');
$$;
CREATE OR REPLACE FUNCTION ingest.activity_kind(input TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN input IN ('queued','started','resumed','succeeded','failed','paused','cancelled','cancel_requested','page_saved','page_error','source_error') THEN 'run.'||input ELSE 'run.activity' END;
$$;
CREATE OR REPLACE FUNCTION ingest.materialize_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO ingest.activity_logs(source_event_id,event_id,level,kind,message,provider_id,run_id,data,created_at)
 SELECT NEW.id,'event-'||NEW.id,CASE WHEN NEW.kind='page_saved' THEN 'debug' WHEN NEW.kind IN ('failed','page_error','source_error') THEN 'error' WHEN NEW.kind IN ('paused','cancelled','cancel_requested') THEN 'warn' ELSE 'info' END,
 ingest.activity_kind(NEW.kind),ingest.activity_message(ingest.activity_kind(NEW.kind)),provider_id,NEW.run_id,ingest.safe_activity_data(NEW.data),NEW.created_at
 FROM ingest.runs WHERE id=NEW.run_id;
 RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'run.queued' THEN 'Collection queued'
 WHEN 'run.started' THEN 'Collection attempt started'
 WHEN 'run.resumed' THEN 'Collection queued for resume'
 WHEN 'run.succeeded' THEN 'Collection succeeded'
 WHEN 'run.failed' THEN 'Collection failed; review its retained records and source configuration'
 WHEN 'run.paused' THEN 'Collection paused with a resumable checkpoint'
 WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'run.cancel_requested' THEN 'Collection cancellation requested'
 WHEN 'run.page_saved' THEN 'Response page retained'
 WHEN 'run.page_error' THEN 'Response page retained with an error'
 WHEN 'run.source_error' THEN 'Source request failed before a response could be retained; review source configuration and system health'
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
CREATE OR REPLACE FUNCTION ingest.fanout_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE envelope BYTEA;
BEGIN
 IF NEW.kind IN ('run.succeeded','run.failed','run.paused','run.cancelled','schedule.failed',
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
