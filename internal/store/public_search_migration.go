package store

const publicSearchSchema = `
ALTER TABLE ingest.aio_public_access ADD COLUMN search_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_trigger_check;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_trigger_check CHECK(trigger IN ('manual','automatic','playback','public'));
ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_search;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_public_search(kind), CASE kind
 WHEN 'security.public_search_enabled' THEN 'Public Search enabled'
 WHEN 'security.public_search_disabled' THEN 'Public Search disabled'
 ELSE NULL END);
$$;
`
