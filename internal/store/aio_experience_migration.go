package store

const aioExperienceSchema = `
CREATE FUNCTION ingest.aio_review_evidence(fields JSONB) RETURNS JSONB
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT jsonb_strip_nulls(jsonb_build_object(
 'title',fields->'title','name',fields->'name',
 'info_hash',fields->'info_hash','hash',fields->'hash','infohash',fields->'infohash',
 'imdb_id',fields->'imdb_id','imdb',fields->'imdb','tt_id',fields->'tt_id',
 'tmdb_id',fields->'tmdb_id','tmdb',fields->'tmdb','tvdb_id',fields->'tvdb_id','tvdb',fields->'tvdb',
 'mal_id',fields->'mal_id','mal',fields->'mal','season',fields->'season','episode',fields->'episode',
 'external_ids',COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(fields->'external_ids')='object' THEN fields->'external_ids' ELSE '{}'::jsonb END) WHERE key IN ('imdb','imdb_id','tmdb','tmdb_id','tvdb','tvdb_id','mal','mal_id')),'{}'),
 'attributes',COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(CASE WHEN jsonb_typeof(fields->'attributes')='object' THEN fields->'attributes' ELSE '{}'::jsonb END) WHERE key IN ('imdb','imdbid','tmdb','tmdbid','tvdb','tvdbid','mal','malid','season','episode')),'{}')
 ));
$$;
CREATE TABLE ingest.aio_match_reviews (
 provider_id TEXT NOT NULL,source_id TEXT NOT NULL,
 active BOOLEAN NOT NULL DEFAULT TRUE,revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 fingerprint TEXT NOT NULL,evidence_fingerprint TEXT NOT NULL,media_type TEXT NOT NULL CHECK(media_type IN ('movie','tv')),
 tmdb_id TEXT NOT NULL,override_episode BOOLEAN NOT NULL DEFAULT FALSE,
 season INTEGER CHECK(season BETWEEN 0 AND 999),episode INTEGER CHECK(episode BETWEEN 1 AND 99999),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),PRIMARY KEY(provider_id,source_id),
 FOREIGN KEY(provider_id,source_id) REFERENCES ingest.torrents(provider_id,source_id) ON DELETE CASCADE,
 CHECK(episode IS NULL OR season IS NOT NULL),
 CHECK(media_type='tv' OR (season IS NULL AND episode IS NULL))
);
CREATE INDEX aio_match_reviews_work ON ingest.aio_match_reviews(media_type,tmdb_id) WHERE active;
CREATE TABLE ingest.stremio_requests (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 created_at TIMESTAMPTZ NOT NULL,media_type TEXT NOT NULL,resource TEXT NOT NULL,
 duration_ms BIGINT NOT NULL CHECK(duration_ms>=0),outcome TEXT NOT NULL CHECK(outcome IN ('results','empty','invalid','failed','cancelled','busy')),
 matched INTEGER NOT NULL CHECK(matched>=0),eligible INTEGER NOT NULL CHECK(eligible>=0),
 invalid_hash INTEGER NOT NULL CHECK(invalid_hash>=0),wrong_episode INTEGER NOT NULL CHECK(wrong_episode>=0),
 private_unavailable INTEGER NOT NULL CHECK(private_unavailable>=0),file_unavailable INTEGER NOT NULL CHECK(file_unavailable>=0),
 returned INTEGER NOT NULL CHECK(returned>=0),truncated BOOLEAN NOT NULL,
 results JSONB NOT NULL CHECK(jsonb_typeof(results)='array')
);
CREATE INDEX stremio_requests_recent ON ingest.stremio_requests(created_at DESC,id DESC);
`
