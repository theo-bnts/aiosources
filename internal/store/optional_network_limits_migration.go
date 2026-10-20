package store

// Zero removes just that restriction. Saved positive quotas remain unchanged.
const optionalNetworkLimitsSchema = `
ALTER TABLE ingest.network_settings DROP CONSTRAINT network_settings_public_limits_check;
ALTER TABLE ingest.network_settings ADD CONSTRAINT network_settings_public_limits_check
 CHECK(COALESCE(jsonb_typeof(public_limits)='object'
 AND public_limits ?& ARRAY['setup_per_minute','requests_per_minute','concurrent']
 AND (public_limits->>'setup_per_minute')::int BETWEEN 0 AND 100000
 AND (public_limits->>'requests_per_minute')::int BETWEEN 0 AND 100000
 AND (public_limits->>'concurrent')::int BETWEEN 0 AND 1000, FALSE));
`
