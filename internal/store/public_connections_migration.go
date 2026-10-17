package store

const publicConnectionsSchema = `
ALTER TABLE ingest.aio_public_access RENAME COLUMN allow_private_downloads TO expose_sources;
ALTER TABLE ingest.aio_public_access
 ADD COLUMN torznab_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN torznab_generation BIGINT NOT NULL DEFAULT 1;
ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_connections;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_public_connections(kind), CASE kind
 WHEN 'torznab.public_searched' THEN 'Public Torznab catalog search completed'
 WHEN 'torznab.public_failed' THEN 'Public Torznab catalog request failed'
 WHEN 'security.public_torznab_enabled' THEN 'Public Torznab enabled'
 WHEN 'security.public_torznab_disabled' THEN 'Public Torznab disabled'
 ELSE NULL END);
$$;
`
