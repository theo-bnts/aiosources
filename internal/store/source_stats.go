package store

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

// SourceTorrentStats uses one statement snapshot for publication and matching
// counts. Group by the shared decision predicate once, avoiding a fingerprint
// calculation for every separate counter or a query for every source.
func (s *Store) SourceTorrentStats(ctx context.Context, providers []string) ([]model.SourceTorrentStats, error) {
	result := []model.SourceTorrentStats{}
	if len(providers) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `WITH requested AS (
 SELECT DISTINCT unnest($1::text[]) AS provider_id
), decisions AS (
 SELECT t.provider_id,`+aioDiagnosticStatus+` AS result`+aioDiagnosticSource+`
 WHERE t.provider_id=ANY($1::text[])
), counts AS (
 SELECT provider_id,result,count(*) AS total FROM decisions GROUP BY provider_id,result
), latest_import AS (
 SELECT DISTINCT ON (provider_id) provider_id,id,status FROM ingest.runs
 WHERE provider_id=ANY($1::text[]) AND mode<>'preview'
 AND (mode='full' OR (config->>'adapter'='http_json' AND config->'http'->>'catalog'='true'))
 ORDER BY provider_id,(status IN ('queued','running')) DESC,created_at DESC,id DESC
), progress AS (
 SELECT r.provider_id,r.id,count(*) AS torrents FROM latest_import r
 JOIN ingest.staged_torrents t ON t.run_id=r.id
 WHERE r.status IN ('queued','running','paused','failed') AND NOT t.deleted
 GROUP BY r.provider_id,r.id
)
SELECT requested.provider_id,COALESCE(sum(c.total),0)::bigint,
 COALESCE(sum(c.total) FILTER(WHERE c.result='matched'),0)::bigint,
 COALESCE(sum(c.total) FILTER(WHERE c.result='ambiguous'),0)::bigint,
 COALESCE(sum(c.total) FILTER(WHERE c.result='unmatched'),0)::bigint,
 COALESCE(sum(c.total) FILTER(WHERE c.result='pending'),0)::bigint,
 p.id,COALESCE(p.torrents,0)
FROM requested LEFT JOIN counts c USING(provider_id) LEFT JOIN progress p USING(provider_id)
GROUP BY requested.provider_id,p.id,p.torrents ORDER BY requested.provider_id`, providers)
	if err != nil {
		return nil, databaseError("count source torrents", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item model.SourceTorrentStats
		var runID *string
		var staged int64
		if err := rows.Scan(&item.ProviderID, &item.Imported, &item.Matched, &item.Ambiguous, &item.Unmatched, &item.Pending, &runID, &staged); err != nil {
			return nil, databaseError("read source torrent counts", err)
		}
		if runID != nil {
			item.PendingImport = &model.SourcePendingImport{RunID: *runID, Torrents: staged}
		}
		result = append(result, item)
	}
	return result, databaseError("count source torrents", rows.Err())
}
