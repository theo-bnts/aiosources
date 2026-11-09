package store

const stremioConnectionsSchema = `
ALTER TABLE aiosources.shares DROP CONSTRAINT shares_format_check;
ALTER TABLE aiosources.shares ADD CHECK(format IN ('catalog','torznab','stremio'));
ALTER TABLE aiosources.shares ADD COLUMN stream_preferences JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(stream_preferences)='object');
ALTER FUNCTION aiosources.access_activity_message(TEXT) RENAME TO access_activity_message_before_stremio_connections;
CREATE FUNCTION aiosources.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_stremio_connections(kind), CASE kind
 WHEN 'stremio.created' THEN 'Stremio access created'
 WHEN 'stremio.permissions_changed' THEN 'Stremio settings changed'
 WHEN 'stremio.rotated' THEN 'Stremio installation key replaced'
 WHEN 'stremio.revoked' THEN 'Stremio access revoked'
 ELSE NULL END);
$$;
`
