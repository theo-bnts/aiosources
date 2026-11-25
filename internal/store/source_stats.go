package store

import (
	"context"
	"slices"

	"github.com/theo-bnts/aiosources/internal/model"
)

// SourceTorrentStats combines shared per-source identification counts with
// live pending-import progress from active/interrupted runs.
// Totals cover the entire catalog, so filtering the displayed sources does not
// change percentages.
func (s *Store) SourceTorrentStats(ctx context.Context, providers []string) (model.SourceTorrentOverview, error) {
	result := model.SourceTorrentOverview{Items: []model.SourceTorrentStats{}}
	if len(providers) == 0 {
		return result, nil
	}

	counts, err := s.decisionCounts(ctx)
	if err != nil {
		return result, err
	}

	// Collect unique requested providers sorted identically to ORDER BY provider_id.
	unique := make([]string, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			unique = append(unique, p)
		}
	}
	slices.Sort(unique)

	pendingByProvider := map[string]*model.SourcePendingImport{}
	rows, err := s.pool.Query(ctx, `WITH latest_import AS (
 SELECT DISTINCT ON (provider_id) provider_id,id,status FROM aiosources.runs
 WHERE mode<>'preview'
 AND (mode='full' OR (config->>'adapter' IN ('json','http_json') AND config->'http'->>'catalog'='true'))
 ORDER BY provider_id,(status IN ('queued','running')) DESC,created_at DESC,id DESC
), progress AS (
 SELECT r.provider_id,r.id,count(*)::bigint AS torrents,
 count(*) FILTER(WHERE EXISTS(SELECT 1 FROM aiosources.torrents published
  WHERE published.provider_id=r.provider_id AND published.source_id=t.source_id))::bigint AS updates FROM latest_import r
 JOIN aiosources.staged_torrents t ON t.run_id=r.id
 WHERE r.status IN ('queued','running','paused','failed') AND NOT t.deleted
 GROUP BY r.provider_id,r.id
)
SELECT provider_id,id,torrents,torrents-updates,updates FROM progress`)
	if err != nil {
		return result, databaseError("count source pending imports", err)
	}
	defer rows.Close()
	for rows.Next() {
		var providerID, runID string
		var torrents, newTorrents, updates int64
		if err := rows.Scan(&providerID, &runID, &torrents, &newTorrents, &updates); err != nil {
			return result, databaseError("read source pending imports", err)
		}
		result.Totals.AwaitingImport += torrents
		pendingByProvider[providerID] = &model.SourcePendingImport{
			RunID:    runID,
			Torrents: torrents,
			New:      newTorrents,
			Updates:  updates,
		}
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("count source pending imports", err)
	}

	result.Totals.Imported = counts.totals.total
	result.Items = make([]model.SourceTorrentStats, 0, len(unique))
	for _, providerID := range unique {
		c := counts.byProvider[providerID]
		result.Items = append(result.Items, model.SourceTorrentStats{
			ProviderID:    providerID,
			Imported:      c.total,
			Matched:       c.matched,
			Ambiguous:     c.ambiguous,
			Unmatched:     c.unmatched,
			Pending:       c.pending,
			PendingImport: pendingByProvider[providerID],
		})
	}
	return result, nil
}
