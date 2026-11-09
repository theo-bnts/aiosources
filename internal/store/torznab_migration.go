package store

// Torznab uses the same revocable grants and admission leases as catalog
// sharing, but reads the current publication directly. Enabling it must never
// start retaining catalog synchronization snapshots.
const torznabSchema = `
ALTER TABLE aiosources.shares
 ADD COLUMN format TEXT NOT NULL DEFAULT 'catalog' CHECK(format IN ('catalog','torznab')),
 ADD COLUMN media_types TEXT[] NOT NULL DEFAULT '{}' CHECK(media_types <@ ARRAY['movie','tv','other']::text[]),
 ADD CONSTRAINT shares_catalog_media CHECK(format<>'catalog' OR cardinality(media_types)=0);

CREATE OR REPLACE FUNCTION aiosources.sync_catalog_tracking() RETURNS VOID LANGUAGE plpgsql AS $$
DECLARE active BOOLEAN; previous BOOLEAN;
BEGIN
 PERFORM pg_advisory_xact_lock(1768843109,3);
 SELECT EXISTS(SELECT 1 FROM aiosources.shares WHERE format='catalog' AND enabled AND (expires_at IS NULL OR expires_at>clock_timestamp())) INTO active;
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

ALTER FUNCTION aiosources.access_activity_message(TEXT) RENAME TO access_activity_message_before_torznab;
CREATE FUNCTION aiosources.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(aiosources.access_activity_message_before_torznab(kind), CASE kind
 WHEN 'torznab.created' THEN 'Torznab access created'
 WHEN 'torznab.permissions_changed' THEN 'Torznab permissions changed'
 WHEN 'torznab.rotated' THEN 'Torznab API key replaced'
 WHEN 'torznab.revoked' THEN 'Torznab access revoked'
 WHEN 'torznab.searched' THEN 'Torznab catalog search completed'
 WHEN 'torznab.failed' THEN 'Torznab catalog request failed'
 ELSE NULL END);
$$;
`
