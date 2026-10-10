package store

const operationsSchema = `
CREATE TABLE ingest.operation_runs (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('metadata','matching','cache','cleanup')),
 service TEXT NOT NULL CHECK(service IN ('tmdb','local','alldebrid','torbox')),
 trigger TEXT NOT NULL CHECK(trigger IN ('manual','automatic','playback')),
 resource TEXT NOT NULL DEFAULT '',
 lock_key BIGINT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled','interrupted')),
 phase TEXT NOT NULL DEFAULT 'configuration',
 processed BIGINT NOT NULL DEFAULT 0 CHECK(processed>=0),
 errors BIGINT NOT NULL DEFAULT 0 CHECK(errors>=0),
 results JSONB NOT NULL DEFAULT '{}',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 finished_at TIMESTAMPTZ,
 error TEXT NOT NULL DEFAULT '',
 CHECK ((status='running') = (finished_at IS NULL)),
 CHECK ((kind='metadata' AND service='tmdb') OR (kind='matching' AND service='local') OR (kind='cache' AND service IN ('alldebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'))
);
CREATE INDEX operation_runs_history ON ingest.operation_runs(kind,created_at DESC,id DESC);
CREATE INDEX operation_runs_active ON ingest.operation_runs(lock_key) WHERE status='running';
CREATE TABLE ingest.operation_events (
 id BIGSERIAL PRIMARY KEY,
 run_id TEXT NOT NULL REFERENCES ingest.operation_runs(id),
 phase TEXT NOT NULL,
 processed BIGINT NOT NULL,
 errors BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX operation_events_run ON ingest.operation_events(run_id,id);
`
