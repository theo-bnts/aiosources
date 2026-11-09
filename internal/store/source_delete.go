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
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.runs WHERE provider_id=$1 AND status IN ('queued','running'))`, providerID).Scan(&active); err != nil {
		return databaseError("check source deletion safety", err)
	}
	if active {
		return model.ErrBusy
	}

	const downloads = `SELECT d.run_id FROM aiosources.operation_downloads d JOIN aiosources.torrents t ON t.occurrence_id=d.input->>'occurrence_id' WHERE t.provider_id=$1`
	rows, err := tx.Query(ctx, `SELECT lock_key,status FROM aiosources.operation_runs WHERE id IN (`+downloads+`) ORDER BY lock_key,id`, providerID)
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
 SELECT pg_advisory_xact_lock(1768843109,9);
 SELECT revision FROM aiosources.aio_public_access WHERE singleton FOR UPDATE;
 SELECT revision FROM aiosources.public_catalog_policy WHERE singleton FOR UPDATE;
 SELECT id FROM aiosources.shares ORDER BY id FOR UPDATE;
 SELECT metadata_generation FROM aiosources.aio_state FOR UPDATE;
 SELECT set_config('aiosources.history_retention','on',TRUE);
 SELECT set_config('aiosources.storage_reset','on',TRUE);`); err != nil {
		return databaseError("lock source publication", err)
	}

	// Removing the final explicit source revokes public catalog access before
	// clearing its selection. Empty must never become an unrestricted grant.
	tag, err := tx.Exec(ctx, `UPDATE aiosources.aio_public_access SET revision=revision+1,catalog_enabled=FALSE,catalog_generation=catalog_generation+1 WHERE singleton AND catalog_enabled AND EXISTS(SELECT 1 FROM aiosources.public_catalog_policy WHERE singleton AND source_ids=ARRAY[$1]::text[])`, providerID)
	if err != nil {
		return databaseError("revoke removed public catalog source", err)
	}
	if tag.RowsAffected() > 0 {
		if err = recordAccessActivityTx(ctx, tx, "security.public_catalog_disabled", "public"); err != nil {
			return err
		}
	}
	queries := []string{
		`UPDATE aiosources.public_catalog_policy SET source_ids=array_remove(source_ids,$1),revision=revision+1 WHERE singleton AND $1=ANY(source_ids)`,

		`DELETE FROM aiosources.activity_logs WHERE run_id IN (` + downloads + `)`,
		`DELETE FROM aiosources.operation_events WHERE run_id IN (` + downloads + `)`,
		`DELETE FROM aiosources.operation_runs WHERE id IN (` + downloads + `)`,
		// Never turn the last selected source into unrestricted access.
		`DELETE FROM aiosources.shares WHERE scope='selected' AND source_ids=ARRAY[$1]::text[]`,
		`UPDATE aiosources.shares SET source_ids=array_remove(source_ids,$1),revision=revision+1,updated_at=now() WHERE scope='selected' AND $1=ANY(source_ids)`,
		// A new sequence boundary forces existing receivers to restart with an
		// authoritative Full, without retaining this source's old payloads or
		// rebuilding any other source's journal. Sequence allocation is monotonic.
		`UPDATE aiosources.catalog_retention SET floor=GREATEST(floor,nextval(pg_get_serial_sequence('aiosources.catalog_journal','sequence'))) WHERE singleton AND (EXISTS(SELECT 1 FROM aiosources.catalog_journal WHERE provider_id=$1) OR EXISTS(SELECT 1 FROM aiosources.catalog_baselines WHERE provider_id=$1))`,
		`DELETE FROM aiosources.torrents WHERE provider_id=$1`,
		`DELETE FROM aiosources.catalog_journal WHERE provider_id=$1`,
		`DELETE FROM aiosources.catalog_baselines WHERE provider_id=$1`,
		`DELETE FROM aiosources.torrent_history WHERE provider_id=$1 OR run_id IN (SELECT id FROM aiosources.runs WHERE provider_id=$1)`,
		`DELETE FROM aiosources.torrent_history_baselines WHERE provider_id=$1`,
		`DELETE FROM aiosources.stremio_requests WHERE results @> jsonb_build_array(jsonb_build_object('provider',$1::text))`,
		`DELETE FROM aiosources.activity_logs WHERE provider_id=$1 OR run_id IN (SELECT id FROM aiosources.runs WHERE provider_id=$1)`,
		`DELETE FROM aiosources.events WHERE run_id IN (SELECT id FROM aiosources.runs WHERE provider_id=$1)`,
		`DELETE FROM aiosources.staged_torrents WHERE provider_id=$1 OR run_id IN (SELECT id FROM aiosources.runs WHERE provider_id=$1)`,
		`UPDATE aiosources.raw_records SET payload_id=NULL,payload_retained=FALSE WHERE payload_id IN (SELECT id FROM aiosources.raw_records WHERE provider_id=$1)`,
		`DELETE FROM aiosources.raw_records WHERE provider_id=$1`,
		`UPDATE aiosources.pages SET payload_id=NULL,payload_retained=FALSE WHERE payload_id IN (SELECT id FROM aiosources.pages WHERE provider_id=$1)`,
		`DELETE FROM aiosources.pages WHERE provider_id=$1`,
	}
	for _, table := range []string{"run_scope_memberships", "run_scope_counts", "run_scope_refresh_candidates", "run_record_identities", "run_progress_identities"} {
		queries = append(queries, `DELETE FROM aiosources.`+table+` WHERE run_id IN (SELECT id FROM aiosources.runs WHERE provider_id=$1)`)
	}
	queries = append(queries,
		`UPDATE aiosources.network_settings SET overrides=overrides-('source:'||$1),revision=revision+1 WHERE singleton AND overrides ? ('source:'||$1)`,
		`DELETE FROM aiosources.runs WHERE provider_id=$1`,
		`DELETE FROM aiosources.schedule_clocks WHERE provider_id=$1`,
		`DELETE FROM aiosources.provider_requests WHERE provider_id=$1`,
		`DELETE FROM aiosources.collection_state WHERE provider_id=$1`,
		`DELETE FROM aiosources.remote_checkpoints WHERE provider_id=$1`,
		`DELETE FROM aiosources.legacy_imports WHERE provider_id=$1`,
		`UPDATE aiosources.settings SET value=jsonb_set(jsonb_set(value::jsonb,'{trackers}',(value::jsonb->'trackers')-$1),'{revision}',to_jsonb((value::jsonb->>'revision')::bigint+1))::text WHERE key='aiosource.config.v2' AND (value::jsonb->'trackers') ? $1`,
	)
	for _, query := range queries {
		if _, err = tx.Exec(ctx, query, providerID); err != nil {
			return databaseError("remove source data", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.aio_state SET metadata_generation=metadata_generation+1;
 SELECT set_config('aiosources.storage_reset','off',TRUE);`); err != nil {
		return databaseError("invalidate source identification", err)
	}
	if remove != nil {
		if err = remove(); err != nil {
			return err
		}
	}
	return databaseError("commit source deletion", tx.Commit(ctx))
}
