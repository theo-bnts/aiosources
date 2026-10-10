package store

import (
	"context"
)

// ImportLegacy copies normalized public.torrents rows without touching the
// legacy table or pretending that its original HTTP bytes can be reconstructed.
// The ledger also prevents a later import from resurrecting rows removed by a
// completed full traversal.
func (s *Store) ImportLegacy(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, databaseError("begin historical import", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", migrationLockNamespace, int32(2)); err != nil {
		return 0, databaseError("lock historical import", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('public.torrents') IS NOT NULL").Scan(&exists); err != nil {
		return 0, databaseError("inspect historical table", err)
	}
	if !exists {
		return 0, nil
	}
	// Serializing writes for the short bulk-copy transaction prevents import
	// from interleaving with a full publication. Readers remain unblocked.
	if _, err := tx.Exec(ctx, "LOCK TABLE ingest.torrents IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return 0, databaseError("lock historical destination", err)
	}
	var imported int64
	err = tx.QueryRow(ctx, `WITH legacy AS MATERIALIZED (
    SELECT to_jsonb(source) AS fields FROM public.torrents AS source
), candidates AS MATERIALIZED (
    SELECT fields->>'provider' AS provider_id,fields->>'id' AS source_id,
        fields || jsonb_strip_nulls(jsonb_build_object(
            'title',COALESCE(NULLIF(fields->'title','null'::jsonb),NULLIF(fields->'name','null'::jsonb)),
            'size',COALESCE(NULLIF(fields->'size','null'::jsonb),NULLIF(fields->'file_size_bytes','null'::jsonb)),
            'info_hash',COALESCE(NULLIF(fields->'info_hash','null'::jsonb),NULLIF(fields->'hash','null'::jsonb)),
            'published_at',COALESCE(NULLIF(fields->'published_at','null'::jsonb),NULLIF(fields->'timestamp','null'::jsonb),NULLIF(fields->'pub_date','null'::jsonb),NULLIF(fields->'upload_date','null'::jsonb),NULLIF(fields->'created_at','null'::jsonb)),
            'categories',COALESCE(NULLIF(fields->'categories','null'::jsonb),(
                SELECT jsonb_agg(value ORDER BY ordinal)
                FROM (VALUES (fields->'category_id',1),(fields->'subcategory_id',2)) AS categories(value,ordinal)
                WHERE value IS NOT NULL AND value <> 'null'::jsonb
            ))
        )) AS fields
    FROM legacy
    WHERE NULLIF(fields->>'provider','') IS NOT NULL AND NULLIF(fields->>'id','') IS NOT NULL
      AND NOT EXISTS(SELECT 1 FROM ingest.legacy_imports AS prior
          WHERE prior.provider_id=legacy.fields->>'provider' AND prior.source_id=legacy.fields->>'id')
), inserted AS (
    INSERT INTO ingest.torrents(provider_id,source_id,fields,raw_id,historical)
    SELECT provider_id,source_id,fields,NULL,TRUE FROM candidates
    ON CONFLICT(provider_id,source_id) DO NOTHING RETURNING provider_id,source_id
), recorded AS (
    INSERT INTO ingest.legacy_imports(provider_id,source_id)
    SELECT provider_id,source_id FROM candidates
    ON CONFLICT(provider_id,source_id) DO NOTHING RETURNING provider_id
)
SELECT count(*) FROM inserted`).Scan(&imported)
	if err != nil {
		return 0, databaseError("import historical normalized rows", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, databaseError("commit historical import", err)
	}
	return imported, nil
}
