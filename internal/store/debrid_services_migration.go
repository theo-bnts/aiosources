package store

// Widen existing constraints without rebuilding tables or changing credentials,
// observations, connection settings or in-progress downloads.
const debridServicesSchema = `
ALTER TABLE ingest.aio_shared_availability DROP CONSTRAINT aio_shared_availability_service_check;
ALTER TABLE ingest.aio_shared_availability ADD CONSTRAINT aio_shared_availability_service_check
 CHECK(service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) NOT VALID;
ALTER TABLE ingest.stremio_requests DROP CONSTRAINT stremio_requests_service_check;
ALTER TABLE ingest.stremio_requests ADD CONSTRAINT stremio_requests_service_check
 CHECK(service IN ('','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) NOT VALID;
ALTER TABLE ingest.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE ingest.activity_logs ADD CONSTRAINT activity_logs_service_check
 CHECK(service IN ('','tmdb','network','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) NOT VALID;
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_service_check;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_service_check
 CHECK(service IN ('tmdb','local','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) NOT VALID;
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_kind_service_check;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_kind_service_check
 CHECK ((kind='metadata' AND service='tmdb') OR (kind='matching' AND service='local')
 OR (kind IN ('cache','download') AND service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'))
 OR (kind='cleanup' AND service='alldebrid')) NOT VALID;
CREATE TABLE ingest.aio_preparations(
 service TEXT NOT NULL CHECK(service IN ('debridlink','deepbrid','premiumize','realdebrid')),
 account TEXT NOT NULL, hash TEXT NOT NULL, provider_id TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(service,account,hash)
);
CREATE INDEX aio_preparations_expiry ON ingest.aio_preparations(expires_at);
`
