package store

const gamesSchema = `
ALTER TABLE aiosources.aio_works ADD COLUMN igdb_id TEXT NOT NULL DEFAULT '', ADD COLUMN game_data JSONB NOT NULL DEFAULT '{}', ADD COLUMN igdb_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE aiosources.aio_works DROP CONSTRAINT aio_works_identifier_namespace, DROP CONSTRAINT aio_works_media_type_check;
ALTER TABLE aiosources.aio_works ADD CONSTRAINT aio_works_media_type_check CHECK(media_type IN ('movie','tv','book','game')),
ADD CONSTRAINT aio_works_identifier_namespace CHECK(
 (media_type='book' AND tmdb_id IS NULL AND openlibrary_id ~ '^OL[1-9][0-9]{0,14}[WM]$' AND igdb_id='') OR
 (media_type IN ('movie','tv') AND tmdb_id IS NOT NULL AND openlibrary_id='' AND igdb_id='') OR
 (media_type='game' AND tmdb_id IS NULL AND openlibrary_id='' AND igdb_id ~ '^[1-9][0-9]{0,14}$'));
CREATE UNIQUE INDEX aio_works_igdb ON aiosources.aio_works(igdb_id) WHERE igdb_id<>'';
ALTER TABLE aiosources.aio_matches ADD COLUMN media_hint TEXT NOT NULL DEFAULT '' CHECK(media_hint IN ('','movie','tv','book','game'));
ALTER TABLE aiosources.aio_match_reviews ADD COLUMN igdb_id TEXT NOT NULL DEFAULT '';
ALTER TABLE aiosources.aio_match_reviews DROP CONSTRAINT aio_match_reviews_media_type_check;
ALTER TABLE aiosources.aio_match_reviews ADD CONSTRAINT aio_match_reviews_media_type_check CHECK(media_type IN ('','movie','tv','book','game'));
ALTER TABLE aiosources.shares DROP CONSTRAINT shares_media_types_check;
ALTER TABLE aiosources.shares ADD CONSTRAINT shares_media_types_check CHECK(media_types <@ ARRAY['movie','tv','book','ebook','audiobook','game','other']::text[]);
ALTER TABLE aiosources.operation_runs DROP CONSTRAINT operation_runs_service_check, DROP CONSTRAINT operation_runs_kind_service_check;
ALTER TABLE aiosources.operation_runs ADD CONSTRAINT operation_runs_service_check CHECK(service IN ('tmdb','igdb','openlibrary','local','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')),
ADD CONSTRAINT operation_runs_kind_service_check CHECK((kind='metadata' AND service IN ('tmdb','igdb','openlibrary')) OR (kind='matching' AND service='local') OR (kind IN ('cache','download') AND service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'));
ALTER TABLE aiosources.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE aiosources.activity_logs ADD CONSTRAINT activity_logs_service_check CHECK(service IN ('','tmdb','igdb','openlibrary','network','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'));
CREATE OR REPLACE FUNCTION aiosources.activity_message(kind TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN kind LIKE 'maintenance.games_%' THEN 'Game information reset' || CASE split_part(split_part(kind,'.',2),'_',2)
 WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 WHEN kind LIKE 'maintenance.%' THEN
 CASE split_part(split_part(kind,'.',2),'_',1)
 WHEN 'books' THEN 'Book information reset' WHEN 'videos' THEN 'Video information reset'
 WHEN 'metadata' THEN 'Information reset' WHEN 'mapping' THEN 'Automatic identification reset'
 WHEN 'cache' THEN 'Cache observations reset' ELSE 'Maintenance' END
 || CASE split_part(split_part(kind,'.',2),'_',2) WHEN 'completed' THEN ' completed' WHEN 'interrupted' THEN ' interrupted' ELSE ' failed' END
 ELSE aiosources.pre_information_reset_activity_message(kind) END;
$$;
ALTER FUNCTION aiosources.aio_review_evidence(jsonb) RENAME TO aio_bibliographic_review_evidence;
CREATE FUNCTION aiosources.aio_review_evidence(fields JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT aiosources.aio_bibliographic_review_evidence(fields) || CASE WHEN
 fields ?| ARRAY['igdb','igdb_id','game_platform'] OR fields->'external_ids' ?| ARRAY['igdb','igdb_id'] OR fields->'attributes' ?| ARRAY['igdb','igdbid','igdb_id'] OR COALESCE(fields->>'media_type',fields->>'type','') IN ('game','games','videogame') OR COALESCE(fields->'categories',fields->'category','null')::text ~ '\m(1[0-9]{3}|4050)\M'
 THEN jsonb_strip_nulls(jsonb_build_object('igdb',fields->'igdb','igdb_id',fields->'igdb_id','game_platform',fields->'game_platform','platform',fields->'platform','platforms',fields->'platforms',
 'game_external',NULLIF(jsonb_strip_nulls(jsonb_build_object('igdb',fields#>'{external_ids,igdb}','igdb_id',fields#>'{external_ids,igdb_id}')),'{}'::jsonb),
 'game_attributes',NULLIF(jsonb_strip_nulls(jsonb_build_object('igdb',fields#>'{attributes,igdb}','igdbid',fields#>'{attributes,igdbid}','igdb_id',fields#>'{attributes,igdb_id}')),'{}'::jsonb))) ELSE '{}'::jsonb END
$$;
CREATE TABLE aiosources.igdb_state (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 complete BOOLEAN NOT NULL DEFAULT FALSE, works BIGINT NOT NULL DEFAULT 0, processed BIGINT NOT NULL DEFAULT 0, total BIGINT NOT NULL DEFAULT 0,
 phase TEXT NOT NULL DEFAULT 'initial' CHECK(phase IN ('initial','changes','reconciliation')),
 after_id BIGINT NOT NULL DEFAULT 0, generation BIGINT NOT NULL DEFAULT 0,
 started_at TIMESTAMPTZ, last_sync TIMESTAMPTZ, changes_through TIMESTAMPTZ, last_reconciliation TIMESTAMPTZ,
 retry_at TIMESTAMPTZ, error TEXT NOT NULL DEFAULT ''
);
INSERT INTO aiosources.igdb_state(singleton) VALUES(TRUE);
` + gamesSharingMetadataSchema

// Shared metadata remains video-only, as for the independent book catalog.
const gamesSharingMetadataSchema = `
CREATE OR REPLACE FUNCTION aiosources.catalog_entity_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE entity TEXT; label TEXT;
BEGIN
 IF TG_TABLE_NAME='aio_matches' THEN
  entity:='metadata';
  IF NEW.status<>'matched' OR NEW.work_id IS NULL THEN RETURN NULL; END IF;
  SELECT media_type||':'||tmdb_id INTO label FROM aiosources.aio_works WHERE id=NEW.work_id AND catalog_import_source='' AND media_type IN ('movie','tv');
  IF label IS NULL THEN RETURN NULL; END IF;
 ELSE
  IF NEW.media_type NOT IN ('movie','tv') THEN RETURN NULL; END IF;
  entity:='metadata'; label:=NEW.media_type||':'||NEW.tmdb_id;
  IF NEW.catalog_import_source<>'' THEN RETURN NULL; END IF;
  IF TG_OP='UPDATE' AND OLD.revision=NEW.revision AND OLD.display_title=NEW.display_title AND OLD.localized_titles=NEW.localized_titles AND OLD.poster_path=NEW.poster_path AND OLD.localized_posters=NEW.localized_posters AND OLD.catalog_import_source=NEW.catalog_import_source THEN RETURN NULL; END IF;
 END IF;
 IF TG_TABLE_NAME='aio_works' THEN
  IF NOT EXISTS(SELECT 1 FROM aiosources.aio_matches WHERE work_id=NEW.id AND status='matched') THEN RETURN NULL; END IF;
 END IF;
 IF NOT aiosources.catalog_export_active(entity) THEN RETURN NULL; END IF;
 INSERT INTO aiosources.catalog_entity_changes(kind,identity,changed_tx) VALUES(entity,label,pg_current_xact_id()::text::bigint)
 ON CONFLICT(kind,identity) DO UPDATE SET changed_tx=EXCLUDED.changed_tx;
 RETURN NULL;
END;
$$;
CREATE OR REPLACE FUNCTION aiosources.public_catalog_policy_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_TABLE_NAME='aio_public_access' THEN
  IF (NEW.enabled,NEW.expose_sources,NEW.catalog_enabled) IS NOT DISTINCT FROM (OLD.enabled,OLD.expose_sources,OLD.catalog_enabled) THEN RETURN NULL; END IF;
  UPDATE aiosources.public_catalog_policy SET revision=revision+1 WHERE singleton;
 END IF;
 PERFORM aiosources.sync_catalog_tracking();
 PERFORM aiosources.prune_catalog_entity_index();
 IF EXISTS(SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.expose_sources AND a.catalog_enabled AND p.catalog_entities->>'metadata'='true') THEN
  INSERT INTO aiosources.catalog_entity_changes(kind,identity,changed_tx)
  SELECT 'metadata',w.media_type||':'||w.tmdb_id,pg_current_xact_id()::text::bigint
  FROM aiosources.aio_works w CROSS JOIN aiosources.public_catalog_policy p
  WHERE p.singleton AND w.catalog_import_source='' AND w.media_type IN ('movie','tv') AND EXISTS(
   SELECT 1 FROM aiosources.aio_matches m JOIN aiosources.torrents t USING(provider_id,source_id)
   WHERE m.work_id=w.id AND m.status='matched' AND m.fingerprint=t.fields_fingerprint AND m.revision=w.revision
AND NOT EXISTS(SELECT 1 FROM aiosources.aio_match_queue q WHERE q.provider_id=t.provider_id AND q.source_id=t.source_id) AND NOT w.adult
   AND (cardinality(p.source_ids)=0 OR t.provider_id=ANY(p.source_ids))) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NULL;
END;
$$;
`
