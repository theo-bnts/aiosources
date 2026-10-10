package store

// Retire the separate audit/diagnostic products; keep security/share outcomes in
// the common activity history before removing their old table and API.
const consoleSimplificationSchema = `
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'security.mfa_enabled' THEN 'Two-step verification enabled'
 WHEN 'security.mfa_disabled' THEN 'Two-step verification disabled'
 WHEN 'security.recovery_rotated' THEN 'Recovery codes replaced'
 WHEN 'security.session_revoked' THEN 'Administrator session revoked'
 WHEN 'security.sessions_revoked' THEN 'Other administrator sessions revoked'
 WHEN 'share.created' THEN 'Catalog share created'
 WHEN 'share.permissions_changed' THEN 'Catalog share permissions changed'
 WHEN 'share.rotated' THEN 'Catalog share credentials replaced'
 WHEN 'share.revoked' THEN 'Catalog share revoked'
 ELSE NULL END;
$$;
ALTER FUNCTION ingest.activity_message(TEXT) RENAME TO core_activity_message;
CREATE FUNCTION ingest.activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message(kind),ingest.core_activity_message(kind));
$$;
INSERT INTO ingest.activity_logs(event_id,level,kind,message,created_at)
 SELECT 'security-history-'||id::text,'info',action,ingest.activity_message(action),created_at
 FROM ingest.security_audit;
DROP TABLE ingest.security_audit;
DROP FUNCTION ingest.guard_security_audit();
DROP TABLE ingest.health_settings;
DROP TABLE ingest.health_daily_samples;
DROP TABLE ingest.health_source_observations;
DROP TABLE ingest.health_alert_states;
DROP INDEX ingest.runs_health_success;
DROP INDEX ingest.events_health_failure;
DROP INDEX ingest.pages_health_failure;
`
