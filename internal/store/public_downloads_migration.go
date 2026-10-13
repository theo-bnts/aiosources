package store

const publicDownloadsSchema = `
ALTER TABLE ingest.aio_public_access ADD COLUMN allow_private_downloads BOOLEAN NOT NULL DEFAULT FALSE;
ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_downloads;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_public_downloads(kind), CASE kind
 WHEN 'security.public_source_downloads_enabled' THEN 'Visitor announce URLs enabled'
 WHEN 'security.public_source_downloads_disabled' THEN 'Visitor announce URLs disabled'
 ELSE NULL END);
$$;
`
