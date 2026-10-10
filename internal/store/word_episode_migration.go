package store

// Leave migration 44 immutable for instances that already applied it. This
// additional one-time pass covers explicit word-based episode numbering.
const wordEpisodeRecheckSchema = `
INSERT INTO ingest.aio_match_queue(provider_id,source_id)
SELECT t.provider_id,t.source_id
FROM ingest.torrents t JOIN ingest.aio_matches m USING(provider_id,source_id)
WHERE m.status='matched' AND t.fields->>'title' ~* '(?:episode|ep)[[:space:]]*[0-9]'
AND NOT EXISTS(SELECT 1 FROM ingest.aio_match_reviews r WHERE r.provider_id=t.provider_id AND r.source_id=t.source_id AND r.active)
ON CONFLICT DO NOTHING;
`
