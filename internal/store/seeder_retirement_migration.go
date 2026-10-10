package store

// Remove non-durable counts from interpreted records and their snapshots. Raw
// response archives retain their original bytes and integrity references.
const seederRetirementSchema = `
CREATE FUNCTION ingest.without_seeders(value JSONB) RETURNS JSONB
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
 IF NOT value @? '$.**.seeders' THEN RETURN value; END IF;
 CASE jsonb_typeof(value)
 WHEN 'object' THEN
  RETURN COALESCE((SELECT jsonb_object_agg(key,ingest.without_seeders(item))
    FROM jsonb_each(value) AS entry(key,item) WHERE key<>'seeders'),'{}'::jsonb);
 WHEN 'array' THEN
  RETURN COALESCE((SELECT jsonb_agg(ingest.without_seeders(item) ORDER BY ordinal)
    FROM jsonb_array_elements(value) WITH ORDINALITY AS entry(item,ordinal)),'[]'::jsonb);
 ELSE RETURN value;
 END CASE;
END;
$$;

LOCK TABLE ingest.torrents IN SHARE ROW EXCLUSIVE MODE;
-- Only adjust matches that are current for the pre-migration record. Stale
-- matches and the existing queue must retain their freshness/retry semantics.
UPDATE ingest.aio_matches AS match SET fingerprint=md5(ingest.without_seeders(torrent.fields)::text)
FROM ingest.torrents AS torrent
WHERE match.provider_id=torrent.provider_id AND match.source_id=torrent.source_id
 AND match.fingerprint=md5(torrent.fields::text) AND torrent.fields @? '$.**.seeders';

DO $$
DECLARE source TEXT; column_name TEXT; saved JSONB; guard RECORD;
BEGIN
 FOREACH source IN ARRAY ARRAY['torrents','staged_torrents','raw_records','catalog_journal','torrent_history_baselines','torrent_history'] LOOP
  -- Snapshot exact trigger modes, including intentionally disabled triggers.
  -- Avoid false publications, match requests or history events during cleanup.
  SELECT COALESCE(jsonb_agg(jsonb_build_object('name',tgname,'mode',tgenabled::text)),'[]'::jsonb)
   INTO saved FROM pg_trigger WHERE tgrelid=('ingest.'||source)::regclass AND NOT tgisinternal AND tgenabled<>'D';
  FOR guard IN SELECT * FROM jsonb_to_recordset(saved) AS item(name TEXT,mode TEXT) LOOP
   EXECUTE format('ALTER TABLE ingest.%I DISABLE TRIGGER %I',source,guard.name);
  END LOOP;
  FOREACH column_name IN ARRAY CASE WHEN source='torrent_history' THEN ARRAY['before_fields','after_fields'] ELSE ARRAY['fields'] END LOOP
   EXECUTE format('UPDATE ingest.%I SET %I=ingest.without_seeders(%I) WHERE %I @? %L',source,column_name,column_name,column_name,'$.**.seeders');
  END LOOP;
  FOR guard IN SELECT * FROM jsonb_to_recordset(saved) AS item(name TEXT,mode TEXT) LOOP
   EXECUTE format('ALTER TABLE ingest.%I %s TRIGGER %I',source,
    CASE guard.mode WHEN 'R' THEN 'ENABLE REPLICA' WHEN 'A' THEN 'ENABLE ALWAYS' ELSE 'ENABLE' END,guard.name);
  END LOOP;
 END LOOP;
END;
$$;

ALTER TABLE ingest.torrents DROP COLUMN search_seeders;
UPDATE ingest.saved_views SET filters=(filters-'min_seeders'-'max_seeders')
 || CASE WHEN filters->>'sort'='seeders_desc' THEN jsonb_build_object('sort','recent') ELSE '{}'::jsonb END,
 revision=revision+1 WHERE filters ?| ARRAY['min_seeders','max_seeders'] OR filters->>'sort'='seeders_desc';

DO $$ DECLARE constraint_name TEXT; BEGIN
 FOR constraint_name IN SELECT conname FROM pg_constraint
  WHERE conrelid='ingest.shares'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%cardinality(fields)%' LOOP
  EXECUTE format('ALTER TABLE ingest.shares DROP CONSTRAINT %I',constraint_name);
 END LOOP;
END; $$;
UPDATE ingest.shares SET fields=array_remove(fields,'seeders'),
 enabled=enabled AND cardinality(array_remove(fields,'seeders'))>0,revision=revision+1 WHERE 'seeders'=ANY(fields);
ALTER TABLE ingest.shares ADD CONSTRAINT shares_fields_allowed CHECK (
 (cardinality(fields)>0 OR NOT enabled)
 AND fields <@ ARRAY['title','size','info_hash','leechers','published_at','category','categories']::text[]);

CREATE FUNCTION ingest.retire_seeder_fields() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 NEW.fields=ingest.without_seeders(NEW.fields); RETURN NEW;
END; $$;
-- Defensive storage boundary also covers historical import and restore writers.
CREATE TRIGGER torrents_retire_seeders BEFORE INSERT OR UPDATE OF fields ON ingest.torrents
 FOR EACH ROW EXECUTE FUNCTION ingest.retire_seeder_fields();
CREATE TRIGGER staged_torrents_retire_seeders BEFORE INSERT OR UPDATE OF fields ON ingest.staged_torrents
 FOR EACH ROW EXECUTE FUNCTION ingest.retire_seeder_fields();
CREATE TRIGGER raw_records_retire_seeders BEFORE INSERT OR UPDATE OF fields ON ingest.raw_records
 FOR EACH ROW EXECUTE FUNCTION ingest.retire_seeder_fields();
`
