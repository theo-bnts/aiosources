package store

// Revisit only prior automatic misses that can benefit from complete compound
// aliases, standalone/versioned episodes or explicit video categories. Working
// matches and active manual reviews retain their current published decisions.
const compoundMatchingSchema = `
INSERT INTO ingest.aio_match_queue(provider_id,source_id)
SELECT t.provider_id,t.source_id FROM ingest.torrents t JOIN ingest.aio_matches m USING(provider_id,source_id)
WHERE m.status IN ('unmatched','ambiguous') AND m.book_kind=''
 AND (translate(t.fields->>'title','._','  ') ~* '(\(| - |\m(E|Ep|Episode)[[:space:]]*[0-9]| - [0-9]{1,4}v[0-9])'
  OR t.fields->>'type' IN ('movie','film','tv','series','tvshow')
  OR t.fields->>'media_type' IN ('movie','film','tv','series','tvshow')
  OR (t.fields->'categories')::text ~ '\m[25][0-9]{3}\M'
  OR t.fields ?| ARRAY['tmdb_id','imdb_id','tvdb_id','tmdb','imdb','tvdb','external_ids','attributes'])
 AND NOT EXISTS(SELECT 1 FROM ingest.aio_match_reviews r WHERE r.provider_id=t.provider_id AND r.source_id=t.source_id AND r.active)
ON CONFLICT DO NOTHING;
`
