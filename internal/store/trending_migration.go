package store

// Cover the trending time-window scan without reading the torrents JSONB heap.
const trendingIndexSchema = `
CREATE INDEX torrents_trending_recent
 ON aiosources.torrents (COALESCE(search_published_at, first_seen_at) DESC, provider_id, source_id)
 INCLUDE (fields_fingerprint, search_published_at, first_seen_at);
`
