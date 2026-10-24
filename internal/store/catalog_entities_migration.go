package store

// This is an index of the latest changed identity, not another copy of metadata,
// cache files or torrent payloads. Transaction watermarks avoid sequence/commit
// reordering without serializing metadata, imports and provider requests.
const catalogEntitiesSchema = `
ALTER TABLE ingest.shares ADD COLUMN catalog_entities JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(catalog_entities)='object');
ALTER TABLE ingest.aio_works ADD COLUMN catalog_import_source TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.aio_works ADD COLUMN catalog_import_updated_at TIMESTAMPTZ;
ALTER TABLE ingest.aio_shared_availability ADD COLUMN catalog_import_source TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.remote_checkpoints ADD COLUMN catalog_entities JSONB NOT NULL DEFAULT '{}';
CREATE TABLE ingest.catalog_entity_changes (
 kind TEXT NOT NULL CHECK(kind='metadata'),
 identity TEXT NOT NULL,
 changed_tx BIGINT NOT NULL,
 PRIMARY KEY(kind,identity)
);
CREATE INDEX catalog_entity_changes_cursor ON ingest.catalog_entity_changes(changed_tx,(kind||':'||identity));
CREATE TABLE ingest.run_catalog_entities (
 run_id TEXT NOT NULL REFERENCES ingest.runs(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('metadata','availability')),
 identity TEXT NOT NULL,
 data JSONB NOT NULL,
 PRIMARY KEY(run_id,kind,identity)
);
-- Call before data-table locks, after the existing metadata fence. A shared
-- policy fence coordinates opt-in seeding; only the short publication phase
-- serializes overlapping changes to the same natural identity.
CREATE FUNCTION ingest.lock_catalog_entity_writes() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock_shared(1768843109,9);
 IF EXISTS(SELECT 1 FROM ingest.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now()) AND catalog_entities->>'metadata'='true') THEN
  PERFORM pg_advisory_xact_lock(1768843109,8);
 END IF;
END;
$$;
CREATE FUNCTION ingest.catalog_entity_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE entity TEXT; label TEXT;
BEGIN
 IF TG_TABLE_NAME='aio_matches' THEN
  entity:='metadata';
  IF NEW.status<>'matched' OR NEW.work_id IS NULL THEN RETURN NULL; END IF;
  SELECT media_type||':'||tmdb_id INTO label FROM ingest.aio_works WHERE id=NEW.work_id AND catalog_import_source='';
  IF label IS NULL THEN RETURN NULL; END IF;
 ELSE
  entity:='metadata'; label:=NEW.media_type||':'||NEW.tmdb_id;
  IF NEW.catalog_import_source<>'' THEN RETURN NULL; END IF;
  IF TG_OP='UPDATE' AND OLD.revision=NEW.revision AND OLD.display_title=NEW.display_title AND OLD.localized_titles=NEW.localized_titles AND OLD.catalog_import_source=NEW.catalog_import_source THEN RETURN NULL; END IF;
 END IF;
 IF TG_TABLE_NAME='aio_works' THEN
  IF NOT EXISTS(SELECT 1 FROM ingest.aio_matches WHERE work_id=NEW.id AND status='matched') THEN RETURN NULL; END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM ingest.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now()) AND catalog_entities->>entity='true') THEN RETURN NULL; END IF;
 INSERT INTO ingest.catalog_entity_changes(kind,identity,changed_tx) VALUES(entity,label,pg_current_xact_id()::text::bigint)
 ON CONFLICT(kind,identity) DO UPDATE SET changed_tx=EXCLUDED.changed_tx;
 RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_metadata_changed AFTER INSERT OR UPDATE ON ingest.aio_works FOR EACH ROW EXECUTE FUNCTION ingest.catalog_entity_changed();
CREATE TRIGGER catalog_match_changed AFTER INSERT OR UPDATE ON ingest.aio_matches FOR EACH ROW EXECUTE FUNCTION ingest.catalog_entity_changed();
CREATE FUNCTION ingest.catalog_entity_deleted() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM ingest.catalog_entity_changes WHERE kind='metadata' AND identity=OLD.media_type||':'||OLD.tmdb_id;
 RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_metadata_deleted AFTER DELETE ON ingest.aio_works FOR EACH ROW EXECUTE FUNCTION ingest.catalog_entity_deleted();

CREATE FUNCTION ingest.prune_catalog_entity_index() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,8);
 IF NOT EXISTS(SELECT 1 FROM ingest.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now()) AND catalog_entities->>'metadata'='true') THEN
  DELETE FROM ingest.catalog_entity_changes WHERE kind='metadata';
 END IF;

END;
$$;
CREATE FUNCTION ingest.catalog_entity_policy_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM ingest.prune_catalog_entity_index();
 RETURN NULL;
END;
$$;
CREATE TRIGGER catalog_entity_policy_changed AFTER INSERT OR DELETE OR UPDATE OF enabled,expires_at,catalog_entities ON ingest.shares FOR EACH STATEMENT EXECUTE FUNCTION ingest.catalog_entity_policy_changed();
`
