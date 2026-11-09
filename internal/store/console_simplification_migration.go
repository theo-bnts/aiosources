package store

// Retire the separate audit/diagnostic products; keep security/share outcomes in
// the common activity history before removing their old table and API.
const consoleSimplificationSchema = `
CREATE FUNCTION aiosources.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
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
ALTER FUNCTION aiosources.activity_message(TEXT) RENAME TO core_activity_message;
CREATE FUNCTION aiosources.activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(aiosources.access_activity_message(kind),aiosources.core_activity_message(kind));
$$;
INSERT INTO aiosources.activity_logs(event_id,level,kind,message,created_at)
 SELECT 'security-history-'||id::text,'info',action,aiosources.activity_message(action),created_at
 FROM aiosources.security_audit;
DROP TABLE aiosources.security_audit;
DROP FUNCTION aiosources.guard_security_audit();
DROP TABLE aiosources.health_settings;
DROP TABLE aiosources.health_daily_samples;
DROP TABLE aiosources.health_source_observations;
DROP TABLE aiosources.health_alert_states;
DROP INDEX aiosources.runs_health_success;
DROP INDEX aiosources.events_health_failure;
DROP INDEX aiosources.pages_health_failure;
`
