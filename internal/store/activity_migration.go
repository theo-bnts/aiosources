package store

// activitySchema is migration 4. Original event rows remain unchanged. Backfill
// runs before fan-out triggers are installed, so historical events never send.
const activitySchema = `
CREATE TABLE ingest.activity_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_event_id BIGINT UNIQUE REFERENCES ingest.events(id),
    event_id TEXT NOT NULL UNIQUE,
    level TEXT NOT NULL CHECK(level IN ('debug','info','warn','error')),
    kind TEXT NOT NULL,
    message TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX activity_recent ON ingest.activity_logs(created_at DESC,id DESC);
CREATE INDEX activity_provider_recent ON ingest.activity_logs(provider_id,created_at DESC,id DESC);
CREATE INDEX activity_run_recent ON ingest.activity_logs(run_id,created_at DESC,id DESC);
CREATE INDEX activity_level_recent ON ingest.activity_logs(level,created_at DESC,id DESC);
CREATE TABLE ingest.notifications (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    activity_id BIGINT NOT NULL UNIQUE REFERENCES ingest.activity_logs(id),
    level TEXT NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    read_at TIMESTAMPTZ
);
CREATE INDEX notifications_recent ON ingest.notifications(created_at DESC,id DESC);
CREATE INDEX notifications_unread ON ingest.notifications(created_at DESC,id DESC) WHERE read_at IS NULL;
CREATE TABLE ingest.webhooks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    url_secret_ref TEXT NOT NULL,
    signing_secret_ref TEXT NOT NULL DEFAULT '',
    events TEXT[] NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE TABLE ingest.webhook_deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES ingest.webhooks(id),
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    body BYTEA NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','delivering','delivered','failed','cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ DEFAULT NOW(),
    lease_until TIMESTAMPTZ,
    lease_token TEXT,
    last_status INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    UNIQUE(webhook_id,event_id)
);
CREATE INDEX webhook_delivery_due ON ingest.webhook_deliveries(next_attempt_at) WHERE status='pending';
CREATE INDEX webhook_delivery_leases ON ingest.webhook_deliveries(lease_until) WHERE status='delivering';
CREATE INDEX webhook_delivery_history ON ingest.webhook_deliveries(webhook_id,created_at DESC,id DESC);

CREATE FUNCTION ingest.safe_activity_data(input JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE AS $$
 SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(input)
 WHERE (key IN ('page_id','page','pages','records','auxiliary_records','errors','committed_pages','max_pages','attempts','status_code','duration_ms','workers') AND jsonb_typeof(value)='number')
    OR (key IN ('done','emitted') AND jsonb_typeof(value)='boolean')
    OR (key='mode' AND value IN ('"preview"'::jsonb,'"incremental"'::jsonb,'"full"'::jsonb))
    OR (key='trigger' AND value IN ('"manual"'::jsonb,'"scheduled"'::jsonb))
    OR (key='status' AND value IN ('"queued"'::jsonb,'"running"'::jsonb,'"succeeded"'::jsonb,'"failed"'::jsonb,'"paused"'::jsonb,'"cancelled"'::jsonb))
    OR (key IN ('provider_id','run_id') AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$')
    OR (key='version' AND jsonb_typeof(value)='string' AND value #>> '{}' ~ '^(dev|v?[0-9]+[.][0-9]+[.][0-9]+([-+][A-Za-z0-9.-]+)?)$');
$$;
CREATE FUNCTION ingest.activity_kind(input TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN input IN ('queued','started','resumed','succeeded','failed','paused','cancelled','cancel_requested','page_saved','page_error') THEN 'run.'||input ELSE 'run.activity' END;
$$;
CREATE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
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
 WHEN 'schedule.failed' THEN 'Scheduled collection could not be queued; review source configuration and service status'
 WHEN 'schedule.queued' THEN 'Scheduled collection queued'
 WHEN 'schedule.skipped' THEN 'Scheduled collection skipped because a collection is already active'
 WHEN 'service.started' THEN 'Administration service started'
 WHEN 'service.stopped' THEN 'Administration service stopped'
 WHEN 'service.failed' THEN 'Administration service failed'
 WHEN 'scheduler.started' THEN 'Collection scheduler started'
 WHEN 'scheduler.stopped' THEN 'Collection scheduler stopped'
 WHEN 'scheduler.failed' THEN 'Collection scheduler failed'
 ELSE 'Collection activity recorded' END;
$$;
CREATE FUNCTION ingest.activity_title(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE kind WHEN 'run.succeeded' THEN 'Collection completed' WHEN 'run.failed' THEN 'Collection failed'
 WHEN 'run.paused' THEN 'Collection paused' WHEN 'run.cancelled' THEN 'Collection cancelled'
 WHEN 'schedule.failed' THEN 'Schedule needs attention' ELSE 'Activity' END;
$$;
INSERT INTO ingest.activity_logs(source_event_id,event_id,level,kind,message,provider_id,run_id,data,created_at)
SELECT e.id,'event-'||e.id,CASE WHEN e.kind='page_saved' THEN 'debug' WHEN e.kind IN ('failed','page_error') THEN 'error' WHEN e.kind IN ('paused','cancelled','cancel_requested') THEN 'warn' ELSE 'info' END,
 ingest.activity_kind(e.kind),ingest.activity_message(ingest.activity_kind(e.kind)),r.provider_id,e.run_id,ingest.safe_activity_data(e.data),e.created_at
FROM ingest.events e JOIN ingest.runs r ON r.id=e.run_id ORDER BY e.id;
INSERT INTO ingest.notifications(activity_id,level,kind,title,message,provider_id,run_id,created_at)
SELECT id,level,kind,ingest.activity_title(kind),message,provider_id,run_id,created_at FROM ingest.activity_logs
WHERE kind IN ('run.succeeded','run.failed','run.paused','run.cancelled','schedule.failed') ORDER BY id;

CREATE FUNCTION ingest.materialize_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO ingest.activity_logs(source_event_id,event_id,level,kind,message,provider_id,run_id,data,created_at)
 SELECT NEW.id,'event-'||NEW.id,CASE WHEN NEW.kind='page_saved' THEN 'debug' WHEN NEW.kind IN ('failed','page_error') THEN 'error' WHEN NEW.kind IN ('paused','cancelled','cancel_requested') THEN 'warn' ELSE 'info' END,
 ingest.activity_kind(NEW.kind),ingest.activity_message(ingest.activity_kind(NEW.kind)),provider_id,NEW.run_id,ingest.safe_activity_data(NEW.data),NEW.created_at
 FROM ingest.runs WHERE id=NEW.run_id;
 RETURN NEW;
END;
$$;
CREATE FUNCTION ingest.fanout_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE envelope BYTEA;
BEGIN
 IF NEW.kind IN ('run.succeeded','run.failed','run.paused','run.cancelled','schedule.failed') THEN
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
CREATE TRIGGER activity_event_insert AFTER INSERT ON ingest.events FOR EACH ROW EXECUTE FUNCTION ingest.materialize_activity();
CREATE TRIGGER activity_fanout AFTER INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.fanout_activity();

-- Matching row locks serialize deletion against reference creation. Unlike a
-- foreign key, soft-deleted hooks do not retain a live secret dependency.
CREATE FUNCTION ingest.check_webhook_secrets() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.deleted_at IS NULL THEN
  PERFORM name FROM ingest.secrets WHERE name=NEW.url_secret_ref FOR KEY SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'Webhook destination secret is unavailable' USING ERRCODE='23503'; END IF;
  IF NEW.signing_secret_ref<>'' THEN
   PERFORM name FROM ingest.secrets WHERE name=NEW.signing_secret_ref FOR KEY SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'Webhook signing secret is unavailable' USING ERRCODE='23503'; END IF;
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER webhook_secret_check BEFORE INSERT OR UPDATE ON ingest.webhooks FOR EACH ROW EXECUTE FUNCTION ingest.check_webhook_secrets();
CREATE FUNCTION ingest.protect_webhook_secret() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM ingest.webhooks WHERE deleted_at IS NULL AND (url_secret_ref=OLD.name OR signing_secret_ref=OLD.name)) THEN
  RAISE EXCEPTION 'Secret is referenced by a webhook' USING ERRCODE='23505';
 END IF;
 RETURN OLD;
END;
$$;
CREATE TRIGGER webhook_secret_delete BEFORE DELETE ON ingest.secrets FOR EACH ROW EXECUTE FUNCTION ingest.protect_webhook_secret();
`
