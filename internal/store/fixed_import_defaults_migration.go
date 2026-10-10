package store

// Existing runs already hold their own policy, timeout and page budget. Dropping
// the mutable fallbacks cannot alter their checkpoints or execution snapshots.
const fixedImportDefaultsSchema = `
ALTER TABLE ingest.collection_settings
 DROP COLUMN auto_resume_interrupted,
 DROP COLUMN default_request_timeout_seconds,
 DROP COLUMN default_preview_pages,
 DROP COLUMN default_max_pages,
 DROP COLUMN default_max_duration_seconds;
UPDATE ingest.collection_settings SET revision=revision+1;
`
