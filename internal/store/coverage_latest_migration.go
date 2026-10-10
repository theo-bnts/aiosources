package store

// Rebuild only derived coverage. The last accepted, valid observation owns an
// identity's memberships; neither rejected responses nor equal hashes do so.
const coverageLatestSchema = `
CREATE TABLE ingest.run_scope_refresh_candidates (
    run_id TEXT NOT NULL REFERENCES ingest.runs(id),
    source_id TEXT NOT NULL,
    PRIMARY KEY (run_id, source_id)
);
CREATE TEMP TABLE coverage_latest_backfill ON COMMIT DROP AS
WITH generations AS (
    SELECT run_id,MAX(id) AS first_page FROM ingest.pages WHERE reset_staging AND error='' GROUP BY run_id
), accepted AS (
    SELECT raw.id,raw.run_id,raw.source_id,raw.fields,raw.ignored,runs.config->'traversal'->'scopes' AS scopes
    FROM ingest.raw_records raw
    JOIN ingest.pages page ON page.id=raw.page_id
    JOIN ingest.runs runs ON runs.id=raw.run_id
    LEFT JOIN generations generation ON generation.run_id=raw.run_id
    WHERE jsonb_typeof(runs.config->'traversal'->'scopes')='array'
      AND raw.source_id<>'' AND raw.error='' AND NOT raw.auxiliary AND page.error=''
      AND page.id>=COALESCE(generation.first_page,0)
      AND (runs.mode<>'full' OR runs.status='succeeded' OR EXISTS (
          SELECT 1 FROM ingest.staged_torrents staged WHERE staged.run_id=raw.run_id AND staged.source_id=raw.source_id AND raw.id>=staged.raw_id
      ))
), interpreted AS (
    SELECT accepted.*,ARRAY(
        SELECT scope->>'id' FROM jsonb_array_elements(accepted.scopes) scope
        WHERE jsonb_typeof(scope->'match')='object' AND scope->'match'<>'{}'::jsonb
        AND NOT EXISTS (
            SELECT 1 FROM jsonb_each(scope->'match') expected
            WHERE jsonb_typeof(accepted.fields->expected.key) NOT IN ('string','number')
               OR NULLIF(btrim(accepted.fields->>expected.key),'') IS DISTINCT FROM btrim(expected.value #>> '{}')
        )
    ) AS matches
    FROM accepted
)
SELECT DISTINCT ON (run_id,source_id) run_id,source_id,matches
FROM interpreted
WHERE NOT ignored OR cardinality(matches)=0
ORDER BY run_id,source_id,id DESC;
DELETE FROM ingest.run_scope_memberships m USING ingest.runs r
WHERE m.run_id=r.id AND jsonb_typeof(r.config->'traversal'->'scopes')='array';
DELETE FROM ingest.run_scope_counts c USING ingest.runs r
WHERE c.run_id=r.id AND jsonb_typeof(r.config->'traversal'->'scopes')='array';
INSERT INTO ingest.run_scope_memberships(run_id,scope_id,source_id)
SELECT latest.run_id,scope_id,latest.source_id
FROM coverage_latest_backfill latest CROSS JOIN LATERAL unnest(latest.matches) scope_id;
INSERT INTO ingest.run_scope_counts(run_id,scope_id,observed_count)
SELECT m.run_id,m.scope_id,count(*) FROM ingest.run_scope_memberships m JOIN ingest.runs r ON r.id=m.run_id
WHERE jsonb_typeof(r.config->'traversal'->'scopes')='array'
GROUP BY m.run_id,m.scope_id;
DELETE FROM ingest.staged_torrents staged USING coverage_latest_backfill latest
WHERE staged.run_id=latest.run_id AND staged.source_id=latest.source_id AND cardinality(latest.matches)=0;
DELETE FROM ingest.run_record_identities ids USING coverage_latest_backfill latest
WHERE ids.run_id=latest.run_id AND ids.source_id=latest.source_id AND cardinality(latest.matches)=0;
UPDATE ingest.runs runs SET distinct_records=(SELECT count(*) FROM ingest.run_record_identities ids WHERE ids.run_id=runs.id)
WHERE runs.id IN (SELECT run_id FROM coverage_latest_backfill WHERE cardinality(matches)=0);
`

const filterCoverageSchema = `
CREATE INDEX pages_filter_coverage ON ingest.pages(run_id,(metadata->>'fingerprint_scope'))
WHERE error='' AND metadata->>'traversal_phase'='options_list';
CREATE INDEX raw_records_filter_coverage ON ingest.raw_records(page_id,source_id)
WHERE error='' AND NOT ignored AND NOT auxiliary;
`
