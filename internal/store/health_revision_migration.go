package store

const healthRevisionSchema = `
ALTER TABLE aiosources.health_settings ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK(revision > 0);
`
