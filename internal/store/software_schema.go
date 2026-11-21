package store

const softwareSchema = `
ALTER TABLE aiosources.aio_works
 ADD COLUMN software_key TEXT NOT NULL DEFAULT '',
 ADD COLUMN software_data JSONB NOT NULL DEFAULT '{}',
 ADD COLUMN software_normalized TEXT NOT NULL DEFAULT '',
 ADD COLUMN sw_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE aiosources.aio_works DROP CONSTRAINT aio_works_identifier_namespace, DROP CONSTRAINT aio_works_media_type_check;
ALTER TABLE aiosources.aio_works ADD CONSTRAINT aio_works_media_type_check CHECK(media_type IN ('movie','tv','book','game','software','music')),
ADD CONSTRAINT aio_works_identifier_namespace CHECK(
 (media_type='book' AND tmdb_id IS NULL AND openlibrary_id ~ '^OL[1-9][0-9]{0,14}[WM]$' AND igdb_id='' AND musicbrainz_id='' AND software_key='') OR
 (media_type IN ('movie','tv') AND tmdb_id IS NOT NULL AND openlibrary_id='' AND igdb_id='' AND musicbrainz_id='' AND software_key='') OR
 (media_type='game' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id ~ '^[1-9][0-9]{0,14}$' AND musicbrainz_id='' AND software_key='') OR
 (media_type='software' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id='' AND musicbrainz_id='' AND software_key ~ '^(winget|brew|cpe):[a-z0-9][a-z0-9._:-]{0,190}$') OR
 (media_type='music' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id='' AND musicbrainz_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' AND software_key=''));
CREATE UNIQUE INDEX aio_works_software_key ON aiosources.aio_works(software_key) WHERE software_key<>'';
CREATE INDEX aio_works_software_normalized ON aiosources.aio_works(software_normalized) WHERE media_type='software' AND software_normalized<>'';
CREATE INDEX aio_works_software_normalized_trgm ON aiosources.aio_works USING gin(software_normalized gin_trgm_ops) WHERE media_type='software' AND software_normalized<>'';

ALTER TABLE aiosources.aio_matches DROP CONSTRAINT IF EXISTS aio_matches_media_hint_check;
ALTER TABLE aiosources.aio_matches ADD CONSTRAINT aio_matches_media_hint_check CHECK(media_hint IN ('','movie','tv','book','game','software','music'));
ALTER TABLE aiosources.aio_matches ADD COLUMN software_kind TEXT NOT NULL DEFAULT '' CHECK(software_kind IN ('','pc','0day','iso','mac','mobile','ios','android'));

ALTER TABLE aiosources.aio_match_reviews ADD COLUMN software_key TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.aio_match_reviews DROP CONSTRAINT aio_match_reviews_media_type_check;
ALTER TABLE aiosources.aio_match_reviews ADD CONSTRAINT aio_match_reviews_media_type_check CHECK(media_type IN ('','movie','tv','book','game','software','music'));

ALTER TABLE aiosources.shares DROP CONSTRAINT shares_media_types_check;
ALTER TABLE aiosources.shares ADD CONSTRAINT shares_media_types_check CHECK(media_types <@ ARRAY['movie','tv','book','ebook','audiobook','game','software','music','other']::text[]);

ALTER TABLE aiosources.operation_runs DROP CONSTRAINT operation_runs_service_check, DROP CONSTRAINT operation_runs_kind_service_check;
ALTER TABLE aiosources.operation_runs ADD CONSTRAINT operation_runs_service_check CHECK(service IN ('tmdb','igdb','software','musicbrainz','openlibrary','local','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')),
ADD CONSTRAINT operation_runs_kind_service_check CHECK((kind='metadata' AND service IN ('tmdb','igdb','software','musicbrainz','openlibrary')) OR (kind='matching' AND service='local') OR (kind IN ('cache','download') AND service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'));

ALTER TABLE aiosources.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE aiosources.activity_logs ADD CONSTRAINT activity_logs_service_check CHECK(service IN ('','tmdb','igdb','software','musicbrainz','openlibrary','network','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'));

CREATE OR REPLACE FUNCTION aiosources.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN kind LIKE 'maintenance.software_%' THEN 'Software information reset' || CASE split_part(split_part(kind,'.',2),'_',2)
 WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 WHEN kind LIKE 'maintenance.music_%' THEN 'Music information reset' || CASE split_part(split_part(kind,'.',2),'_',2)
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

ALTER FUNCTION aiosources.aio_review_evidence(jsonb) RENAME TO aio_music_review_evidence;
CREATE FUNCTION aiosources.aio_review_evidence(fields JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT aiosources.aio_music_review_evidence(fields) || CASE WHEN
 (fields ?| ARRAY['software_key','winget_id','winget','homebrew_cask','brew_cask','cask','nvd_cpe','cpe','vendor','publisher','version','software_version','architecture','arch','os']
  OR fields->'external_ids' ?| ARRAY['software_key','winget_id','winget','homebrew_cask','nvd_cpe','cpe']
  OR fields->'attributes' ?| ARRAY['software_key','winget_id','homebrew_cask','nvd_cpe','vendor','publisher','version','architecture','os']
  OR COALESCE(fields->>'media_type',fields->>'type','') IN ('software','app','application','apps','0day','iso','mac','macos','mobile','ios','android','apk')
  OR COALESCE(fields->'categories',fields->'category','null')::text ~ '\m(4000|4010|4020|4030|4040|4060|4070)\M')
 AND NOT (COALESCE(fields->'categories',fields->'category','null')::text ~ '\m4050\M')
 THEN jsonb_strip_nulls(jsonb_build_object(
  'software_key',COALESCE(fields->'software_key',fields#>'{external_ids,software_key}',fields#>'{attributes,software_key}'),
  'winget_id',COALESCE(fields->'winget_id',fields->'winget',fields#>'{external_ids,winget_id}',fields#>'{attributes,winget_id}'),
  'homebrew_cask',COALESCE(fields->'homebrew_cask',fields->'brew_cask',fields->'cask',fields#>'{external_ids,homebrew_cask}',fields#>'{attributes,homebrew_cask}'),
  'nvd_cpe',COALESCE(fields->'nvd_cpe',fields->'cpe',fields#>'{external_ids,nvd_cpe}',fields#>'{attributes,nvd_cpe}'),
  'vendor',COALESCE(fields->'vendor',fields->'publisher',fields#>'{attributes,vendor}',fields#>'{attributes,publisher}'),
  'version',COALESCE(fields->'version',fields->'software_version',fields#>'{attributes,version}'),
  'architecture',COALESCE(fields->'architecture',fields->'arch',fields#>'{attributes,architecture}'),
  'os',COALESCE(fields->'os',fields#>'{attributes,os}')
 )) ELSE '{}'::jsonb END
$$;

CREATE TABLE aiosources.sw_sync_state (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 complete BOOLEAN NOT NULL DEFAULT FALSE,
 works BIGINT NOT NULL DEFAULT 0,
 nvd_entries BIGINT NOT NULL DEFAULT 0,
 winget_entries BIGINT NOT NULL DEFAULT 0,
 homebrew_entries BIGINT NOT NULL DEFAULT 0,
 icons BIGINT NOT NULL DEFAULT 0,
 icons_indexed BIGINT NOT NULL DEFAULT 0,
 imported BIGINT NOT NULL DEFAULT 0,
 position BIGINT NOT NULL DEFAULT 0,
 filtered BIGINT NOT NULL DEFAULT 0,
 skipped BIGINT NOT NULL DEFAULT 0,
 phase TEXT NOT NULL DEFAULT 'idle' CHECK(phase IN ('idle','nvd_meta','nvd_dump','nvd_incremental','winget','homebrew','icons','merging','projecting','cleanup','recovery')),
 selection TEXT NOT NULL DEFAULT '',
 generation BIGINT NOT NULL DEFAULT 0,
 nvd_snapshot TEXT NOT NULL DEFAULT '',
 nvd_snapshot_sha256 TEXT NOT NULL DEFAULT '',
 nvd_snapshot_at TIMESTAMPTZ,
 nvd_changes_through TIMESTAMPTZ,
 winget_etag TEXT NOT NULL DEFAULT '',
 winget_synced_at TIMESTAMPTZ,
 homebrew_etag TEXT NOT NULL DEFAULT '',
 homebrew_synced_at TIMESTAMPTZ,
 icons_etag TEXT NOT NULL DEFAULT '',
 icons_synced_at TIMESTAMPTZ,
 last_import TIMESTAMPTZ,
 last_sync TIMESTAMPTZ,
 needs_full_import BOOLEAN NOT NULL DEFAULT FALSE,
 recovery_reason TEXT NOT NULL DEFAULT '',
 retry_at TIMESTAMPTZ,
 checked_at TIMESTAMPTZ,
 error TEXT NOT NULL DEFAULT ''
);
INSERT INTO aiosources.sw_sync_state(singleton) VALUES(TRUE);

CREATE TABLE aiosources.sw_source_entries (
 source TEXT NOT NULL CHECK(source IN ('nvd','winget','homebrew')),
 external_id TEXT NOT NULL,
 part TEXT NOT NULL DEFAULT 'a' CHECK(part IN ('a','o')),
 name TEXT NOT NULL,
 vendor TEXT NOT NULL DEFAULT '',
 vendor_norm TEXT NOT NULL DEFAULT '',
 product_norm TEXT NOT NULL DEFAULT '',
 description TEXT NOT NULL DEFAULT '',
 license TEXT NOT NULL DEFAULT '',
 latest_version TEXT NOT NULL DEFAULT '',
 homepage_key TEXT NOT NULL DEFAULT '',
 platforms TEXT[] NOT NULL DEFAULT '{}',
 tags TEXT[] NOT NULL DEFAULT '{}',
 languages TEXT[] NOT NULL DEFAULT '{}',
 aliases TEXT[] NOT NULL DEFAULT '{}',
 is_plugin BOOLEAN NOT NULL DEFAULT FALSE,
 last_modified_at TIMESTAMPTZ,
 sw_seen BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(source, external_id)
);
CREATE INDEX sw_source_entries_source_seen ON aiosources.sw_source_entries(source, sw_seen);

CREATE TABLE aiosources.sw_icons (
 reference TEXT PRIMARY KEY CHECK(reference ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 sw_seen BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX sw_icons_normalized ON aiosources.sw_icons(normalized) WHERE normalized<>'';

CREATE TABLE aiosources.sw_catalog (
 software_key TEXT PRIMARY KEY CHECK(software_key ~ '^(winget|brew|cpe):[a-z0-9][a-z0-9._:-]{0,190}$'),
 name TEXT NOT NULL,
 normalized TEXT NOT NULL DEFAULT '',
 vendor TEXT NOT NULL DEFAULT '',
 vendor_norm TEXT NOT NULL DEFAULT '',
 part TEXT NOT NULL DEFAULT 'a' CHECK(part IN ('a','o')),
 description TEXT NOT NULL DEFAULT '',
 license TEXT NOT NULL DEFAULT '',
 latest_version TEXT NOT NULL DEFAULT '',
 platforms TEXT[] NOT NULL DEFAULT '{}',
 tags TEXT[] NOT NULL DEFAULT '{}',
 languages TEXT[] NOT NULL DEFAULT '{}',
 nvd_cpe TEXT NOT NULL DEFAULT '',
 winget_id TEXT NOT NULL DEFAULT '',
 homebrew_cask TEXT NOT NULL DEFAULT '',
 icon_ref TEXT NOT NULL DEFAULT '' CHECK(icon_ref='' OR icon_ref ~ '^[a-z0-9][a-z0-9-]{0,127}$'),
 sw_generation BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX sw_catalog_normalized ON aiosources.sw_catalog(normalized) WHERE normalized<>'';
CREATE INDEX sw_catalog_normalized_trgm ON aiosources.sw_catalog USING gin(normalized gin_trgm_ops) WHERE normalized<>'';
CREATE INDEX sw_catalog_vendor_norm ON aiosources.sw_catalog(vendor_norm) WHERE vendor_norm<>'';
CREATE INDEX sw_catalog_vendor_norm_trgm ON aiosources.sw_catalog USING gin(vendor_norm gin_trgm_ops) WHERE vendor_norm<>'';

CREATE TABLE aiosources.sw_aliases (
 software_key TEXT NOT NULL REFERENCES aiosources.sw_catalog(software_key) ON DELETE CASCADE,
 alias TEXT NOT NULL,
 normalized TEXT NOT NULL,
 PRIMARY KEY(software_key, normalized)
);
CREATE INDEX sw_aliases_normalized ON aiosources.sw_aliases(normalized) WHERE normalized<>'';
CREATE INDEX sw_aliases_normalized_trgm ON aiosources.sw_aliases USING gin(normalized gin_trgm_ops) WHERE normalized<>'';

CREATE TABLE aiosources.sw_external_ids (
 kind TEXT NOT NULL CHECK(kind IN ('nvd_cpe','winget_id','homebrew_cask')),
 external_id TEXT NOT NULL,
 external_lower TEXT NOT NULL,
 software_key TEXT NOT NULL REFERENCES aiosources.sw_catalog(software_key) ON DELETE CASCADE,
 PRIMARY KEY(kind, external_lower)
);
CREATE INDEX sw_external_ids_software_key ON aiosources.sw_external_ids(software_key);

CREATE FUNCTION aiosources.sw_resolve_key(raw TEXT) RETURNS TEXT LANGUAGE SQL STABLE PARALLEL SAFE AS $$
 SELECT COALESCE(
  (SELECT c.software_key FROM aiosources.sw_catalog c WHERE c.software_key=lower(trim(raw)) LIMIT 1),
  (SELECT e.software_key FROM aiosources.sw_external_ids e WHERE e.external_lower=lower(trim(raw)) LIMIT 1),
  lower(trim(raw))
 );
$$;
`
