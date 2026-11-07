package store

// Keep narrow discovery probes away from the full metadata/source JSON. The
// chronological index also lets LIMIT precede expensive Torznab projections.
// Expression statistics prevent the valid-record predicates being estimated as
// a handful of rows when almost the whole publication satisfies them.
const catalogReadSchema = `
CREATE INDEX aio_works_book_entries ON ingest.aio_works(id) WHERE media_type='book';
CREATE INDEX torrents_torznab_recent ON ingest.torrents((COALESCE(search_published_at,first_seen_at)) DESC,provider_id,source_id)
INCLUDE(fields_fingerprint)
WHERE length(search_info_hash)=40 AND jsonb_typeof(fields->'title')='string' AND fields->>'title'<>''
AND (search_size IS NULL OR search_size BETWEEN 0 AND 9223372036854775807);
CREATE STATISTICS ingest.torrents_torznab_fields ON (length(search_info_hash)),(jsonb_typeof(fields->'title')) FROM ingest.torrents;
ANALYZE ingest.torrents;
`
