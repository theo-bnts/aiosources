package store

// Classify both new definitions and immutable snapshots without rewriting the
// saved source JSON. Existing catalog history keeps its indexed classification.
const jsonAdapterSchema = `
ALTER TABLE ingest.runs ALTER COLUMN remote_catalog SET EXPRESSION AS (
 config @> '{"adapter":"json","http":{"catalog":true}}'::jsonb
 OR config @> '{"adapter":"http_json","http":{"catalog":true}}'::jsonb
);
ANALYZE ingest.runs(remote_catalog);
`
