package store

// Existing run snapshots remain full; only newly admitted work uses the new
// compact default. Published records and TMDB correspondence evidence are not
// rewritten during this upgrade.
const storageSchema = `
ALTER FUNCTION aiosources.activity_message(TEXT) RENAME TO pre_storage_activity_message;
CREATE FUNCTION aiosources.activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN kind='storage.sources_reset' THEN 'Source data reset; definitions and credentials retained' ELSE aiosources.pre_storage_activity_message(kind) END;
$$;

CREATE TABLE aiosources.storage_settings (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 mode TEXT NOT NULL DEFAULT 'compact' CHECK(mode IN ('compact','full')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0)
);
INSERT INTO aiosources.storage_settings(singleton) VALUES(TRUE);
ALTER TABLE aiosources.runs ADD COLUMN storage_mode TEXT NOT NULL DEFAULT 'full'
 CHECK(storage_mode IN ('compact','full'));
ALTER TABLE aiosources.runs ALTER COLUMN storage_mode SET DEFAULT 'compact';

-- Sharing can never export arbitrary fields, so its snapshot must not copy
-- descriptions, detail payloads or private URLs even in full storage mode.
CREATE FUNCTION aiosources.catalog_fields(value JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT COALESCE(jsonb_object_agg(key,item),'{}'::jsonb) FROM jsonb_each(value) AS entry(key,item)
 WHERE key=ANY(ARRAY['title','size','info_hash','leechers','published_at','category','categories'])
 AND (jsonb_typeof(item) IN ('string','number','boolean','null') OR
 (jsonb_typeof(item)='array' AND NOT jsonb_path_exists(item,'$[*] ? (@.type() == "object" || @.type() == "array")')));
$$;
CREATE TABLE aiosources.catalog_tracking (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 enabled BOOLEAN NOT NULL
);
INSERT INTO aiosources.catalog_tracking(singleton,enabled)
SELECT TRUE,EXISTS(SELECT 1 FROM aiosources.shares WHERE enabled AND (expires_at IS NULL OR expires_at>now()));

CREATE OR REPLACE FUNCTION aiosources.catalog_journal_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('aiosources.storage_reset',TRUE)='on' THEN RETURN NULL; END IF;
 IF TG_OP IN ('DELETE','UPDATE') AND current_setting('aiosources.history_retention',TRUE)='on' THEN RETURN NULL; END IF;
 RAISE EXCEPTION 'Published catalogue history is immutable';
END;
$$;

-- Called under the publication lock. Keep the sequence monotonic so expired
-- readers cannot mistake an unrelated rebuilt snapshot for their continuation.
CREATE FUNCTION aiosources.clear_catalog_tracking() RETURNS VOID LANGUAGE plpgsql AS $$
BEGIN
 UPDATE aiosources.catalog_retention SET floor=GREATEST(floor,COALESCE((SELECT max(sequence) FROM aiosources.catalog_journal),0));
 PERFORM set_config('aiosources.storage_reset','on',TRUE);
 TRUNCATE aiosources.catalog_journal,aiosources.catalog_baselines;
 PERFORM set_config('aiosources.storage_reset','off',TRUE);
END;
$$;

CREATE FUNCTION aiosources.sync_catalog_tracking() RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE active BOOLEAN; previous BOOLEAN;
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 SELECT EXISTS(SELECT 1 FROM aiosources.shares WHERE enabled AND (expires_at IS NULL OR expires_at>clock_timestamp())) INTO active;
 SELECT enabled INTO previous FROM aiosources.catalog_tracking WHERE singleton FOR UPDATE;
 IF active=previous THEN RETURN; END IF;
 IF NOT active THEN
  PERFORM aiosources.clear_catalog_tracking();
 ELSE
  -- Capture the current catalog before any subsequent publication can commit.
  -- First-time and re-enabled recipients can therefore request a complete Full.
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields)
  SELECT provider_id,source_id,aiosources.catalog_origin(origin,provider_id,source_id),aiosources.catalog_fields(fields)
  FROM aiosources.torrents ORDER BY last_seen_at,provider_id,source_id;
 END IF;
 UPDATE aiosources.catalog_tracking SET enabled=active WHERE singleton;
END;
$$;

CREATE FUNCTION aiosources.catalog_tracking_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM aiosources.sync_catalog_tracking();
 RETURN NULL;
END;
$$;
-- Access counters do not change policy and must not acquire the publication
-- lock after an export has locked its retained snapshot boundary.
CREATE TRIGGER catalog_tracking_policy_lock BEFORE INSERT OR DELETE OR UPDATE OF enabled,expires_at ON aiosources.shares
 FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_publication_lock();
CREATE TRIGGER catalog_tracking_policy_changed AFTER INSERT OR DELETE OR UPDATE OF enabled,expires_at ON aiosources.shares
 FOR EACH STATEMENT EXECUTE FUNCTION aiosources.catalog_tracking_changed();

CREATE OR REPLACE FUNCTION aiosources.catalog_publication_record() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE previous_origin JSONB; next_origin JSONB;
BEGIN
 IF NOT (SELECT enabled FROM aiosources.catalog_tracking WHERE singleton) THEN RETURN NULL; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.provider_id=NEW.provider_id AND OLD.source_id=NEW.source_id
   AND aiosources.catalog_fields(OLD.fields)=aiosources.catalog_fields(NEW.fields)
   AND OLD.origin IS NOT DISTINCT FROM NEW.origin THEN RETURN NULL; END IF;
 END IF;
 IF TG_OP IN ('UPDATE','DELETE') THEN previous_origin:=aiosources.catalog_origin(OLD.origin,OLD.provider_id,OLD.source_id); END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN next_origin:=aiosources.catalog_origin(NEW.origin,NEW.provider_id,NEW.source_id); END IF;
 IF TG_OP='DELETE' OR (TG_OP='UPDATE' AND (OLD.provider_id<>NEW.provider_id OR OLD.source_id<>NEW.source_id OR previous_origin<>next_origin)) THEN
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields,deleted)
  VALUES(OLD.provider_id,OLD.source_id,previous_origin,'{}',TRUE);
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') THEN
  INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields)
  VALUES(NEW.provider_id,NEW.source_id,next_origin,aiosources.catalog_fields(NEW.fields));
 END IF;
 RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION aiosources.catalog_publication_truncate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 IF current_setting('aiosources.storage_reset',TRUE)='on'
  OR NOT (SELECT enabled FROM aiosources.catalog_tracking WHERE singleton) THEN RETURN NULL; END IF;
 INSERT INTO aiosources.catalog_journal(provider_id,source_id,origin,fields,deleted)
 SELECT provider_id,source_id,aiosources.catalog_origin(origin,provider_id,source_id),'{}',TRUE FROM aiosources.torrents;
 RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION aiosources.torrent_history_truncate() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('aiosources.storage_reset',TRUE)='on' THEN RETURN NULL; END IF;
 PERFORM pg_advisory_xact_lock(1768843109,3);
 INSERT INTO aiosources.torrent_history(occurrence_id,provider_id,source_id,run_id,kind,before_fields,origin,before_origin)
 SELECT occurrence_id,provider_id,source_id,NULLIF(current_setting('aiosources.publication_run_id',TRUE),''),'deleted',fields,
  aiosources.catalog_origin(origin,provider_id,source_id),aiosources.catalog_origin(origin,provider_id,source_id) FROM aiosources.torrents;
 RETURN NULL;
END;
$$;

DO $$ BEGIN
 IF NOT (SELECT enabled FROM aiosources.catalog_tracking WHERE singleton) THEN
  PERFORM aiosources.clear_catalog_tracking();
 END IF;
END; $$;
`
