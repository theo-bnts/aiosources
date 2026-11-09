package store

// runRecordsSchema is migration 14. Identity membership is derived from accepted
// source records, never content hashes. Original observations remain untouched.
const runRecordsSchema = `
ALTER TABLE aiosources.runs ADD COLUMN distinct_records BIGINT NOT NULL DEFAULT 0 CHECK (distinct_records >= 0);
CREATE TABLE aiosources.run_record_identities (
    run_id TEXT NOT NULL REFERENCES aiosources.runs(id),
    source_id TEXT NOT NULL CHECK (source_id <> ''),
    PRIMARY KEY (run_id,source_id)
);
WITH generations AS (
    SELECT run_id,MAX(id) AS first_page FROM aiosources.pages
    WHERE reset_staging AND error='' GROUP BY run_id
)
INSERT INTO aiosources.run_record_identities(run_id,source_id)
SELECT DISTINCT raw.run_id,raw.source_id
FROM aiosources.raw_records raw
JOIN aiosources.pages page ON page.id=raw.page_id
LEFT JOIN generations generation ON generation.run_id=raw.run_id
WHERE raw.error='' AND raw.source_id<>'' AND NOT raw.ignored AND NOT raw.auxiliary
  AND page.error='' AND raw.page_id>=COALESCE(generation.first_page,0);
UPDATE aiosources.runs run SET distinct_records=counts.total
FROM (SELECT run_id,count(*) AS total FROM aiosources.run_record_identities GROUP BY run_id) counts
WHERE run.id=counts.run_id;
`
