package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Keep source attribution aligned with the Storage page. Shared metadata and
// provider caches belong to their own categories, not to an individual source.
const sourceStorageRelations = `(c.relname IN ('torrents','staged_torrents','raw_records','pages','events','runs','collection_state','legacy_imports','schedule_clocks','torrent_history','torrent_history_baselines','torrent_history_metadata') OR c.relname LIKE 'run_%')`

// Estimate ownership of allocated tables, TOAST and indexes from PostgreSQL's
// ANALYZE distributions. This reads catalog statistics, never source records or
// response bodies. Sparse/unanalysed relations have no attributable estimate.
func sourceStorageEstimates(ctx context.Context, tx pgx.Tx) ([]model.SourceStorageEstimate, error) {
	rows, err := tx.Query(ctx, `WITH relations AS (
 SELECT c.oid,c.relname,a.attname FROM pg_class c
 JOIN pg_namespace n ON n.oid=c.relnamespace
 CROSS JOIN LATERAL (
  SELECT attname FROM pg_attribute WHERE attrelid=c.oid AND attnum>0 AND NOT attisdropped
  AND attname IN ('provider_id','run_id') ORDER BY (attname='provider_id') DESC LIMIT 1
 ) a WHERE n.nspname='ingest' AND c.relkind='r' AND `+sourceStorageRelations+`
), distributions AS (
 SELECT rel.oid,rel.attname,d.value,d.frequency FROM relations rel
 JOIN pg_stats s ON s.schemaname='ingest' AND s.tablename=rel.relname AND s.attname=rel.attname AND NOT s.inherited
 CROSS JOIN LATERAL (
  SELECT value,frequency::double precision FROM unnest(s.most_common_vals::text::text[],s.most_common_freqs) common(value,frequency)
  UNION ALL
  SELECT value,(1-COALESCE((SELECT sum(f) FROM unnest(s.most_common_freqs) f),0)-s.null_frac)
   /NULLIF(cardinality(s.histogram_bounds::text::text[]),0)
  FROM unnest(s.histogram_bounds::text::text[]) histogram(value)
 ) d
), attributed AS (
 SELECT CASE WHEN d.attname='provider_id' THEN d.value ELSE r.provider_id END AS provider_id,
 pg_total_relation_size(d.oid)*GREATEST(d.frequency,0) AS bytes
 FROM distributions d LEFT JOIN ingest.runs r ON d.attname='run_id' AND r.id=d.value
)
SELECT provider_id,sum(bytes)::bigint FROM attributed
WHERE provider_id IS NOT NULL AND provider_id<>'' GROUP BY provider_id ORDER BY provider_id`)
	if err != nil {
		return nil, databaseError("estimate source storage", err)
	}
	defer rows.Close()
	items := []model.SourceStorageEstimate{}
	for rows.Next() {
		var item model.SourceStorageEstimate
		if err := rows.Scan(&item.ProviderID, &item.Bytes); err != nil {
			return nil, databaseError("read source storage estimate", err)
		}
		items = append(items, item)
	}
	return items, databaseError("estimate source storage", rows.Err())
}
