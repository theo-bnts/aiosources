package store

// Remove non-durable counts from interpreted records and their snapshots. Raw
// response archives retain their original bytes and integrity references.
const leecherRetirementSchema = `
CREATE OR REPLACE FUNCTION ingest.without_swarm_counts(value JSONB) RETURNS JSONB
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
 IF NOT (value @? '$.**.peers' OR value @? '$.**.seeders' OR value @? '$.**.leechers') THEN RETURN value; END IF;
 CASE jsonb_typeof(value)
 WHEN 'object' THEN
  RETURN COALESCE((SELECT jsonb_object_agg(key,ingest.without_swarm_counts(item))
    FROM jsonb_each(value) AS entry(key,item) WHERE key NOT IN ('seeders','peers','leechers')),'{}'::jsonb);
 WHEN 'array' THEN
  RETURN COALESCE((SELECT jsonb_agg(ingest.without_swarm_counts(item) ORDER BY ordinal)
    FROM jsonb_array_elements(value) WITH ORDINALITY AS entry(item,ordinal)),'[]'::jsonb);
 ELSE RETURN value;
 END CASE;
END;
$$;

LOCK TABLE ingest.torrents IN SHARE ROW EXCLUSIVE MODE;
-- Only adjust matches that are current for the pre-migration record. Stale
-- matches and the existing queue must retain their freshness/retry semantics.
UPDATE ingest.aio_matches AS match SET fingerprint=md5(ingest.without_swarm_counts(torrent.fields)::text)
FROM ingest.torrents AS torrent
WHERE match.provider_id=torrent.provider_id AND match.source_id=torrent.source_id
 AND match.fingerprint=md5(torrent.fields::text) AND torrent.fields @? '$.**.leechers';

UPDATE ingest.aio_match_reviews AS review SET fingerprint=md5(ingest.without_swarm_counts(torrent.fields)::text)
FROM ingest.torrents AS torrent WHERE review.provider_id=torrent.provider_id AND review.source_id=torrent.source_id
 AND review.fingerprint=md5(torrent.fields::text) AND torrent.fields @? '$.**.leechers';

DO $$
DECLARE source TEXT; column_name TEXT; saved JSONB; guard RECORD;
BEGIN
 FOREACH source IN ARRAY ARRAY['torrents','staged_torrents','raw_records','catalog_journal','catalog_baselines','torrent_history_baselines','torrent_history'] LOOP
  -- Snapshot exact trigger modes, including intentionally disabled triggers.
  -- Avoid false publications, match requests or history events during cleanup.
  SELECT COALESCE(jsonb_agg(jsonb_build_object('name',tgname,'mode',tgenabled::text)),'[]'::jsonb)
   INTO saved FROM pg_trigger WHERE tgrelid=('ingest.'||source)::regclass AND NOT tgisinternal AND tgenabled<>'D';
  FOR guard IN SELECT * FROM jsonb_to_recordset(saved) AS item(name TEXT,mode TEXT) LOOP
   EXECUTE format('ALTER TABLE ingest.%I DISABLE TRIGGER %I',source,guard.name);
  END LOOP;
  FOREACH column_name IN ARRAY CASE WHEN source='torrent_history' THEN ARRAY['before_fields','after_fields'] ELSE ARRAY['fields'] END LOOP
   EXECUTE format('UPDATE ingest.%I SET %I=ingest.without_swarm_counts(%I) WHERE %I @? %L',source,column_name,column_name,column_name,'$.**.leechers');
  END LOOP;
  FOR guard IN SELECT * FROM jsonb_to_recordset(saved) AS item(name TEXT,mode TEXT) LOOP
   EXECUTE format('ALTER TABLE ingest.%I %s TRIGGER %I',source,
    CASE guard.mode WHEN 'R' THEN 'ENABLE REPLICA' WHEN 'A' THEN 'ENABLE ALWAYS' ELSE 'ENABLE' END,guard.name);
  END LOOP;
 END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION ingest.retire_seeder_fields() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 NEW.fields=ingest.without_swarm_counts(NEW.fields); RETURN NEW;
END; $$;

DO $$ DECLARE constraint_name TEXT; BEGIN
 FOR constraint_name IN SELECT conname FROM pg_constraint
  WHERE conrelid='ingest.shares'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%cardinality(fields)%' LOOP
  EXECUTE format('ALTER TABLE ingest.shares DROP CONSTRAINT %I',constraint_name);
 END LOOP;
END; $$;
UPDATE ingest.shares SET fields=array_remove(fields,'leechers'),
 enabled=enabled AND cardinality(array_remove(fields,'leechers'))>0,revision=revision+1 WHERE 'leechers'=ANY(fields);
ALTER TABLE ingest.shares ADD CONSTRAINT shares_fields_allowed CHECK (
 (cardinality(fields)>0 OR NOT enabled)
 AND fields <@ ARRAY['title','size','info_hash','published_at','category','categories']::text[]);
`
