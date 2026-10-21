package store

// Preserve existing public installations until Stremio is explicitly disabled.
// Its revocation counter is independent of Search and browser credentials.
const publicStremioSchema = `
ALTER TABLE ingest.aio_public_access ADD COLUMN stremio_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE ingest.aio_public_access ADD COLUMN stremio_generation BIGINT NOT NULL DEFAULT 1;
ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_stremio;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_public_stremio(kind), CASE kind
 WHEN 'security.public_stremio_enabled' THEN 'Public Stremio enabled'
 WHEN 'security.public_stremio_disabled' THEN 'Public Stremio disabled'
 ELSE NULL END);
$$;
`
