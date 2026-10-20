package store

// Keep every saved quota while moving its ownership to Network. Exposure and
// installation generations are independent of traffic admission settings.
const networkLimitsSchema = `
ALTER TABLE ingest.network_settings ADD COLUMN public_limits JSONB NOT NULL DEFAULT '{"setup_per_minute":12,"requests_per_minute":120,"concurrent":50}'
 CHECK(COALESCE(jsonb_typeof(public_limits)='object'
 AND public_limits ?& ARRAY['setup_per_minute','requests_per_minute','concurrent']
 AND (public_limits->>'setup_per_minute')::int BETWEEN 1 AND 100000
 AND (public_limits->>'requests_per_minute')::int BETWEEN 1 AND 100000
 AND (public_limits->>'concurrent')::int BETWEEN 1 AND 1000, FALSE));
UPDATE ingest.network_settings n SET public_limits=jsonb_build_object(
 'setup_per_minute',a.setup_per_minute,'requests_per_minute',a.requests_per_minute,'concurrent',a.concurrent), revision=n.revision+1
FROM ingest.aio_public_access a WHERE n.singleton AND a.singleton;
ALTER TABLE ingest.aio_public_access DROP COLUMN setup_per_minute, DROP COLUMN requests_per_minute, DROP COLUMN concurrent;
`
