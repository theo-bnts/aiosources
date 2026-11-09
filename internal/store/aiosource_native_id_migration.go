package store

// Native MAL assertions are explicitly unverified. The index discovers all
// supported scalar/array representations, while scanAIORecord remains the
// authority for agreeing assertions and conflict vetoes.
const aioMALAssertionPredicate = `fields ? 'mal_id' OR fields ? 'mal'
OR fields->'external_ids' ?| ARRAY['mal','mal_id']
OR fields->'attributes' ?| ARRAY['mal','malid']`
