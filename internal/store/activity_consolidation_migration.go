package store

// Every notification references an existing log. Preserve its short title and
// service context there before retiring the duplicate inbox and outbound queue.
// Historical migrations remain immutable so older backups can still upgrade.
const activityConsolidationSchema = `
ALTER TABLE ingest.activity_logs ADD COLUMN title TEXT NOT NULL DEFAULT '';
UPDATE ingest.activity_logs l SET title=n.title,service=CASE WHEN l.service='' THEN n.service ELSE l.service END
FROM ingest.notifications n WHERE n.activity_id=l.id;

INSERT INTO ingest.activity_logs(event_id,level,kind,title,message,provider_id,run_id,created_at,data)
SELECT 'retired-delivery-'||d.id,
 CASE WHEN d.status='failed' THEN 'warn' ELSE 'info' END,
 'external_notification.'||CASE WHEN d.status IN ('delivered','failed') THEN d.status ELSE 'cancelled' END,
 'External notification history',
 CASE d.status
  WHEN 'delivered' THEN 'External notification was delivered before this feature was removed.'
  WHEN 'failed' THEN 'External notification delivery failed before this feature was removed.'
  WHEN 'cancelled' THEN 'External notification delivery was cancelled.'
  ELSE 'Pending external notification was cancelled when this feature was removed.' END,
 COALESCE(l.provider_id,''),COALESCE(l.run_id,''),COALESCE(d.delivered_at,d.created_at),
 jsonb_build_object('status',d.status,'attempts',d.attempts,'status_code',d.last_status,'event_type',d.event_type)
FROM ingest.webhook_deliveries d LEFT JOIN ingest.activity_logs l ON l.event_id=d.event_id
ORDER BY d.created_at,d.id;

DROP TRIGGER activity_fanout ON ingest.activity_logs;
DROP TRIGGER webhook_secret_delete ON ingest.secrets;
DROP FUNCTION ingest.fanout_activity();
DROP FUNCTION ingest.activity_title(TEXT);
DROP FUNCTION ingest.protect_webhook_secret();
DROP TABLE ingest.webhook_deliveries;
DROP TABLE ingest.webhooks;
DROP FUNCTION ingest.check_webhook_secrets();
DROP TABLE ingest.notifications;

CREATE FUNCTION ingest.notify_activity() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('ingest_run_changes','');
 RETURN NEW;
END;
$$;
CREATE TRIGGER activity_changed AFTER INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.notify_activity();
`
