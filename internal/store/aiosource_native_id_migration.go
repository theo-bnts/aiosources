package store

// Native MAL assertions are explicitly unverified. The index discovers all
// supported scalar/array representations, while scanAIORecord remains the
// authority for agreeing assertions and conflict vetoes.
const aioMALAssertionPredicate = `fields ? 'mal_id' OR fields ? 'mal'
OR fields->'external_ids' ?| ARRAY['mal','mal_id']
OR fields->'attributes' ?| ARRAY['mal','malid']`

const aioNativeIDSchema = `
CREATE FUNCTION ingest.aio_native_mal_candidates(fields JSONB) RETURNS TEXT[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    WITH RECURSIVE assertions(value) AS (
        SELECT value FROM (VALUES
            (fields->'mal_id'), (fields->'mal'),
            (fields#>'{external_ids,mal}'), (fields#>'{external_ids,mal_id}'),
            (fields#>'{attributes,mal}'), (fields#>'{attributes,malid}')
        ) AS roots(value)
        UNION ALL
        SELECT member.value FROM assertions
        CROSS JOIN LATERAL jsonb_array_elements(
            CASE WHEN jsonb_typeof(assertions.value)='array' THEN assertions.value ELSE '[]'::jsonb END
        ) AS member(value)
    ), candidates AS (
        SELECT regexp_replace(value #>> '{}','^0+','') AS id
        FROM assertions WHERE jsonb_typeof(value) IN ('string','number')
    )
    SELECT COALESCE(array_agg(DISTINCT id),ARRAY[]::text[])
    FROM candidates WHERE id ~ '^[1-9][0-9]{0,15}$';
$$;
CREATE INDEX aio_torrents_native_mal ON ingest.torrents USING GIN((ingest.aio_native_mal_candidates(fields)))
WHERE ` + aioMALAssertionPredicate + `;
`
