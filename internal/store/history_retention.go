package store

import (
	"context"
	"time"
)

const historyBatchSize = 5000

// PruneHistory expires diagnostic content after seven days. Each pass is
// bounded and transactional; an interrupted pass is retried without removing
// published fields, queued work, matching decisions or recovery evidence.
func (s *Store) PruneHistory(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, databaseError("begin history cleanup", err)
	}
	defer rollback(tx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(1768843109,4)`).Scan(&locked); err != nil {
		return 0, databaseError("lock history cleanup", err)
	}
	if !locked {
		return 0, nil
	}
	var cutoff time.Time
	if err = tx.QueryRow(ctx, `SELECT now()-make_interval(days=>(SELECT (values->>'days')::int FROM ingest.runtime_settings WHERE section='history'))`).Scan(&cutoff); err != nil {
		return 0, databaseError("read history cutoff", err)
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('ingest.history_retention','on',true)`); err != nil {
		return 0, databaseError("authorize history cleanup", err)
	}
	// Paused work and unsuccessful full imports with staging are recoverable.
	// A metadata child retains its parent's identity membership until it expires.
	// Skip row locks held by workers or Resume, rather than blocking either.
	rows, err := tx.Query(ctx, `SELECT r.id FROM ingest.runs r
 WHERE r.finished_at<$1 AND r.status IN ('succeeded','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM ingest.staged_torrents t WHERE t.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.runs child WHERE child.metadata_parent_run_id=r.id)
 ORDER BY r.finished_at,r.id LIMIT 64 FOR UPDATE OF r SKIP LOCKED`, cutoff)
	if err != nil {
		return 0, databaseError("select expired collections", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, databaseError("read expired collections", err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, databaseError("read expired collections", err)
	}
	var removed int64
	exec := func(query string, args ...any) error {
		result, err := tx.Exec(ctx, query, args...)
		if err == nil {
			removed += result.RowsAffected()
		}
		return databaseError("expire history batch", err)
	}
	// A reference never keeps an old original archive alive indefinitely. New
	// observations retain their processing identity, with no archived payload.
	for _, table := range []string{"raw_records", "pages"} {
		if result, e := tx.Exec(ctx, `UPDATE ingest.`+table+` SET payload_id=NULL,payload_retained=FALSE
 WHERE id IN (SELECT r.id FROM ingest.`+table+` r JOIN ingest.`+table+` payload ON payload.id=r.payload_id
 WHERE payload.created_at<$1 ORDER BY r.id LIMIT $2)`, cutoff, historyBatchSize); e != nil {
			return 0, databaseError("expire archive references", e)
		} else {
			removed += result.RowsAffected()
		}
	}
	if err = exec(`DELETE FROM ingest.raw_records WHERE id IN (
 SELECT r.id FROM ingest.raw_records r WHERE r.created_at<$1
 AND NOT EXISTS(SELECT 1 FROM ingest.raw_records reference WHERE reference.payload_id=r.id)
 ORDER BY r.created_at,r.id LIMIT $2)`, cutoff, historyBatchSize); err != nil {
		return 0, err
	}
	// Fingerprints, filter coverage and the latest retry observation still serve
	// an unfinished traversal. Their original response bytes are diagnostic only.
	if result, e := tx.Exec(ctx, `UPDATE ingest.pages SET body=''::bytea,payload_id=NULL,payload_retained=FALSE
 WHERE id IN (SELECT id FROM ingest.pages WHERE created_at<$1
 AND (payload_retained OR octet_length(body)>0) ORDER BY created_at,id LIMIT $2)`, cutoff, historyBatchSize); e != nil {
		return 0, databaseError("expire response payloads", e)
	} else {
		removed += result.RowsAffected()
	}
	if err = exec(`DELETE FROM ingest.pages WHERE id IN (
 SELECT p.id FROM ingest.pages p JOIN ingest.runs r ON r.id=p.run_id WHERE p.created_at<$1
 AND (r.status='succeeded' OR r.id=ANY($2::text[])
  OR (p.fingerprint IS NULL AND COALESCE(p.metadata->>'traversal_phase','')<>'options_list'
   AND p.id<>(SELECT id FROM ingest.pages latest WHERE latest.run_id=r.id ORDER BY id DESC LIMIT 1)))
 AND NOT EXISTS(SELECT 1 FROM ingest.raw_records raw WHERE raw.page_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.pages reference WHERE reference.payload_id=p.id)
 ORDER BY p.created_at,p.id LIMIT $3)`, cutoff, ids, historyBatchSize); err != nil {
		return 0, err
	}
	for _, table := range []string{"activity_logs", "events", "operation_events", "stremio_requests"} {
		if err = exec(`DELETE FROM ingest.`+table+` WHERE id IN (
 SELECT id FROM ingest.`+table+` WHERE created_at<$1 ORDER BY created_at,id LIMIT $2)`, cutoff, historyBatchSize); err != nil {
			return 0, err
		}
	}
	if err = exec(`DELETE FROM ingest.operation_runs WHERE id IN (
 SELECT r.id FROM ingest.operation_runs r WHERE finished_at<$1 AND status<>'running'
 AND NOT EXISTS(SELECT 1 FROM ingest.operation_events e WHERE e.run_id=r.id)
 ORDER BY finished_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)`, cutoff, historyBatchSize); err != nil {
		return 0, err
	}
	for _, table := range []string{"run_record_identities", "run_scope_memberships", "run_scope_counts", "run_scope_refresh_candidates", "run_progress_identities"} {
		if err = exec(`DELETE FROM ingest.`+table+` WHERE ctid IN (
 SELECT ctid FROM ingest.`+table+` WHERE run_id=ANY($1::text[]) LIMIT $2)`, ids, historyBatchSize); err != nil {
			return 0, err
		}
	}
	before := removed
	if err = exec(`DELETE FROM ingest.torrent_history WHERE id IN (
 SELECT id FROM ingest.torrent_history WHERE occurred_at<$1 ORDER BY occurred_at,id LIMIT $2)`, cutoff, historyBatchSize); err != nil {
		return 0, err
	}
	if err = exec(`DELETE FROM ingest.torrent_history_baselines WHERE occurrence_id IN (
 SELECT occurrence_id FROM ingest.torrent_history_baselines WHERE recorded_at<$1 ORDER BY recorded_at,occurrence_id LIMIT $2)`, cutoff, historyBatchSize); err != nil {
		return 0, err
	}
	if removed > before {
		if _, err = tx.Exec(ctx, `UPDATE ingest.torrent_history_metadata SET history_since=GREATEST(history_since,$1) WHERE singleton AND history_since<$1`, cutoff); err != nil {
			return 0, databaseError("advance retained history boundary", err)
		}
	}
	if err = exec(`DELETE FROM ingest.runs r WHERE r.id=ANY($1::text[])
 AND NOT EXISTS(SELECT 1 FROM ingest.pages p WHERE p.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.raw_records raw WHERE raw.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.events e WHERE e.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.torrent_history h WHERE h.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.run_record_identities i WHERE i.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.run_scope_memberships i WHERE i.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.run_scope_counts i WHERE i.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.run_scope_refresh_candidates i WHERE i.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.run_progress_identities i WHERE i.run_id=r.id)`, ids); err != nil {
		return 0, err
	}
	// Serialize the immutable publication boundary with native writers. Keep
	// the last state of each copy at that boundary plus all seven-day changes.
	// Older readers must restart with Full; recent paged snapshots stay exact.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3)`); err != nil {
		return 0, databaseError("lock catalog retention", err)
	}
	if _, err = tx.Exec(ctx, `SELECT ingest.sync_catalog_tracking()`); err != nil {
		return 0, databaseError("reconcile catalog storage", err)
	}
	if err = exec(`WITH expired AS MATERIALIZED (
 SELECT sequence,provider_id,source_id,origin,fields,deleted,recorded_at FROM ingest.catalog_journal
 WHERE recorded_at<$1 ORDER BY recorded_at,sequence LIMIT $2
), boundary AS (
 UPDATE ingest.catalog_retention SET floor=candidate.floor
 FROM (SELECT max(sequence) AS floor FROM expired) candidate
 WHERE singleton AND catalog_retention.floor<candidate.floor RETURNING catalog_retention.floor
), latest AS (
 SELECT DISTINCT ON(provider_id,source_id,origin) * FROM expired
 ORDER BY provider_id,source_id,origin,sequence DESC
), retained AS (
 INSERT INTO ingest.catalog_baselines AS previous(provider_id,source_id,origin,fields,deleted,sequence,recorded_at)
 SELECT provider_id,source_id,origin,ingest.catalog_fields(fields),deleted,sequence,recorded_at FROM latest
 ON CONFLICT(provider_id,source_id,origin) DO UPDATE SET fields=EXCLUDED.fields,deleted=EXCLUDED.deleted,
 sequence=EXCLUDED.sequence,recorded_at=EXCLUDED.recorded_at WHERE previous.sequence<EXCLUDED.sequence
 RETURNING 1
)
DELETE FROM ingest.catalog_journal WHERE sequence IN (SELECT sequence FROM expired)`, cutoff, historyBatchSize); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, databaseError("commit history cleanup", err)
	}
	return removed, nil
}
