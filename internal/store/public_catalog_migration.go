package store

// Public catalogs retain the same bounded publication journal as named shares.
// Only one administrator policy is saved; visitor configurations remain stateless.
const publicCatalogSchema = `
ALTER TABLE aiosources.aio_public_access
 ADD COLUMN catalog_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN catalog_generation BIGINT NOT NULL DEFAULT 1;
CREATE TABLE aiosources.public_catalog_policy (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 source_ids TEXT[] NOT NULL DEFAULT '{}',
 fields TEXT[] NOT NULL DEFAULT ARRAY['title','size','info_hash','published_at','category','categories'],
 catalog_entities JSONB NOT NULL DEFAULT '{"metadata":true,"availability":true,"identifiers":true,"languages":true,"technical":true,"files":true}'
 CHECK(jsonb_typeof(catalog_entities)='object'),
 CHECK(cardinality(fields)>0 AND fields <@ ARRAY['title','size','info_hash','published_at','category','categories']::text[])
);
INSERT INTO aiosources.public_catalog_policy(singleton) VALUES(TRUE);

CREATE FUNCTION aiosources.catalog_export_active(entity TEXT DEFAULT '') RETURNS BOOLEAN LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM aiosources.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now())
 AND (entity='' OR catalog_entities->>entity='true')) OR EXISTS(
 SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.expose_sources AND a.catalog_enabled
 AND (entity='' OR p.catalog_entities->>entity='true'));
$$;
CREATE OR REPLACE FUNCTION aiosources.sync_catalog_tracking() RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE active BOOLEAN; previous BOOLEAN;
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 SELECT aiosources.catalog_export_active() INTO active;
 SELECT enabled INTO previous FROM aiosources.catalog_tracking WHERE singleton FOR UPDATE;
 IF active=previous THEN RETURN; END IF;
 IF NOT active THEN
  PERFORM aiosources.clear_catalog_tracking();
 ELSE
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields)
  SELECT provider_id,source_id,aiosources.catalog_origin(origin,provider_id,source_id),aiosources.catalog_fields(fields)
  FROM aiosources.torrents ORDER BY last_seen_at,provider_id,source_id;
 END IF;
 UPDATE aiosources.catalog_tracking SET enabled=active WHERE singleton;
END;
$$;
CREATE OR REPLACE FUNCTION aiosources.lock_catalog_entity_writes() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(1768843109,9);
 IF aiosources.catalog_export_active('metadata') THEN
  PERFORM pg_advisory_xact_lock(1768843109,8);
 END IF;
END;
$$;
CREATE OR REPLACE FUNCTION aiosources.prune_catalog_entity_index() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,8);
 IF NOT aiosources.catalog_export_active('metadata') THEN
  DELETE FROM aiosources.catalog_entity_changes WHERE kind='metadata';
 END IF;
END;
$$;
CREATE OR REPLACE FUNCTION aiosources.catalog_entity_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE entity TEXT; label TEXT;
BEGIN
 IF TG_TABLE_NAME='aio_matches' THEN
  entity:='metadata';
  IF NEW.status<>'matched' OR NEW.work_id IS NULL THEN RETURN NULL; END IF;
  SELECT media_type||':'||tmdb_id INTO label FROM aiosources.aio_works WHERE id=NEW.work_id AND catalog_import_source='' AND media_type<>'book';
  IF label IS NULL THEN RETURN NULL; END IF;
 ELSE
  IF NEW.media_type='book' THEN RETURN NULL; END IF;
  entity:='metadata'; label:=NEW.media_type||':'||NEW.tmdb_id;
  IF NEW.catalog_import_source<>'' THEN RETURN NULL; END IF;
  IF TG_OP='UPDATE' AND OLD.revision=NEW.revision AND OLD.display_title=NEW.display_title AND OLD.localized_titles=NEW.localized_titles AND OLD.catalog_import_source=NEW.catalog_import_source THEN RETURN NULL; END IF;
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
CREATE FUNCTION aiosources.public_catalog_policy_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
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
  WHERE p.singleton AND w.catalog_import_source='' AND w.media_type<>'book' AND EXISTS(
   SELECT 1 FROM aiosources.aio_matches m JOIN aiosources.torrents t USING(provider_id,source_id)
   WHERE m.work_id=w.id AND ` + aioPublishedMatch + `
   AND (cardinality(p.source_ids)=0 OR t.provider_id=ANY(p.source_ids))) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NULL;
END;
$$;
CREATE TRIGGER public_catalog_access_policy AFTER UPDATE OF enabled,expose_sources,catalog_enabled ON aiosources.aio_public_access
 FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();
CREATE TRIGGER public_catalog_data_policy AFTER UPDATE OF source_ids,fields,catalog_entities ON aiosources.public_catalog_policy
 FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();

ALTER FUNCTION aiosources.access_activity_message(TEXT) RENAME TO access_activity_message_before_public_catalog;
CREATE FUNCTION aiosources.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_public_catalog(kind), CASE kind
 WHEN 'security.public_catalog_enabled' THEN 'Public catalog sharing enabled'
 WHEN 'security.public_catalog_disabled' THEN 'Public catalog sharing disabled'
 WHEN 'security.public_catalog_permissions_changed' THEN 'Public catalog data permissions changed'
 WHEN 'catalog.public_link_created' THEN 'Public catalog connection created'
 WHEN 'catalog.public_exported' THEN 'Public catalog page exported'
 WHEN 'catalog.public_failed' THEN 'Public catalog request failed'
 ELSE NULL END);
$$;
`
