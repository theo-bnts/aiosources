package store

const aiosourceSchema = `
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
CREATE TABLE aiosources.aio_works (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 media_type TEXT NOT NULL CHECK(media_type IN ('movie','tv')),
 tmdb_id TEXT NOT NULL, imdb_id TEXT NOT NULL DEFAULT '', tvdb_id TEXT NOT NULL DEFAULT '',
 years INTEGER[] NOT NULL, revision BIGINT NOT NULL DEFAULT 1, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(media_type,tmdb_id)
);
CREATE INDEX aio_works_imdb ON aiosources.aio_works(imdb_id) WHERE imdb_id<>'';
CREATE INDEX aio_works_tvdb ON aiosources.aio_works(tvdb_id) WHERE tvdb_id<>'';
CREATE INDEX aio_works_tmdb ON aiosources.aio_works(tmdb_id);
CREATE TABLE aiosources.aio_titles(work_id BIGINT NOT NULL REFERENCES aiosources.aio_works(id) ON DELETE CASCADE,title TEXT NOT NULL,normalized TEXT NOT NULL,PRIMARY KEY(work_id,title));
CREATE INDEX aio_titles_exact ON aiosources.aio_titles(normalized);
CREATE INDEX aio_titles_fuzzy ON aiosources.aio_titles USING gin(normalized public.gin_trgm_ops);
CREATE TABLE aiosources.aio_seasons(work_id BIGINT NOT NULL REFERENCES aiosources.aio_works(id) ON DELETE CASCADE,season INTEGER NOT NULL,year INTEGER NOT NULL,PRIMARY KEY(work_id,season));
CREATE TABLE aiosources.aio_episodes(work_id BIGINT NOT NULL REFERENCES aiosources.aio_works(id) ON DELETE CASCADE,season INTEGER NOT NULL,episode INTEGER NOT NULL,year INTEGER NOT NULL,PRIMARY KEY(work_id,season,episode));
CREATE TABLE aiosources.aio_matches(
 provider_id TEXT NOT NULL,source_id TEXT NOT NULL, fingerprint TEXT NOT NULL,
 work_id BIGINT REFERENCES aiosources.aio_works(id) ON DELETE SET NULL,revision BIGINT NOT NULL DEFAULT 0,
 normalized TEXT NOT NULL DEFAULT '',status TEXT NOT NULL CHECK(status IN ('matched','ambiguous','unmatched')),reason TEXT NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),PRIMARY KEY(provider_id,source_id),
 FOREIGN KEY(provider_id,source_id) REFERENCES aiosources.torrents(provider_id,source_id) ON DELETE CASCADE
);
CREATE INDEX aio_matches_normalized ON aiosources.aio_matches(normalized);
CREATE INDEX aio_matches_fuzzy ON aiosources.aio_matches USING gin(normalized public.gin_trgm_ops);
CREATE TABLE aiosources.aio_match_queue(provider_id TEXT NOT NULL,source_id TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(provider_id,source_id),FOREIGN KEY(provider_id,source_id) REFERENCES aiosources.torrents(provider_id,source_id) ON DELETE CASCADE);
CREATE FUNCTION aiosources.aio_queue_torrent() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.fields::text=OLD.fields::text THEN RETURN NEW; END IF;
 INSERT INTO aiosources.aio_match_queue(provider_id,source_id) VALUES(NEW.provider_id,NEW.source_id)
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=aiosources.aio_match_queue.version+1;
 RETURN NEW;
END $$;
CREATE TRIGGER aio_queue_torrent AFTER INSERT OR UPDATE OF fields ON aiosources.torrents
FOR EACH ROW EXECUTE FUNCTION aiosources.aio_queue_torrent();
INSERT INTO aiosources.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM aiosources.torrents;
CREATE INDEX aio_matches_work ON aiosources.aio_matches(work_id) WHERE status='matched';
CREATE TABLE aiosources.aio_season_requests(tmdb_id TEXT NOT NULL,season INTEGER NOT NULL CHECK(season>=0 AND season<=999),PRIMARY KEY(tmdb_id,season));
CREATE TABLE aiosources.aio_inventory(media_type TEXT NOT NULL,tmdb_id TEXT NOT NULL,export_date TEXT NOT NULL,PRIMARY KEY(media_type,tmdb_id));
CREATE TABLE aiosources.aio_metadata_jobs(media_type TEXT NOT NULL,tmdb_id TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,attempts INTEGER NOT NULL DEFAULT 0,next_attempt TIMESTAMPTZ NOT NULL DEFAULT now(),PRIMARY KEY(media_type,tmdb_id));
CREATE TABLE aiosources.aio_state(id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(id),last_sync TIMESTAMPTZ,changes_through TIMESTAMPTZ,cache_generation BIGINT NOT NULL DEFAULT 1,metadata_generation BIGINT NOT NULL DEFAULT 1,inventory_date TEXT NOT NULL DEFAULT '');
INSERT INTO aiosources.aio_state(id) VALUES(TRUE);
CREATE TABLE aiosources.aio_availability(service TEXT NOT NULL,account TEXT NOT NULL,hash TEXT NOT NULL,cached BOOLEAN NOT NULL,checked_at TIMESTAMPTZ NOT NULL,files JSONB NOT NULL DEFAULT '[]',PRIMARY KEY(service,account,hash));
CREATE TABLE aiosources.aio_resources(service TEXT NOT NULL,account TEXT NOT NULL,resource TEXT NOT NULL,hashes TEXT[] NOT NULL,checked_at TIMESTAMPTZ NOT NULL,last_attempt TIMESTAMPTZ NOT NULL,retry INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(service,account,resource));
CREATE TABLE aiosources.aio_cleanup(account TEXT NOT NULL,hash TEXT NOT NULL,magnet_id BIGINT NOT NULL,failed BOOLEAN NOT NULL DEFAULT FALSE,created_at TIMESTAMPTZ NOT NULL DEFAULT now(),PRIMARY KEY(account,magnet_id));
CREATE TABLE aiosources.aio_incidents(service TEXT PRIMARY KEY,active BOOLEAN NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE INDEX aio_torrents_hash ON aiosources.torrents(lower(fields->>'info_hash'));
CREATE INDEX aio_torrents_mal ON aiosources.torrents((fields->>'mal_id'));
`
