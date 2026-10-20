package store

const incomingNetworkSchema = `
ALTER TABLE ingest.network_settings ADD COLUMN incoming JSONB NOT NULL DEFAULT '{"enabled":false,"ip_header":"X-Forwarded-For","format":"chain","trusted_proxies":[]}' CHECK(jsonb_typeof(incoming)='object');
ALTER TABLE ingest.aio_public_access
 ADD COLUMN setup_per_minute INTEGER NOT NULL DEFAULT 12 CHECK(setup_per_minute BETWEEN 1 AND 100000),
 ADD COLUMN requests_per_minute INTEGER NOT NULL DEFAULT 120 CHECK(requests_per_minute BETWEEN 1 AND 100000),
 ADD COLUMN concurrent INTEGER NOT NULL DEFAULT 50 CHECK(concurrent BETWEEN 1 AND 1000);
ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_limits;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_public_limits(kind), CASE kind
 WHEN 'security.public_limits_changed' THEN 'Public request limits changed'
 ELSE NULL END);
$$;
`
