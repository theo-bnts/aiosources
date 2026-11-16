package store

// Keep existing sessions and deliberate custom values. Only former defaults
// change automatically; older session caps are bounded by the new supported range.
const accessLimitsSchema = `
ALTER TABLE aiosources.runtime_settings DROP CONSTRAINT runtime_settings_section_check;
ALTER TABLE aiosources.runtime_settings ADD CONSTRAINT runtime_settings_section_check
 CHECK(section IN ('debrid','outgoing','connections','history','identification','matching','metadata','metadata_retries','source_retries','debrid_retries','downloads','security','trusted_security','payloads'));
UPDATE aiosources.runtime_settings SET values=values || jsonb_build_object(
 'session_hours',CASE WHEN (values->>'session_hours')::int=12 THEN 24 ELSE (values->>'session_hours')::int END,
 'session_limit',CASE WHEN (values->>'session_limit')::int=256 THEN 10 ELSE LEAST((values->>'session_limit')::int,100) END,
 'login_global_per_minute',CASE WHEN (values->>'login_global_per_minute')::int=60 THEN GREATEST(50,(values->>'login_per_minute')::int) ELSE (values->>'login_global_per_minute')::int END
),revision=revision+1 WHERE section='security';
INSERT INTO aiosources.runtime_settings(section,values)
 SELECT 'trusted_security',values || jsonb_build_object('session_limit',10,'login_account_per_minute',values->'login_per_minute')
 FROM aiosources.runtime_settings WHERE section='security';
`
