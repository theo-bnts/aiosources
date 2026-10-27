package store

const booksSchema = `
ALTER TABLE ingest.aio_works DROP CONSTRAINT aio_works_media_type_check;
ALTER TABLE ingest.aio_works ADD CONSTRAINT aio_works_media_type_check CHECK(media_type IN ('movie','tv','book'));
ALTER TABLE ingest.aio_works ALTER COLUMN tmdb_id DROP NOT NULL;
ALTER TABLE ingest.aio_works ADD COLUMN openlibrary_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.aio_works ADD COLUMN poster_path TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.aio_works ADD COLUMN book_data JSONB NOT NULL DEFAULT '{}';
ALTER TABLE ingest.aio_works ADD COLUMN book_normalized TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.aio_works ADD CONSTRAINT aio_works_identifier_namespace CHECK(
 (media_type='book' AND tmdb_id IS NULL AND openlibrary_id ~ '^OL[1-9][0-9]{0,14}[WM]$')
 OR (media_type IN ('movie','tv') AND tmdb_id IS NOT NULL AND openlibrary_id='')
);
CREATE INDEX aio_works_book_titles_exact ON ingest.aio_works(book_normalized) WHERE media_type='book';
CREATE INDEX aio_works_book_titles_fuzzy ON ingest.aio_works USING gin(book_normalized public.gin_trgm_ops) WHERE media_type='book';

CREATE INDEX aio_works_video_entries ON ingest.aio_works(id) WHERE media_type IN ('movie','tv');
CREATE UNIQUE INDEX aio_works_openlibrary ON ingest.aio_works(openlibrary_id) WHERE openlibrary_id<>'';
CREATE TABLE ingest.ol_authors(id TEXT PRIMARY KEY,name TEXT NOT NULL,aliases TEXT[] NOT NULL DEFAULT '{}',normalized TEXT NOT NULL DEFAULT '');
CREATE INDEX ol_authors_names ON ingest.ol_authors USING gin(normalized public.gin_trgm_ops);
CREATE INDEX aio_works_book_authors ON ingest.aio_works USING gin((book_data->'author_keys'));
CREATE TABLE ingest.ol_redirects(source TEXT PRIMARY KEY,target TEXT NOT NULL CHECK(source<>target));
CREATE INDEX ol_redirects_target ON ingest.ol_redirects(target);
CREATE FUNCTION ingest.ol_resolve(key TEXT) RETURNS TEXT LANGUAGE sql STABLE STRICT PARALLEL SAFE AS $$
 WITH RECURSIVE path(id,seen) AS (SELECT key,ARRAY[key] UNION ALL SELECT r.target,p.seen||r.target FROM path p JOIN ingest.ol_redirects r ON r.source=p.id WHERE cardinality(p.seen)<8 AND NOT r.target=ANY(p.seen))
 SELECT id FROM path p WHERE NOT EXISTS(SELECT 1 FROM ingest.ol_redirects r WHERE r.source=p.id) LIMIT 1;
$$;
CREATE FUNCTION ingest.ol_aliases(key TEXT) RETURNS TEXT[] LANGUAGE sql STABLE STRICT PARALLEL SAFE AS $$
 WITH RECURSIVE aliases(id,seen) AS (SELECT key,ARRAY[key] UNION ALL SELECT r.source,a.seen||r.source FROM aliases a JOIN ingest.ol_redirects r ON r.target=a.id WHERE cardinality(a.seen)<8 AND NOT r.source=ANY(a.seen))
 SELECT ARRAY(SELECT DISTINCT id FROM aliases LIMIT 1000);
$$;
CREATE TABLE ingest.ol_editions(
 id TEXT PRIMARY KEY,work_keys TEXT[] NOT NULL,title TEXT NOT NULL,normalized TEXT NOT NULL,
 isbn TEXT[] NOT NULL DEFAULT '{}',author_keys TEXT[] NOT NULL DEFAULT '{}',languages TEXT[] NOT NULL DEFAULT '{}',
 year INT NOT NULL DEFAULT 0,format TEXT NOT NULL DEFAULT '',cover BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX ol_editions_works ON ingest.ol_editions USING gin(work_keys);
CREATE INDEX ol_editions_isbn ON ingest.ol_editions USING gin(isbn);
CREATE INDEX ol_editions_exact ON ingest.ol_editions(normalized);
CREATE INDEX ol_editions_fuzzy ON ingest.ol_editions USING gin(normalized public.gin_trgm_ops);
CREATE TABLE ingest.ol_import_state(
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),snapshot TEXT NOT NULL DEFAULT '',
 etag TEXT NOT NULL DEFAULT '',position BIGINT NOT NULL DEFAULT 0,imported BIGINT NOT NULL DEFAULT 0,
 skipped BIGINT NOT NULL DEFAULT 0,works BIGINT NOT NULL DEFAULT 0,editions BIGINT NOT NULL DEFAULT 0,authors BIGINT NOT NULL DEFAULT 0,
 checked_at TIMESTAMPTZ,complete BOOLEAN NOT NULL DEFAULT FALSE,retry_at TIMESTAMPTZ,last_sync TIMESTAMPTZ,error TEXT NOT NULL DEFAULT ''
);
INSERT INTO ingest.ol_import_state(singleton) VALUES(TRUE);
ALTER TABLE ingest.aio_match_reviews ADD COLUMN openlibrary_id TEXT NOT NULL DEFAULT '';
ALTER TABLE ingest.aio_match_reviews DROP CONSTRAINT IF EXISTS aio_match_reviews_media_type_check;
ALTER TABLE ingest.aio_match_reviews ADD CONSTRAINT aio_match_reviews_media_type_check CHECK(media_type IN ('','movie','tv','book'));
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_service_check;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_service_check CHECK(service IN ('tmdb','openlibrary','local','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'));
ALTER TABLE ingest.operation_runs DROP CONSTRAINT operation_runs_kind_service_check;
ALTER TABLE ingest.operation_runs ADD CONSTRAINT operation_runs_kind_service_check CHECK((kind='metadata' AND service IN ('tmdb','openlibrary')) OR (kind='matching' AND service='local') OR (kind IN ('cache','download') AND service IN ('alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox')) OR (kind='cleanup' AND service='alldebrid'));
ALTER TABLE ingest.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE ingest.activity_logs ADD CONSTRAINT activity_logs_service_check CHECK(service IN ('','tmdb','openlibrary','network','alldebrid','debridlink','deepbrid','premiumize','realdebrid','torbox'));
ALTER FUNCTION ingest.aio_review_evidence(JSONB) RENAME TO aio_video_review_evidence;
CREATE FUNCTION ingest.aio_review_evidence(fields JSONB) RETURNS JSONB LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT ingest.aio_video_review_evidence(fields) || CASE WHEN
 fields ?| ARRAY['isbn','isbn_10','isbn_13','book_title','book_format','openlibrary','openlibrary_id']
 OR COALESCE(fields->>'media_type',fields->>'type','') IN ('book','ebook','audiobook')
 OR COALESCE(fields->'categories',fields->'category','null')::text ~ '\m(70[0-9]{2}|3030)\M'
 OR fields->>'title' ~* '\m(epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|audiobook|ebook|livre audio)\M'
 OR COALESCE(fields->'attributes','{}') ?| ARRAY['isbn','isbn10','isbn13','isbn_10','isbn_13','booktitle','book_title','openlibrary','openlibrary_id']
 OR COALESCE(fields->'external_ids','{}') ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 THEN jsonb_strip_nulls(jsonb_build_object(
  'isbn',fields->'isbn','isbn_10',fields->'isbn_10','isbn_13',fields->'isbn_13',
  'author',fields->'author','authors',fields->'authors','book_title',fields->'book_title','book_format',fields->'book_format',
  'openlibrary_id',fields->'openlibrary_id','openlibrary',fields->'openlibrary',
  'book_attributes',NULLIF(jsonb_strip_nulls(jsonb_build_object(
   'isbn',fields#>'{attributes,isbn}','isbn10',fields#>'{attributes,isbn10}','isbn13',fields#>'{attributes,isbn13}',
   'isbn_10',fields#>'{attributes,isbn_10}','isbn_13',fields#>'{attributes,isbn_13}',
   'author',fields#>'{attributes,author}','authors',fields#>'{attributes,authors}',
   'booktitle',fields#>'{attributes,booktitle}','book_title',fields#>'{attributes,book_title}',
   'openlibrary',fields#>'{attributes,openlibrary}','openlibrary_id',fields#>'{attributes,openlibrary_id}',
   'book_format',fields#>'{attributes,book_format}')),'{}'::jsonb),
  'book_identifiers',NULLIF(jsonb_strip_nulls(jsonb_build_object(
   'isbn',fields#>'{external_ids,isbn}','isbn_10',fields#>'{external_ids,isbn_10}','isbn_13',fields#>'{external_ids,isbn_13}',
   'openlibrary',fields#>'{external_ids,openlibrary}','openlibrary_id',fields#>'{external_ids,openlibrary_id}')),'{}'::jsonb)
 )) ELSE '{}'::jsonb END;
$$;
ALTER TABLE ingest.shares DROP CONSTRAINT shares_media_types_check;
ALTER TABLE ingest.shares ADD CONSTRAINT shares_media_types_check CHECK(media_types <@ ARRAY['movie','tv','book','ebook','audiobook','other']::text[]);
CREATE OR REPLACE FUNCTION ingest.catalog_entity_changed() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE entity TEXT; label TEXT;
BEGIN
 IF TG_TABLE_NAME='aio_matches' THEN
  entity:='metadata';
  IF NEW.status<>'matched' OR NEW.work_id IS NULL THEN RETURN NULL; END IF;
  SELECT media_type||':'||tmdb_id INTO label FROM ingest.aio_works WHERE id=NEW.work_id AND catalog_import_source='' AND media_type<>'book';
  IF label IS NULL THEN RETURN NULL; END IF;
 ELSE
  IF NEW.media_type='book' THEN RETURN NULL; END IF;
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
INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id)
SELECT media_type,tmdb_id FROM ingest.aio_works w WHERE media_type IN ('movie','tv') AND catalog_import_source='' AND poster_path='' AND EXISTS(SELECT 1 FROM ingest.aio_matches m WHERE m.work_id=w.id AND m.status='matched') ON CONFLICT DO NOTHING;
`
