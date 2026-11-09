package store

// These keys only discover records to reconsider. The ordinary book parser
// still validates checksums, conflicting assertions and bibliographic evidence.
const nativeBookAssertionPredicate = `fields ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->'attributes' ?| ARRAY['isbn','isbn10','isbn13','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->'external_ids' ?| ARRAY['isbn','isbn_10','isbn_13','openlibrary','openlibrary_id']
 OR fields->>'title' ~* '[0-9][0-9 -]{8,}[0-9X]'`
