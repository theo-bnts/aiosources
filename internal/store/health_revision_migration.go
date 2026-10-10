package store

const healthRevisionSchema = `
ALTER TABLE ingest.health_settings ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK(revision > 0);
`
