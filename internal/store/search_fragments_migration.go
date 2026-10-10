package store

// Keep the same bounded, public search document for whole-word phrases and
// fragments. The expression index avoids rewriting every published row or
// storing a second copy of its text when upgrading an existing catalogue.
const searchFragmentsSchema = `
CREATE FUNCTION ingest.torrent_search_text(provider TEXT, source TEXT, fields JSONB) RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE encoded BYTEA;
BEGIN
    encoded := substring(convert_to(left(provider,128) || ' ' || left(source,4096) || ' ' ||
        left(COALESCE(fields->>'info_hash',''),64) || ' ' ||
        CASE WHEN jsonb_typeof(fields->'title')='string' THEN left(fields->>'title',131072) ELSE '' END,'UTF8') FROM 1 FOR 131072);
    LOOP
        BEGIN
            RETURN convert_from(encoded,'UTF8');
        EXCEPTION WHEN character_not_in_repertoire THEN
            encoded := substring(encoded FROM 1 FOR octet_length(encoded)-1);
        END;
    END LOOP;
END;
$$;
CREATE OR REPLACE FUNCTION ingest.torrent_search_vector(provider TEXT, source TEXT, fields JSONB) RETURNS TSVECTOR
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT to_tsvector('simple'::regconfig,ingest.torrent_search_text(provider,source,fields));
$$;
CREATE INDEX torrents_fragments ON ingest.torrents USING GIN
    (ingest.torrent_search_text(provider_id,source_id,fields) public.gin_trgm_ops);
`
