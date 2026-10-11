package store

// Preserve the common access messages and add public exposure changes without
// rewriting the immutable migrations or retaining any visitor information.
const publicAccessActivitySchema = `
CREATE OR REPLACE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE kind
 WHEN 'security.mfa_enabled' THEN 'Two-step verification enabled'
 WHEN 'security.mfa_disabled' THEN 'Two-step verification disabled'
 WHEN 'security.recovery_rotated' THEN 'Recovery codes replaced'
 WHEN 'security.session_revoked' THEN 'Administrator session revoked'
 WHEN 'security.sessions_revoked' THEN 'Other administrator sessions revoked'
 WHEN 'security.public_access_enabled' THEN 'Public access enabled'
 WHEN 'security.public_access_disabled' THEN 'Public access disabled'
 WHEN 'share.created' THEN 'Catalog share created'
 WHEN 'share.permissions_changed' THEN 'Catalog share permissions changed'
 WHEN 'share.rotated' THEN 'Catalog share credentials replaced'
 WHEN 'share.revoked' THEN 'Catalog share revoked'
 ELSE NULL END;
$$;
`
