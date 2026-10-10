package store

// Reconsider prior misses once, plus previously accepted records whose title
// numbering was misread. Working decisions and active reviews stay published
// unless their own affected source is queued; existing queue versions survive.
const matchingRecheckSchema = `
WITH affected AS (
 SELECT t.provider_id,t.source_id FROM ingest.torrents t
 LEFT JOIN ingest.aio_matches m USING(provider_id,source_id)
 WHERE m.status IS DISTINCT FROM 'matched'
 UNION
 SELECT t.provider_id,t.source_id FROM ingest.torrents t
 JOIN ingest.aio_matches m USING(provider_id,source_id)
 WHERE m.status='matched' AND t.fields->>'title' ~ '[[:alpha:]][eE][0-9]'
 UNION
 SELECT t.provider_id,t.source_id FROM ingest.torrents t
 JOIN ingest.aio_matches m USING(provider_id,source_id)
 CROSS JOIN LATERAL regexp_matches(t.fields->>'title','(?:[sSeE]|[sS]eason|[sS]aison)[[:space:]]*([0-9]{1,4})[[:space:]]*[-–][[:space:]]*(?:[sSeE]|[sS]eason|[sS]aison)?[[:space:]]*([0-9]{1,4})','gi') AS r(numbers)
 WHERE m.status='matched' AND (r.numbers[2]::int<r.numbers[1]::int OR r.numbers[2]::int-r.numbers[1]::int>100)
)
INSERT INTO ingest.aio_match_queue(provider_id,source_id)
SELECT provider_id,source_id FROM affected a
WHERE NOT EXISTS(SELECT 1 FROM ingest.aio_match_reviews r WHERE r.provider_id=a.provider_id AND r.source_id=a.source_id AND r.active)
ON CONFLICT DO NOTHING;
`
