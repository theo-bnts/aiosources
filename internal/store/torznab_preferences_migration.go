package store

const torznabPreferencesSchema = `
ALTER TABLE ingest.shares ADD COLUMN torznab_preferences JSONB NOT NULL DEFAULT '{}'
 CHECK(jsonb_typeof(torznab_preferences)='object');
`
