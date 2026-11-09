package store

const aioPerformanceSchema = `
ALTER TABLE aiosources.aio_works ADD COLUMN adult BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX aio_metadata_jobs_due ON aiosources.aio_metadata_jobs(next_attempt,media_type,tmdb_id);
CREATE INDEX aio_works_refresh ON aiosources.aio_works(updated_at,media_type,tmdb_id);
CREATE INDEX aio_torrents_tmdb ON aiosources.torrents((fields->>'tmdb_id')) WHERE fields->>'tmdb_id' IS NOT NULL;
CREATE INDEX aio_torrents_imdb ON aiosources.torrents((regexp_replace(fields->>'imdb_id','^tt','','i'))) WHERE fields->>'imdb_id' IS NOT NULL;
CREATE INDEX aio_torrents_tvdb ON aiosources.torrents((fields->>'tvdb_id')) WHERE fields->>'tvdb_id' IS NOT NULL;

-- Reconsider old misses with the corrected parser and candidate discovery.
-- Working correspondences remain published; existing queue versions stay intact.
INSERT INTO aiosources.aio_match_queue(provider_id,source_id)
SELECT t.provider_id,t.source_id FROM aiosources.torrents t
LEFT JOIN aiosources.aio_matches m USING(provider_id,source_id)
WHERE m.status IS DISTINCT FROM 'matched' ON CONFLICT DO NOTHING;
`
