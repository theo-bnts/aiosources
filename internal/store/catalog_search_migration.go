package store

// Discovery reads the distinct published work IDs without loading every match.
// Retain the complete work index for foreign-key checks during information reset.
const catalogSearchIndexSchema = `
CREATE INDEX aio_matches_published_work ON ingest.aio_matches(work_id)
WHERE status='matched' AND work_id IS NOT NULL;
`
