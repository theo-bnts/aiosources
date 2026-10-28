package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Admission and control changes share this lock. A data reset owns it
// exclusively, so no scheduled import or Resume can race the reset boundary.
func lockSourceData(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(1768843109,5)`)
	return databaseError("lock source data admission", err)
}

func (s *Store) StorageOverview(ctx context.Context) (model.StorageOverview, error) {
	var result model.StorageOverview
	err := s.pool.QueryRow(ctx, `SELECT mode,revision,(SELECT count(*) FROM ingest.runs WHERE status='running')
 FROM ingest.storage_settings WHERE singleton`).Scan(&result.Settings.Mode, &result.Settings.Revision, &result.Running)
	return result, databaseError("read storage settings", err)
}

func (s *Store) UpdateStorageSettings(ctx context.Context, settings model.StorageSettings) error {
	if !settings.Mode.Valid() || settings.Revision < 1 {
		return model.ErrInvalid
	}
	result, err := s.pool.Exec(ctx, `UPDATE ingest.storage_settings SET mode=$1,revision=revision+1 WHERE singleton AND revision=$2`, settings.Mode, settings.Revision)
	if err != nil {
		return databaseError("save storage settings", err)
	}
	if result.RowsAffected() == 0 {
		return model.ErrConflict
	}
	return nil
}

// ResetSourceData retains definitions (managed outside this database), secrets,
// schedules, sharing permissions, metadata catalogs and the useful debrid cache.
// Running collectors must be paused first. Queued and paused work is removed
// with its checkpoints; a subsequent Incremental starts with a fresh Full.
func (s *Store) ResetSourceData(ctx context.Context, revision int64) error {
	if revision < 1 {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin source data reset", err)
	}
	defer rollback(tx)
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(1768843109,5)`).Scan(&locked); err != nil {
		return databaseError("lock source data reset", err)
	}
	if !locked {
		return model.ErrBusy
	}
	// Retention locks diagnostic tables before publication. Wait for it before
	// taking that lock or attempting TRUNCATE, using the same transaction order.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4)`); err != nil {
		return databaseError("wait for source history cleanup", err)
	}
	var saved int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM ingest.storage_settings WHERE singleton FOR UPDATE`).Scan(&saved); err != nil {
		return databaseError("lock storage settings", err)
	}
	if saved != revision {
		return model.ErrConflict
	}
	var running bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.runs WHERE status='running')`).Scan(&running); err != nil {
		return databaseError("check source reset safety", err)
	}
	if running {
		return model.ErrBusy
	}
	// Acquire publication before table locks, matching every publishing writer.
	// Exports lock their recipient before their snapshot boundary. Lock recipients
	// before clearing that boundary so an in-flight export cannot deadlock reset.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3);
	SELECT id FROM ingest.shares ORDER BY id FOR UPDATE;
 SELECT ingest.clear_catalog_tracking();
 SELECT set_config('ingest.storage_reset','on',TRUE);
 SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
 TRUNCATE ingest.torrents,ingest.staged_torrents,ingest.raw_records,ingest.pages,
 ingest.aio_matches,ingest.aio_match_queue,ingest.aio_match_reviews,
 ingest.torrent_history,ingest.torrent_history_baselines,
 ingest.run_filter_records,ingest.run_scope_memberships,ingest.run_scope_counts,
 ingest.run_scope_refresh_candidates,ingest.run_record_identities,ingest.run_progress_identities;
 DELETE FROM ingest.activity_logs WHERE run_id IN (SELECT id FROM ingest.runs);
 DELETE FROM ingest.events;
 DELETE FROM ingest.runs;
 TRUNCATE ingest.collection_state,ingest.remote_checkpoints;
 UPDATE ingest.torrent_history_metadata SET history_since=clock_timestamp();
 UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1;
 UPDATE ingest.storage_settings SET revision=revision+1;
 UPDATE ingest.shares SET revision=revision+1,updated_at=now();
 SELECT set_config('ingest.storage_reset','off',TRUE);`); err != nil {
		return databaseError("reset source data", err)
	}
	if err := recordActivityTx(ctx, tx, "info", "storage.sources_reset", "", "", "", nil); err != nil {
		return err
	}
	return databaseError("commit source data reset", tx.Commit(ctx))
}
