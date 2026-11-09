package store

import (
	"context"
	"encoding/json"

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
	var options []byte
	err := s.pool.QueryRow(ctx, `SELECT mode,revision,options,(SELECT count(*) FROM aiosources.runs WHERE status='running'),
 EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE l.locktype='advisory' AND l.classid=1768843109 AND l.objid=5 AND l.granted AND a.datname=current_database() AND a.application_name='aiosources-storage-indexes')
 FROM aiosources.storage_settings WHERE singleton`).Scan(&result.Settings.Mode, &result.Settings.Revision, &options, &result.Running, &result.Indexing)
	if err == nil {
		err = json.Unmarshal(options, &result.Settings.Options)
		if err == nil && result.Settings.Options != nil && result.Settings.Options.AdditionalFields == nil {
			result.Settings.Options.AdditionalFields = []string{}
		}
	}
	return result, databaseError("read storage settings", err)
}

func (s *Store) UpdateStorageSettings(ctx context.Context, settings model.StorageSettings) error {
	if !settings.Mode.Valid() || settings.Revision < 1 {
		return model.ErrInvalid
	}
	opts := settings.EffectiveOptions()
	// Older browser bundles round-trip newly returned fields while changing
	// only mode. Honour that explicit legacy edit without discarding unrelated
	// history choices introduced by a newer client.
	if settings.Options != nil && opts.AllFields != (settings.Mode == model.StorageFull) {
		opts.AllFields = settings.Mode == model.StorageFull
		opts.SuccessfulRecords = opts.AllFields
	}
	if !opts.Valid() {
		return model.ErrInvalid
	}
	opts.AdditionalFields = append([]string{}, opts.AdditionalFields...)
	mode := model.StorageCompact
	if opts.AllFields {
		mode = model.StorageFull
	}
	encoded, err := json.Marshal(opts)
	if err != nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin data selection", err)
	}
	defer rollback(tx)
	if err = lockSourceData(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3)`); err != nil {
		return databaseError("lock data selection", err)
	}
	var historyChanged bool
	if err = tx.QueryRow(ctx, `SELECT (options->>'publication_history')::boolean<>$1 FROM aiosources.storage_settings WHERE singleton FOR UPDATE`, opts.PublicationHistory).Scan(&historyChanged); err != nil {
		return databaseError("read previous history selection", err)
	}
	result, err := tx.Exec(ctx, `UPDATE aiosources.storage_settings SET mode=$1,options=$3,revision=revision+1 WHERE singleton AND revision=$2`, mode, settings.Revision, encoded)
	if err != nil {
		return databaseError("save storage settings", err)
	}
	if result.RowsAffected() == 0 {
		return model.ErrConflict
	}
	if historyChanged {
		if _, err = tx.Exec(ctx, `SELECT set_config('aiosources.history_retention','on',true); UPDATE aiosources.torrent_history_metadata SET history_since=clock_timestamp()`); err != nil {
			return databaseError("advance publication history boundary", err)
		}
	}
	return databaseError("commit data selection", tx.Commit(ctx))
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
	if err := tx.QueryRow(ctx, `SELECT revision FROM aiosources.storage_settings WHERE singleton FOR UPDATE`).Scan(&saved); err != nil {
		return databaseError("lock storage settings", err)
	}
	if saved != revision {
		return model.ErrConflict
	}
	var running bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.runs WHERE status='running')`).Scan(&running); err != nil {
		return databaseError("check source reset safety", err)
	}
	if running {
		return model.ErrBusy
	}
	// Acquire publication before table locks, matching every publishing writer.
	// Exports lock their recipient before their snapshot boundary. Lock recipients
	// before clearing that boundary so an in-flight export cannot deadlock reset.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3);
	SELECT id FROM aiosources.shares ORDER BY id FOR UPDATE;
 SELECT revision FROM aiosources.public_catalog_policy WHERE singleton FOR UPDATE;
 SELECT aiosources.clear_catalog_tracking();
 SELECT set_config('aiosources.storage_reset','on',TRUE);
 SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE;
 TRUNCATE aiosources.torrents,aiosources.staged_torrents,aiosources.raw_records,aiosources.pages,
 aiosources.aio_matches,aiosources.aio_match_queue,aiosources.aio_match_reviews,
 aiosources.torrent_history,aiosources.torrent_history_baselines,
 aiosources.run_filter_records,aiosources.run_scope_memberships,aiosources.run_scope_counts,
 aiosources.run_scope_refresh_candidates,aiosources.run_record_identities,aiosources.run_progress_identities;
 DELETE FROM aiosources.activity_logs WHERE run_id IN (SELECT id FROM aiosources.runs);
 DELETE FROM aiosources.events;
 DELETE FROM aiosources.runs;
 TRUNCATE aiosources.collection_state,aiosources.remote_checkpoints;
 UPDATE aiosources.torrent_history_metadata SET history_since=clock_timestamp();
 UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1;
 UPDATE aiosources.storage_settings SET revision=revision+1;
 UPDATE aiosources.shares SET revision=revision+1,updated_at=now();
 UPDATE aiosources.public_catalog_policy SET revision=revision+1 WHERE singleton;
 SELECT set_config('aiosources.storage_reset','off',TRUE);`); err != nil {
		return databaseError("reset source data", err)
	}
	if err := recordActivityTx(ctx, tx, "info", "storage.sources_reset", "", "", "", nil); err != nil {
		return err
	}
	return databaseError("commit source data reset", tx.Commit(ctx))
}
