package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Availability is a small, expiring view, not a historical journal. Refresh it
// at every sync so new torrent memberships can reuse already known hashes.
// Stable keyset pagination and a captured check-time ceiling bound traversal.
func catalogAvailabilityPage(ctx context.Context, tx pgx.Tx, share model.Share, state *catalogToken, result *model.CatalogEnvelope) (bool, error) {
	config, err := readAIOConfig(ctx, tx)
	if err != nil {
		return false, err
	}
	limit := min(state.Limit, 100)
	rows, err := tx.Query(ctx, `SELECT a.service,a.hash FROM ingest.aio_shared_availability a
WHERE (a.service,a.hash)>(split_part($1,':',1),split_part($1,':',2))
AND a.service IN ('alldebrid','debridlink','premiumize','torbox') AND a.catalog_import_source='' AND a.cached
AND a.checked_at<=$2 AND a.checked_at>now()-make_interval(mins=>$3)
AND EXISTS(SELECT 1 FROM ingest.torrents t WHERE t.search_info_hash=a.hash AND ($4 OR t.provider_id=ANY($5::text[])))
ORDER BY a.service,a.hash LIMIT $6`, state.AuxIdentity, state.CacheBefore, config.CacheMinutes, share.Scope == "all", share.SourceIDs, limit+1)
	if err != nil {
		return false, databaseError("read shared cache view", err)
	}
	type key struct{ service, hash string }
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.service, &k.hash); err != nil {
			rows.Close()
			return false, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	more := len(keys) > limit
	if more {
		keys = keys[:limit]
	}
	bytes := 0
	for _, k := range keys {
		var a model.CatalogCacheObservation
		var files []byte
		err = tx.QueryRow(ctx, `SELECT service,hash,checked_at,files FROM ingest.aio_shared_availability
WHERE service=$1 AND hash=$2 AND catalog_import_source='' AND cached AND checked_at<=$3 AND checked_at>now()-make_interval(mins=>$4)`, k.service, k.hash, state.CacheBefore, config.CacheMinutes).Scan(&a.Service, &a.Hash, &a.CheckedAt, &files)
		if err != nil && err != pgx.ErrNoRows {
			return false, err
		}
		if err == nil && json.Unmarshal(files, &a.Files) == nil && a.Valid(time.Now()) {
			data, _ := json.Marshal(a)
			if bytes+len(data) > 6<<20 {
				return true, nil
			}
			bytes += len(data)
			result.Supplement.Availability = append(result.Supplement.Availability, a)
		}
		state.AuxIdentity = k.service + ":" + k.hash
	}
	return more, nil
}
