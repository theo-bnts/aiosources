package store

// Search URLs retain their filters; named saved views are no longer supported.
// Historical migrations stay unchanged so existing installations can upgrade.
const savedViewRetirementSchema = `DROP TABLE ingest.saved_views;`
