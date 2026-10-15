package store

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// MeasureHealth reads relation sizes without traversing observations or holding
// collection leases. A failed measurement never substitutes zero for old data.
func (s *Store) MeasureHealth(ctx context.Context, disk model.HealthDisk) (*model.SystemHealth, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, databaseError("begin storage measurement", err)
	}
	defer rollback(tx)
	report := &model.SystemHealth{Status: "healthy", Disk: disk}
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&report.CheckedAt); err != nil {
		return nil, databaseError("read storage measurement time", err)
	}
	report.NextCheckAt = report.CheckedAt.Add(time.Minute)
	report.Database.Categories = []model.HealthStorageCategory{{Key: "sources"}, {Key: "metadata"}, {Key: "debrid"}, {Key: "stremio"}, {Key: "sharing"}, {Key: "access"}, {Key: "logs"}, {Key: "other"}}
	if err := tx.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&report.Database.TotalBytes); err != nil {
		return nil, databaseError("measure database storage", err)
	}
	rows, err := tx.Query(ctx, `SELECT CASE
WHEN n.nspname='ingest' AND `+sourceStorageRelations+` THEN 'sources'
WHEN n.nspname='ingest' AND c.relname IN ('aio_works','aio_titles','aio_seasons','aio_episodes','aio_metadata_jobs','aio_matches','aio_match_queue','aio_match_reviews','aio_state','aio_title_variants','aio_season_requests','aio_inventory') THEN 'metadata'
WHEN n.nspname='ingest' AND c.relname IN ('aio_availability','aio_shared_availability','aio_resources','aio_cleanup','aio_incidents') THEN 'debrid'
WHEN n.nspname='ingest' AND c.relname IN ('stremio_requests','aio_public_access') THEN 'stremio'
WHEN n.nspname='ingest' AND (c.relname IN ('catalog_journal','catalog_baselines','catalog_identity','catalog_retention','catalog_tracking','remote_checkpoints','shares') OR c.relname LIKE 'share_%') THEN 'sharing'
WHEN n.nspname='ingest' AND c.relname IN ('activity_logs','operation_runs','operation_events') THEN 'logs'
WHEN n.nspname='ingest' AND (c.relname IN ('secrets','security_settings','security_attempts','security_recovery_codes') OR c.relname LIKE 'admin_%') THEN 'access'
ELSE 'other' END AS category,
CASE
 WHEN c.relname='torrents' THEN 'published'
 WHEN c.relname='staged_torrents' OR c.relname LIKE 'run_%' THEN 'imports'
 WHEN c.relname IN ('raw_records','pages','events','torrent_history','torrent_history_baselines','torrent_history_metadata') THEN 'source-history'
 WHEN c.relname IN ('aio_works','aio_titles','aio_seasons','aio_episodes') THEN 'information'
 WHEN c.relname IN ('aio_matches','aio_match_queue','aio_match_reviews') THEN 'identification'
 WHEN c.relname IN ('aio_inventory','aio_metadata_jobs','aio_title_variants','aio_season_requests') THEN 'information-queue'
 WHEN c.relname IN ('aio_availability','aio_shared_availability','aio_resources') THEN 'availability'
 WHEN c.relname IN ('aio_cleanup','aio_incidents') THEN 'debrid-operations'
 WHEN c.relname IN ('catalog_journal','catalog_baselines') THEN 'shared-snapshots'
 WHEN c.relname='stremio_requests' THEN 'stream-history'
 WHEN c.relname='activity_logs' THEN 'activity-history'
 WHEN c.relname IN ('operation_runs','operation_events') THEN 'operation-history'
 ELSE 'state' END AS purpose,
COALESCE(SUM(pg_total_relation_size(c.oid)),0)::bigint,COALESCE(SUM(pg_table_size(c.oid)),0)::bigint,COALESCE(SUM(pg_indexes_size(c.oid)),0)::bigint
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE c.relkind IN ('r','m') AND n.nspname !~ '^pg_' AND n.nspname<>'information_schema' GROUP BY category,purpose`)
	if err != nil {
		return nil, databaseError("measure database relations", err)
	}
	for rows.Next() {
		var categoryKey string
		var part model.HealthStorageCategory
		if err := rows.Scan(&categoryKey, &part.Key, &part.Bytes, &part.TableBytes, &part.IndexBytes); err != nil {
			rows.Close()
			return nil, databaseError("read relation measurement", err)
		}
		for i := range report.Database.Categories {
			if report.Database.Categories[i].Key == categoryKey {
				parent := &report.Database.Categories[i]
				parent.Bytes += part.Bytes
				parent.TableBytes += part.TableBytes
				parent.IndexBytes += part.IndexBytes
				parent.Details = append(parent.Details, part)
			}
		}
		report.Database.RelationBytes += part.Bytes
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, databaseError("read relation measurements", err)
	}
	slices.SortFunc(report.Database.Categories, func(a, b model.HealthStorageCategory) int {
		if order := cmp.Compare(b.Bytes, a.Bytes); order != 0 {
			return order
		}
		return cmp.Compare(a.Key, b.Key)
	})
	for i := range report.Database.Categories {
		slices.SortFunc(report.Database.Categories[i].Details, func(a, b model.HealthStorageCategory) int {
			if order := cmp.Compare(b.Bytes, a.Bytes); order != 0 {
				return order
			}
			return cmp.Compare(a.Key, b.Key)
		})
	}
	report.Database.OtherDatabaseBytes = report.Database.TotalBytes - report.Database.RelationBytes
	// Concurrent writes can grow relations between independent size calls. Keep
	// the raw measurements; a negative remainder is explicitly visible, not faked.
	report.Database.Available = true
	report.Database.MeasuredAt = &report.CheckedAt
	// Source estimates are optional. A statistics permission or planner failure
	// must not hide the independently measured database and filesystem sizes.
	if estimateTx, err := tx.Begin(ctx); err == nil {
		if estimates, err := sourceStorageEstimates(ctx, estimateTx); err == nil {
			if err := estimateTx.Commit(ctx); err == nil {
				report.SourceStorage = estimates
			}
		} else {
			rollback(estimateTx)
		}
	}
	if estimateTx, err := tx.Begin(ctx); err == nil {
		imports, total, cancelled, err := importStorageEstimates(ctx, estimateTx)
		if err == nil {
			if err := estimateTx.Commit(ctx); err == nil {
				report.RetainedImports, report.RetainedImportCount, report.CancelledImportCount = imports, total, cancelled
				report.ImportStorageAvailable = true
			}
		} else {
			rollback(estimateTx)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, databaseError("finish storage measurement", err)
	}
	return report, nil
}
