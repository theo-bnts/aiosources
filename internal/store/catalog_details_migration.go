package store

// A transaction watermark on the existing row avoids duplicating full source
// payloads in the journal. The constant default needs no payload backfill.
const catalogDetailsSchema = `
ALTER TABLE ingest.torrents ADD COLUMN catalog_details_tx BIGINT NOT NULL DEFAULT 0;
CREATE INDEX torrents_catalog_details_cursor ON ingest.torrents(catalog_details_tx,occurrence_id);
CREATE FUNCTION ingest.catalog_details_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR OLD.fields IS DISTINCT FROM NEW.fields OR OLD.origin IS DISTINCT FROM NEW.origin THEN
  NEW.catalog_details_tx:=pg_current_xact_id()::text::bigint;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER catalog_details_changed BEFORE INSERT OR UPDATE OF fields,origin ON ingest.torrents
FOR EACH ROW EXECUTE FUNCTION ingest.catalog_details_changed();
ALTER TABLE ingest.run_catalog_entities DROP CONSTRAINT run_catalog_entities_kind_check;
ALTER TABLE ingest.run_catalog_entities ADD CHECK(kind IN ('metadata','availability','torrent'));
`
