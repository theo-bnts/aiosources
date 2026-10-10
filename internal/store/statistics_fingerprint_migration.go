package store

// Fingerprint semantics are unchanged. Compute once when fields change, rather
// than repeatedly detoasting and hashing full metadata during UI statistics.
const statisticsFingerprintSchema = `
ALTER TABLE ingest.torrents ADD COLUMN fields_fingerprint TEXT GENERATED ALWAYS AS (md5(fields::text)) STORED;
`
