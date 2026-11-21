package store

const musicSchema = `
ALTER TABLE aiosources.aio_works
 ADD COLUMN musicbrainz_id TEXT NOT NULL DEFAULT '',
 ADD COLUMN music_data JSONB NOT NULL DEFAULT '{}',
 ADD COLUMN music_normalized TEXT NOT NULL DEFAULT '',
 ADD COLUMN mb_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE aiosources.aio_works DROP CONSTRAINT aio_works_identifier_namespace, DROP CONSTRAINT aio_works_media_type_check;
ALTER TABLE aiosources.aio_works ADD CONSTRAINT aio_works_media_type_check CHECK(media_type IN ('movie','tv','book','game','music')),
ADD CONSTRAINT aio_works_identifier_namespace CHECK(
 (media_type='book' AND tmdb_id IS NULL AND openlibrary_id ~ '^OL[1-9][0-9]{0,14}[WM]$' AND igdb_id='' AND musicbrainz_id='') OR
 (media_type IN ('movie','tv') AND tmdb_id IS NOT NULL AND openlibrary_id='' AND igdb_id='' AND musicbrainz_id='') OR
 (media_type='game' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id ~ '^[1-9][0-9]{0,14}$' AND musicbrainz_id='') OR
 (media_type='music' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id='' AND musicbrainz_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'));
CREATE UNIQUE INDEX aio_works_musicbrainz ON aiosources.aio_works(musicbrainz_id) WHERE musicbrainz_id<>'';
CREATE INDEX aio_works_music_normalized ON aiosources.aio_works(music_normalized) WHERE media_type='music' AND music_normalized<>'';
CREATE INDEX aio_works_music_normalized_trgm ON aiosources.aio_works USING gin(music_normalized public.gin_trgm_ops) WHERE media_type='music' AND music_normalized<>'';

ALTER TABLE aiosources.aio_matches DROP CONSTRAINT IF EXISTS aio_matches_media_hint_check;
ALTER TABLE aiosources.aio_matches ADD CONSTRAINT aio_matches_media_hint_check CHECK(media_hint IN ('','movie','tv','book','game','music'));
ALTER TABLE aiosources.aio_matches ADD COLUMN music_kind TEXT NOT NULL DEFAULT '' CHECK(music_kind IN ('','music','lossless','mp3'));

ALTER TABLE aiosources.aio_match_reviews ADD COLUMN musicbrainz_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.aio_match_reviews DROP CONSTRAINT aio_match_reviews_media_type_check;
ALTER TABLE aiosources.aio_match_reviews ADD CONSTRAINT aio_match_reviews_media_type_check CHECK(media_type IN ('','movie','tv','book','game','music'));

ALTER TABLE aiosources.shares DROP CONSTRAINT shares_media_types_check;
ALTER TABLE aiosources.shares ADD CONSTRAINT shares_media_types_check CHECK(media_types <@ ARRAY['movie','tv','book','ebook','audiobook','game','music','other']::text[]);

ALTER TABLE aiosources.operation_runs DROP CONSTRAINT operation_runs_service_check, DROP CONSTRAINT operation_runs_kind_service_check;
ALTER TABLE aiosources.operation_runs ADD CONSTRAINT operation_runs_service_check CHECK(service IN ('tmdb','igdb','openlibrary','musicbrainz','local','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')),
ADD CONSTRAINT operation_runs_kind_service_check CHECK((kind='metadata' AND service IN ('tmdb','igdb','openlibrary','musicbrainz')) OR (kind='matching' AND service='local') OR (kind IN ('cache','download') AND service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'));

ALTER TABLE aiosources.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE aiosources.activity_logs ADD CONSTRAINT activity_logs_service_check CHECK(service IN ('','tmdb','igdb','openlibrary','musicbrainz','network','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'));

CREATE OR REPLACE FUNCTION aiosources.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN kind LIKE 'maintenance.music_%' THEN 'Music information reset' || CASE split_part(split_part(kind,'.',2),'_',2)
 WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 WHEN kind LIKE 'maintenance.games_%' THEN 'Game information reset' || CASE split_part(split_part(kind,'.',2),'_',2)
 WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 WHEN kind LIKE 'maintenance.%' THEN
 CASE split_part(split_part(kind,'.',2),'_',1)
 WHEN 'books' THEN 'Book information reset' WHEN 'videos' THEN 'Video information reset'
 WHEN 'metadata' THEN 'Information reset' WHEN 'mapping' THEN 'Automatic identification reset'
 WHEN 'cache' THEN 'Cache observations reset' ELSE 'Maintenance' END
 || CASE split_part(split_part(kind,'.',2),'_',2) WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 ELSE aiosources.pre_information_reset_activity_message(kind) END;
$$;

ALTER FUNCTION aiosources.aio_review_evidence(jsonb) RENAME TO aio_game_review_evidence;
CREATE FUNCTION aiosources.aio_review_evidence(fields JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT aiosources.aio_game_review_evidence(fields) || CASE WHEN
 fields ?| ARRAY['musicbrainz','musicbrainz_id','mbid','musicbrainz_release_group_id','musicbrainz_release_id','discogs','discogs_id','discogs_master_id','wikidata','wikidata_id','barcode','upc','ean','catalog_number','catno','label','artist','album']
 OR fields->'external_ids' ?| ARRAY['musicbrainz','musicbrainz_id','mbid','discogs','discogs_id','wikidata','wikidata_id','barcode']
 OR fields->'attributes' ?| ARRAY['musicbrainz','musicbrainz_id','mbid','discogs','discogs_id','wikidata','wikidata_id','barcode','artist','album','label']
 OR COALESCE(fields->>'media_type',fields->>'type','') IN ('music','audio','album','flac','mp3','lossless')
 OR COALESCE(fields->'categories',fields->'category','null')::text ~ '\m(3000|3010|3040|3050|3060)\M'
 THEN jsonb_strip_nulls(jsonb_build_object(
  'mbid',COALESCE(fields->'musicbrainz_release_group_id',fields->'musicbrainz_id',fields->'musicbrainz',fields->'mbid'),
  'discogs',COALESCE(fields->'discogs_master_id',fields->'discogs_id',fields->'discogs'),
  'wikidata',COALESCE(fields->'wikidata_id',fields->'wikidata'),
  'barcode',COALESCE(fields->'barcode',fields->'upc',fields->'ean'),
  'catno',COALESCE(fields->'catalog_number',fields->'catno'),
  'label',fields->'label','artist',fields->'artist','album',fields->'album',
  'music_external',NULLIF(jsonb_strip_nulls(jsonb_build_object('mbid',COALESCE(fields#>'{external_ids,musicbrainz_id}',fields#>'{external_ids,mbid}'),'discogs',COALESCE(fields#>'{external_ids,discogs_id}',fields#>'{external_ids,discogs}'),'wikidata',COALESCE(fields#>'{external_ids,wikidata_id}',fields#>'{external_ids,wikidata}'))),'{}'::jsonb),
  'music_attributes',NULLIF(jsonb_strip_nulls(jsonb_build_object('mbid',COALESCE(fields#>'{attributes,musicbrainz_id}',fields#>'{attributes,mbid}'),'artist',fields#>'{attributes,artist}','album',fields#>'{attributes,album}','label',fields#>'{attributes,label}')),'{}'::jsonb)
 )) ELSE '{}'::jsonb END
$$;

CREATE TABLE aiosources.mb_state (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 complete BOOLEAN NOT NULL DEFAULT FALSE,
 works BIGINT NOT NULL DEFAULT 0,
 releases BIGINT NOT NULL DEFAULT 0,
 artists BIGINT NOT NULL DEFAULT 0,
 labels BIGINT NOT NULL DEFAULT 0,
 covers BIGINT NOT NULL DEFAULT 0,
 imported BIGINT NOT NULL DEFAULT 0,
 position BIGINT NOT NULL DEFAULT 0,
 filtered BIGINT NOT NULL DEFAULT 0,
 skipped BIGINT NOT NULL DEFAULT 0,
 phase TEXT NOT NULL DEFAULT 'idle' CHECK(phase IN ('idle','dump','covers','projecting','cleanup','replication','recovery')),
 selection TEXT NOT NULL DEFAULT '',
 snapshot TEXT NOT NULL DEFAULT '',
 snapshot_at TIMESTAMPTZ,
 replication_sequence BIGINT NOT NULL DEFAULT 0,
 schema_sequence INT NOT NULL DEFAULT 0,
 generation BIGINT NOT NULL DEFAULT 0,
 last_import TIMESTAMPTZ,
 last_sync TIMESTAMPTZ,
 changes_through TIMESTAMPTZ,
 last_packet_at TIMESTAMPTZ,
 needs_export BOOLEAN NOT NULL DEFAULT FALSE,
 recovery_reason TEXT NOT NULL DEFAULT '',
 schema_unsupported BOOLEAN NOT NULL DEFAULT FALSE,
 retry_at TIMESTAMPTZ,
 checked_at TIMESTAMPTZ,
 error TEXT NOT NULL DEFAULT ''
);
INSERT INTO aiosources.mb_state(singleton) VALUES(TRUE);

CREATE TABLE aiosources.mb_lookups (
 kind TEXT NOT NULL,
 id INT NOT NULL,
 name TEXT NOT NULL,
 PRIMARY KEY(kind, id)
);

CREATE TABLE aiosources.mb_artists (
 id INT PRIMARY KEY,
 gid TEXT NOT NULL,
 name TEXT NOT NULL,
 sort_name TEXT NOT NULL DEFAULT '',
 normalized TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX mb_artists_gid ON aiosources.mb_artists(gid);
CREATE INDEX mb_artists_normalized ON aiosources.mb_artists(normalized) WHERE normalized<>'';

CREATE TABLE aiosources.mb_aliases (
 entity TEXT NOT NULL CHECK(entity IN ('artist','release_group','release','label')),
 id INT NOT NULL,
 entity_id INT NOT NULL,
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 locale TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(entity, id)
);
CREATE INDEX mb_aliases_entity ON aiosources.mb_aliases(entity, entity_id);
CREATE INDEX mb_aliases_normalized ON aiosources.mb_aliases(entity, normalized) WHERE normalized<>'';

CREATE TABLE aiosources.mb_artist_credits (
 id INT PRIMARY KEY,
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE aiosources.mb_artist_credit_names (
 artist_credit INT NOT NULL,
 position INT NOT NULL,
 artist_id INT NOT NULL,
 name TEXT NOT NULL DEFAULT '',
 join_phrase TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(artist_credit, position)
);
CREATE INDEX mb_artist_credit_names_artist ON aiosources.mb_artist_credit_names(artist_id);

CREATE TABLE aiosources.mb_labels (
 id INT PRIMARY KEY,
 gid TEXT NOT NULL,
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX mb_labels_gid ON aiosources.mb_labels(gid);
CREATE INDEX mb_labels_normalized ON aiosources.mb_labels(normalized) WHERE normalized<>'';

CREATE TABLE aiosources.mb_release_groups (
 id INT PRIMARY KEY,
 gid TEXT NOT NULL,
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 artist_credit INT NOT NULL DEFAULT 0,
 primary_type INT NOT NULL DEFAULT 0,
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX mb_release_groups_gid ON aiosources.mb_release_groups(gid);
CREATE INDEX mb_release_groups_ac ON aiosources.mb_release_groups(artist_credit);

CREATE TABLE aiosources.mb_release_group_secondary_types (
 release_group_id INT NOT NULL,
 secondary_type_id INT NOT NULL,
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(release_group_id, secondary_type_id)
);

CREATE TABLE aiosources.mb_releases (
 id INT PRIMARY KEY,
 gid TEXT NOT NULL,
 release_group_id INT NOT NULL,
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 artist_credit INT NOT NULL DEFAULT 0,
 status INT NOT NULL DEFAULT 0,
 language_id INT NOT NULL DEFAULT 0,
 barcode TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX mb_releases_gid ON aiosources.mb_releases(gid);
CREATE INDEX mb_releases_rg ON aiosources.mb_releases(release_group_id);
CREATE INDEX mb_releases_barcode ON aiosources.mb_releases(barcode) WHERE barcode<>'';
CREATE INDEX mb_releases_normalized ON aiosources.mb_releases(normalized) WHERE normalized<>'';

CREATE TABLE aiosources.mb_release_years (
 release_id INT NOT NULL,
 year INT NOT NULL,
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(release_id, year)
);

CREATE TABLE aiosources.mb_release_labels (
 id INT PRIMARY KEY,
 release_id INT NOT NULL,
 label_id INT NOT NULL DEFAULT 0,
 catalog_number TEXT NOT NULL DEFAULT '',
 catalog_normalized TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX mb_release_labels_release ON aiosources.mb_release_labels(release_id);
CREATE INDEX mb_release_labels_label ON aiosources.mb_release_labels(label_id) WHERE label_id>0;
CREATE INDEX mb_release_labels_catno ON aiosources.mb_release_labels(catalog_normalized) WHERE catalog_normalized<>'';

CREATE TABLE aiosources.mb_mediums (
 id INT PRIMARY KEY,
 release_id INT NOT NULL,
 position INT NOT NULL DEFAULT 0,
 format_id INT NOT NULL DEFAULT 0,
 track_count INT NOT NULL DEFAULT 0,
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX mb_mediums_release ON aiosources.mb_mediums(release_id);

CREATE TABLE aiosources.mb_urls (
 id INT PRIMARY KEY,
 discogs_kind TEXT NOT NULL DEFAULT '',
 discogs_id TEXT NOT NULL DEFAULT '',
 wikidata_id TEXT NOT NULL DEFAULT '',
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX mb_urls_discogs ON aiosources.mb_urls(discogs_id) WHERE discogs_id<>'';
CREATE INDEX mb_urls_wikidata ON aiosources.mb_urls(wikidata_id) WHERE wikidata_id<>'';

CREATE TABLE aiosources.mb_entity_urls (
 entity TEXT NOT NULL CHECK(entity IN ('release_group','release','artist')),
 link_id INT NOT NULL,
 entity_id INT NOT NULL,
 url_id INT NOT NULL,
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(entity, link_id)
);
CREATE INDEX mb_entity_urls_entity ON aiosources.mb_entity_urls(entity, entity_id);
CREATE INDEX mb_entity_urls_url ON aiosources.mb_entity_urls(url_id);

CREATE TABLE aiosources.mb_redirects (
 entity TEXT NOT NULL CHECK(entity IN ('artist','release_group','release','label')),
 source_gid TEXT NOT NULL,
 target_id INT NOT NULL,
 mb_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(entity, source_gid)
);
CREATE INDEX mb_redirects_target ON aiosources.mb_redirects(entity, target_id);

CREATE TABLE aiosources.mb_cover_art (
 id BIGINT PRIMARY KEY,
 release_id INT NOT NULL,
 is_front BOOLEAN NOT NULL DEFAULT FALSE,
 ordering INT NOT NULL DEFAULT 0,
 mb_seen BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX mb_cover_art_release_front ON aiosources.mb_cover_art(release_id) WHERE is_front;

CREATE TABLE aiosources.mb_rg_cover_art (
 release_group_id INT PRIMARY KEY,
 release_id INT NOT NULL,
 mb_seen BIGINT NOT NULL DEFAULT 0
);

CREATE FUNCTION aiosources.mb_resolve_rg(key TEXT) RETURNS TEXT LANGUAGE SQL STABLE PARALLEL SAFE AS $$
 SELECT COALESCE(
  (SELECT rg.gid FROM aiosources.mb_redirects r JOIN aiosources.mb_release_groups rg ON rg.id=r.target_id WHERE r.entity='release_group' AND r.source_gid=key LIMIT 1),
  key
 );
$$;
`
