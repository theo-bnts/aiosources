package store

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

// DeleteSourceData removes one definition's publication and recovery data while
// preserving independent secrets, metadata and account-wide debrid caches.
// The registry holds its admission lock and removes the definition immediately
// before commit; a callback failure rolls back every database change.
func (s *Store) DeleteSourceData(ctx context.Context, providerID string, remove func() error) error {
	if providerID == "" {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin source deletion", err)
	}
	defer rollback(tx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(1768843109,5)`).Scan(&locked); err != nil {
		return databaseError("lock source deletion", err)
	}
	if !locked {
		return model.ErrBusy
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4)`); err != nil {
		return databaseError("wait for source history cleanup", err)
	}
	if err = lockProviderQueue(ctx, tx, providerID); err != nil {
		return err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.runs WHERE provider_id=$1 AND status IN ('queued','running'))`, providerID).Scan(&active); err != nil {
		return databaseError("check source deletion safety", err)
	}
	if active {
		return model.ErrBusy
	}

	const downloads = `SELECT d.run_id FROM ingest.operation_downloads d JOIN ingest.torrents t ON t.occurrence_id=d.input->>'occurrence_id' WHERE t.provider_id=$1`
	rows, err := tx.Query(ctx, `SELECT lock_key,status FROM ingest.operation_runs WHERE id IN (`+downloads+`) ORDER BY lock_key,id`, providerID)
	if err != nil {
		return databaseError("read source download preparations", err)
	}
	var keys []int64
	for rows.Next() {
		var key int64
		var status string
		if err = rows.Scan(&key, &status); err != nil {
			rows.Close()
			return databaseError("read source download preparation", err)
		}
		if status == "running" {
			rows.Close()
			return model.ErrBusy
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return databaseError("read source download preparations", err)
	}
	for _, key := range keys {
		if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&locked); err != nil {
			return databaseError("lock source download preparation", err)
		}
		if !locked {
			return model.ErrBusy
		}
	}

	// Match publication/reset lock order. Exporters hold their recipient before
	// reading the retention boundary, so lock recipients before changing it.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3);
 SELECT id FROM ingest.shares ORDER BY id FOR UPDATE;
 SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
 SELECT set_config('ingest.history_retention','on',TRUE);
 SELECT set_config('ingest.storage_reset','on',TRUE);`); err != nil {
		return databaseError("lock source publication", err)
	}

	queries := []string{

		`DELETE FROM ingest.activity_logs WHERE run_id IN (` + downloads + `)`,
		`DELETE FROM ingest.operation_events WHERE run_id IN (` + downloads + `)`,
		`DELETE FROM ingest.operation_runs WHERE id IN (` + downloads + `)`,
		// Never turn the last selected source into unrestricted access.
		`DELETE FROM ingest.shares WHERE scope='selected' AND source_ids=ARRAY[$1]::text[]`,
		`UPDATE ingest.shares SET source_ids=array_remove(source_ids,$1),revision=revision+1,updated_at=now() WHERE scope='selected' AND $1=ANY(source_ids)`,
		// A new sequence boundary forces existing receivers to restart with an
		// authoritative Full, without retaining this source's old payloads or
		// rebuilding any other source's journal. Sequence allocation is monotonic.
		`UPDATE ingest.catalog_retention SET floor=GREATEST(floor,nextval(pg_get_serial_sequence('ingest.catalog_journal','sequence'))) WHERE singleton AND (EXISTS(SELECT 1 FROM ingest.catalog_journal WHERE provider_id=$1) OR EXISTS(SELECT 1 FROM ingest.catalog_baselines WHERE provider_id=$1))`,
		`DELETE FROM ingest.torrents WHERE provider_id=$1`,
		`DELETE FROM ingest.catalog_journal WHERE provider_id=$1`,
		`DELETE FROM ingest.catalog_baselines WHERE provider_id=$1`,
		`DELETE FROM ingest.torrent_history WHERE provider_id=$1 OR run_id IN (SELECT id FROM ingest.runs WHERE provider_id=$1)`,
		`DELETE FROM ingest.torrent_history_baselines WHERE provider_id=$1`,
		`DELETE FROM ingest.stremio_requests WHERE results @> jsonb_build_array(jsonb_build_object('provider',$1::text))`,
		`DELETE FROM ingest.activity_logs WHERE provider_id=$1 OR run_id IN (SELECT id FROM ingest.runs WHERE provider_id=$1)`,
		`DELETE FROM ingest.events WHERE run_id IN (SELECT id FROM ingest.runs WHERE provider_id=$1)`,
		`DELETE FROM ingest.staged_torrents WHERE provider_id=$1 OR run_id IN (SELECT id FROM ingest.runs WHERE provider_id=$1)`,
		`UPDATE ingest.raw_records SET payload_id=NULL,payload_retained=FALSE WHERE payload_id IN (SELECT id FROM ingest.raw_records WHERE provider_id=$1)`,
		`DELETE FROM ingest.raw_records WHERE provider_id=$1`,
		`UPDATE ingest.pages SET payload_id=NULL,payload_retained=FALSE WHERE payload_id IN (SELECT id FROM ingest.pages WHERE provider_id=$1)`,
		`DELETE FROM ingest.pages WHERE provider_id=$1`,
	}
	for _, table := range []string{"run_scope_memberships", "run_scope_counts", "run_scope_refresh_candidates", "run_record_identities", "run_progress_identities"} {
		queries = append(queries, `DELETE FROM ingest.`+table+` WHERE run_id IN (SELECT id FROM ingest.runs WHERE provider_id=$1)`)
	}
	queries = append(queries,
		`UPDATE ingest.network_settings SET overrides=overrides-('source:'||$1),revision=revision+1 WHERE singleton AND overrides ? ('source:'||$1)`,
		`DELETE FROM ingest.runs WHERE provider_id=$1`,
		`DELETE FROM ingest.schedule_clocks WHERE provider_id=$1`,
		`DELETE FROM ingest.provider_requests WHERE provider_id=$1`,
		`DELETE FROM ingest.collection_state WHERE provider_id=$1`,
		`DELETE FROM ingest.remote_checkpoints WHERE provider_id=$1`,
		`DELETE FROM ingest.legacy_imports WHERE provider_id=$1`,
		`UPDATE ingest.settings SET value=jsonb_set(jsonb_set(value::jsonb,'{trackers}',(value::jsonb->'trackers')-$1),'{revision}',to_jsonb((value::jsonb->>'revision')::bigint+1))::text WHERE key='aiosource.config.v2' AND (value::jsonb->'trackers') ? $1`,
	)
	for _, query := range queries {
		if _, err = tx.Exec(ctx, query, providerID); err != nil {
			return databaseError("remove source data", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1;
 SELECT set_config('ingest.storage_reset','off',TRUE);`); err != nil {
		return databaseError("invalidate source identification", err)
	}
	if remove != nil {
		if err = remove(); err != nil {
			return err
		}
	}
	return databaseError("commit source deletion", tx.Commit(ctx))
}
