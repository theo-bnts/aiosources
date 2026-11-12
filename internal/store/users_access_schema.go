package store

// Access changes preserve existing passwords, sessions and explicit feature
// permissions. Obsolete factors and source-visibility state are removed.
const usersAccessSchema = `
ALTER TABLE aiosources.admin_security
 DROP CONSTRAINT admin_security_check,
 DROP COLUMN active_secret, DROP COLUMN pending_secret,
 DROP COLUMN pending_expires_at, DROP COLUMN pending_session_id,
 DROP COLUMN last_step, DROP COLUMN recovery_hashes;
UPDATE aiosources.runtime_settings SET values=values-'enrollment_minutes',revision=revision+1 WHERE section='security' AND values ? 'enrollment_minutes';
DROP TRIGGER public_catalog_access_policy ON aiosources.aio_public_access;
ALTER TABLE aiosources.aio_public_access DROP CONSTRAINT aio_public_catalog_requires_sources;
CREATE OR REPLACE FUNCTION aiosources.catalog_export_active(entity text DEFAULT ''::text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
 SELECT EXISTS(SELECT 1 FROM aiosources.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>now())
 AND (entity='' OR catalog_entities->>entity='true')) OR EXISTS(
 SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.catalog_enabled
 AND (entity='' OR p.catalog_entities->>entity='true')) OR EXISTS(SELECT 1 FROM aiosources.trusted_users u CROSS JOIN aiosources.public_catalog_policy p WHERE u.enabled AND u.permissions->>'catalog_enabled'='true' AND p.singleton AND (entity='' OR p.catalog_entities->>entity='true'));
$$;

CREATE OR REPLACE FUNCTION aiosources.public_catalog_policy_changed() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 IF TG_TABLE_NAME='trusted_users' THEN
  IF TG_OP='UPDATE' THEN
   IF (NEW.enabled,NEW.permissions->>'catalog_enabled') IS NOT DISTINCT FROM (OLD.enabled,OLD.permissions->>'catalog_enabled') THEN RETURN NULL; END IF;
  ELSIF TG_OP='INSERT' THEN
   IF NOT NEW.enabled OR COALESCE(NEW.permissions->>'catalog_enabled','false')<>'true' THEN RETURN NULL; END IF;
  ELSIF TG_OP='DELETE' THEN
   IF NOT OLD.enabled OR COALESCE(OLD.permissions->>'catalog_enabled','false')<>'true' THEN RETURN NULL; END IF;
  END IF;
 END IF;
 IF TG_TABLE_NAME='aio_public_access' THEN
  IF (NEW.enabled,NEW.catalog_enabled) IS NOT DISTINCT FROM (OLD.enabled,OLD.catalog_enabled) THEN RETURN NULL; END IF;
  UPDATE aiosources.public_catalog_policy SET revision=revision+1 WHERE singleton;
 END IF;
 PERFORM aiosources.sync_catalog_tracking();
 PERFORM aiosources.prune_catalog_entity_index();
 IF EXISTS(SELECT 1 FROM aiosources.aio_public_access a CROSS JOIN aiosources.public_catalog_policy p
 WHERE a.singleton AND p.singleton AND a.enabled AND a.catalog_enabled AND p.catalog_entities->>'metadata'='true') OR EXISTS(SELECT 1 FROM aiosources.trusted_users u CROSS JOIN aiosources.public_catalog_policy p WHERE u.enabled AND u.permissions->>'catalog_enabled'='true' AND p.singleton AND p.catalog_entities->>'metadata'='true') THEN
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
ALTER TABLE aiosources.aio_public_access DROP COLUMN expose_sources;
ALTER TABLE aiosources.aio_public_access
 ALTER COLUMN search_enabled SET DEFAULT true,
 ALTER COLUMN stremio_enabled SET DEFAULT true,
 ALTER COLUMN torznab_enabled SET DEFAULT true,
 ALTER COLUMN catalog_enabled SET DEFAULT true;
UPDATE aiosources.trusted_users SET permissions=permissions-'expose_sources' WHERE permissions ? 'expose_sources';
CREATE TRIGGER trusted_catalog_access_policy AFTER INSERT OR UPDATE OF enabled,permissions OR DELETE ON aiosources.trusted_users FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();
CREATE TRIGGER public_catalog_access_policy AFTER UPDATE OF enabled,catalog_enabled ON aiosources.aio_public_access FOR EACH ROW EXECUTE FUNCTION aiosources.public_catalog_policy_changed();
UPDATE aiosources.public_catalog_policy SET revision=revision+1 WHERE singleton AND EXISTS(SELECT 1 FROM aiosources.trusted_users WHERE enabled AND permissions->>'catalog_enabled'='true');
`
