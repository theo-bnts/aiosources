package store

const localizedPostersSchema = `
ALTER TABLE aiosources.aio_works ADD COLUMN localized_posters JSONB NOT NULL DEFAULT '{}'::jsonb
 CHECK(jsonb_typeof(localized_posters)='object');

-- Existing, visible titles acquire image paths through the ordinary metadata
-- worker. Import progress, decisions and delayed retries remain unchanged.
INSERT INTO aiosources.aio_metadata_jobs(media_type,tmdb_id)
SELECT w.media_type,w.tmdb_id FROM (
 SELECT DISTINCT work_id FROM aiosources.aio_matches WHERE status='matched' AND work_id IS NOT NULL
) matched JOIN aiosources.aio_works w ON w.id=matched.work_id
WHERE w.media_type IN ('movie','tv') AND w.catalog_import_source=''
ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION aiosources.catalog_entity_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
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
`
