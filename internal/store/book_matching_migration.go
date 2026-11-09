package store

// Save the parser's classification with its decision. Search and Torznab must
// not independently guess whether the same identified release is an audiobook.
// Existing decisions are rechecked once, without replacing manual choices or
// resetting already queued work and its version.
const bookMatchingSchema = `
ALTER TABLE aiosources.aio_matches ADD COLUMN book_kind TEXT NOT NULL DEFAULT '' CHECK(book_kind IN ('','book','ebook','audiobook'));
-- Keep already published books in their existing type filters until the
-- shared parser has rechecked them through the durable queue below.
UPDATE aiosources.aio_matches m SET book_kind=CASE
 WHEN COALESCE(t.fields->>'media_type',t.fields->>'type','')='audiobook'
 OR COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m3030\M'
 OR COALESCE(t.fields->>'category_name','') ~* '\m(audiobooks?|livres?[ ._-]?audio)\M'
 OR t.fields->>'title' ~* '\m(audiobook|audio[ ._-]?book|livre[ ._-]?audio|m4b|aax)\M'
 THEN 'audiobook' ELSE 'ebook' END
FROM aiosources.torrents t,aiosources.aio_works w
WHERE m.provider_id=t.provider_id AND m.source_id=t.source_id
AND m.work_id=w.id AND w.media_type='book' AND m.status='matched';
INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
SELECT m.provider_id,m.source_id FROM aiosources.aio_matches m
LEFT JOIN aiosources.aio_works w ON w.id=m.work_id
WHERE w.media_type='book' OR m.reason IN (
 'book information not ready','insufficient bibliographic evidence',
 'book collection requires review','multiple or incomplete book candidates',
 'book title corroborated by bibliographic evidence','verified ISBN',
 'verified Open Library identifier','invalid Open Library identifier',
 'conflicting or invalid ISBN evidence')
ON CONFLICT(provider_id,source_id) DO NOTHING;
INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
SELECT provider_id,source_id FROM aiosources.torrents
WHERE aiosources.torrent_search_text(provider_id,source_id,fields) ~* '\me[ ._-]?books?\M'
AND fields->>'title' ~* '\me[ ._-]?books?\M'
ON CONFLICT(provider_id,source_id) DO NOTHING;
`
