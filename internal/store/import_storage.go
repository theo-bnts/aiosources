package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// ANALYZE distributions attribute allocated preparation/resume-state space to
// runs. The only source-row access is an indexed existence check per run, never
// a count or payload scan. Unknown estimates remain null; they are not zeros.
func importStorageEstimates(ctx context.Context, tx pgx.Tx) ([]model.ImportStorageEstimate, int64, int64, error) {
	rows, err := tx.Query(ctx, `WITH distributions AS (
 SELECT c.oid,d.value,d.frequency FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 JOIN pg_stats s ON s.schemaname=n.nspname AND s.tablename=c.relname AND s.attname='run_id' AND NOT s.inherited
`+storageDistributionSQL+` WHERE n.nspname='ingest' AND c.relkind='r' AND (c.relname='staged_torrents' OR c.relname LIKE 'run_%')
), sizes AS (
 SELECT value AS run_id,sum(pg_total_relation_size(oid)*GREATEST(frequency,0))::bigint AS bytes
 FROM distributions GROUP BY value
), retained AS (
 SELECT r.id,r.provider_id,r.provider_name,r.status,r.created_at,s.bytes
 FROM ingest.runs r LEFT JOIN sizes s ON s.run_id=r.id
 WHERE r.status<>'succeeded' AND EXISTS(SELECT 1 FROM ingest.staged_torrents t WHERE t.run_id=r.id)
)
SELECT *,count(*) OVER(),count(*) FILTER(WHERE status='cancelled') OVER()
 FROM retained ORDER BY bytes DESC NULLS LAST,created_at,id LIMIT 20`)
	if err != nil {
		return nil, 0, 0, databaseError("estimate retained imports", err)
	}
	defer rows.Close()
	items := []model.ImportStorageEstimate{}
	var total, cancelled int64
	for rows.Next() {
		var item model.ImportStorageEstimate
		if err := rows.Scan(&item.RunID, &item.ProviderID, &item.ProviderName, &item.Status, &item.CreatedAt, &item.Bytes, &total, &cancelled); err != nil {
			return nil, 0, 0, databaseError("read retained import estimate", err)
		}
		items = append(items, item)
	}
	return items, total, cancelled, databaseError("estimate retained imports", rows.Err())
}
