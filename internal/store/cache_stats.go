package store

import (
	"context"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Use the same freshness rules as SharedAvailability. Account-specific rows
// are not mixed with shareable observations or counted once per credential.
func (s *Store) SharedAvailabilityStats(ctx context.Context, after time.Time) ([]model.SharedCacheStats, error) {
	rows, err := s.pool.Query(ctx, `SELECT service,count(*) FILTER(WHERE cached),count(*) FILTER(WHERE NOT cached),max(checked_at)
 FROM ingest.aio_shared_availability WHERE checked_at>$1 AND checked_at<=now()
 AND (cached OR checked_at>now()-interval '15 minutes') GROUP BY service ORDER BY service`, after)
	if err != nil {
		return nil, databaseError("count shared availability", err)
	}
	defer rows.Close()
	items := []model.SharedCacheStats{}
	for rows.Next() {
		var item model.SharedCacheStats
		if err := rows.Scan(&item.Service, &item.Cached, &item.Uncached, &item.LastChecked); err != nil {
			return nil, databaseError("read shared availability counts", err)
		}
		items = append(items, item)
	}
	return items, databaseError("count shared availability", rows.Err())
}
