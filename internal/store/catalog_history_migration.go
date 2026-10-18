package store

// Keep pagination and counts independent of the size of saved source JSON.
// The durable snapshot, rather than the current registry, owns a run's history.
const catalogHistorySchema = `
ALTER TABLE ingest.runs ADD COLUMN remote_catalog BOOLEAN
 GENERATED ALWAYS AS (config @> '{"adapter":"http_json","http":{"catalog":true}}'::jsonb) STORED;
CREATE INDEX runs_catalog_history ON ingest.runs(remote_catalog,created_at DESC,id DESC);
`
