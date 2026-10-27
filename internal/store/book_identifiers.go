package store

// These keys only discover records to reconsider. The ordinary book parser
// still validates checksums, conflicting assertions and bibliographic evidence.
const nativeBookAssertionPredicate = `fields ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->'attributes' ?| ARRAY['isbn','isbn10','isbn13','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->'external_ids' ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->>'title' ~* '[0-9][0-9 -]{8,}[0-9X]'`

const nativeBookIdentifiersSchema = `
CREATE FUNCTION ingest.aio_native_book_candidates(fields JSONB) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 WITH RECURSIVE objects(value) AS (
  SELECT fields WHERE jsonb_typeof(fields)='object'
  UNION ALL SELECT fields->'attributes' WHERE jsonb_typeof(fields->'attributes')='object'
  UNION ALL SELECT fields->'external_ids' WHERE jsonb_typeof(fields->'external_ids')='object'
 ), assertions(value) AS (
  SELECT member.value FROM objects CROSS JOIN LATERAL jsonb_each(objects.value) member
  WHERE member.key IN ('isbn','isbn10','isbn13','isbn_10','isbn_13','openlibrary','openlibrary_id')
  UNION ALL SELECT member.value FROM assertions CROSS JOIN LATERAL jsonb_array_elements(
   CASE WHEN jsonb_typeof(assertions.value)='array' THEN assertions.value ELSE '[]'::jsonb END) member(value)
 ), raw(value) AS (
  SELECT upper(btrim(value#>>'{}')) FROM assertions WHERE jsonb_typeof(value) IN ('string','number')
  UNION ALL SELECT (m)[1] FROM regexp_matches(upper(COALESCE(fields->>'title','')),'\m((?:97[89][- ]?)?[0-9](?:[- ]?[0-9]){8}[- ]?[0-9X])\M','g') m
 ), normalized(value) AS (
  SELECT regexp_replace(value,'[- ]','','g') FROM raw
 ), keys(value) AS (
  SELECT 'isbn:'||left(value,12) FROM normalized WHERE value ~ '^97[89][0-9]{10}$'
  UNION SELECT 'isbn:978'||left(value,9) FROM normalized WHERE value ~ '^[0-9]{9}[0-9X]$'
  UNION SELECT 'ol:'||regexp_replace(value,'^.*/','') FROM raw WHERE regexp_replace(value,'^.*/','') ~ '^OL[1-9][0-9]{0,14}[WM]$'
 ) SELECT COALESCE(array_agg(DISTINCT value),ARRAY[]::text[]) FROM keys;
$$;
CREATE INDEX aio_torrents_native_books ON ingest.torrents USING gin((ingest.aio_native_book_candidates(fields)))
WHERE ` + nativeBookAssertionPredicate + `;
CREATE INDEX ol_editions_authors ON ingest.ol_editions USING gin(author_keys);
`
