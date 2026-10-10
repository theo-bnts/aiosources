package store

// Index the existing structured observations without duplicating their fields
// in another stored column. Referenced archives keep their original identity.
const rawSearchSchema = `
CREATE FUNCTION ingest.raw_search_text(source TEXT, fields JSONB, problem TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT source || E'\n' || fields::text || E'\n' || problem;
$$;
CREATE INDEX raw_records_fragments ON ingest.raw_records USING GIN
    (ingest.raw_search_text(source_id,fields,error) public.gin_trgm_ops);
`
