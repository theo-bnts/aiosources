package store

// Historical runs retain their original projection. New work snapshots each
// optional choice at admission, independently of later administrator edits.
const storageControlsSchema = `
ALTER TABLE aiosources.storage_settings ADD COLUMN options JSONB NOT NULL DEFAULT
 '{"all_fields":false,"additional_fields":[],"successful_records":false,"record_fields":false,"publication_history":true}'::jsonb
 CHECK(jsonb_typeof(options)='object');
UPDATE aiosources.storage_settings SET options=jsonb_build_object(
 'all_fields',mode='full','additional_fields','[]'::jsonb,'successful_records',mode='full','record_fields',false,'publication_history',true);
ALTER TABLE aiosources.runs ADD COLUMN storage_options JSONB CHECK(jsonb_typeof(storage_options)='object');
CREATE TABLE aiosources.storage_index_settings (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 values JSONB NOT NULL CHECK(jsonb_typeof(values)='object'),
 errors JSONB NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO aiosources.storage_index_settings(singleton,values) VALUES(TRUE,
 '{"torrents_fulltext":true,"torrents_fragments":true,"torrents_categories":true,"torrents_size":true,"torrents_published_at":true,"raw_records_fragments":true}');
CREATE TABLE aiosources.storage_retention_settings (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 days JSONB NOT NULL CHECK(jsonb_typeof(days)='object'),
 request_limit INT NOT NULL DEFAULT 10000 CHECK(request_limit BETWEEN 0 AND 1000000),
 request_results BOOLEAN NOT NULL DEFAULT TRUE
);
INSERT INTO aiosources.storage_retention_settings(singleton,days) VALUES(TRUE,
 '{"diagnostics":-1,"changes":-1,"logs":-1,"operations":-1,"requests":-1,"sharing":-1}');

-- Preserve the current trigger implementations and their reset/immutability
-- guards. Only the entry condition changes; sharing keeps its own independent
-- synchronization journal and is unaffected by diagnostic history choices.
DO $selection$
DECLARE name TEXT; definition TEXT;
BEGIN
 FOREACH name IN ARRAY ARRAY['torrent_history_record','torrent_history_truncate'] LOOP
  SELECT pg_get_functiondef(p.oid) INTO definition FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname='aiosources' AND p.proname=name AND p.pronargs=0;
  IF definition IS NULL THEN RAISE EXCEPTION 'Missing publication history trigger'; END IF;
  definition:=replace(definition,E'BEGIN\n',E'BEGIN\n IF NOT (SELECT (options->>''publication_history'')::boolean FROM aiosources.storage_settings WHERE singleton) THEN RETURN NULL; END IF;\n');
  EXECUTE definition;
 END LOOP;
END;
$selection$;
`
