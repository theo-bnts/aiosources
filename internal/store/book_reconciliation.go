package store

import (
	"context"
	"fmt"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

var bookSnapshotTables = []struct {
	table, key string
	counter    int
}{
	{"ol_editions", "id", 1}, {"ol_authors", "id", 2}, {"ol_redirects", "source", -1}, {"aio_works", "openlibrary_id", 0},
}

func (s *Store) BookExportLoaded(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET data_complete=TRUE WHERE singleton`)
	return err
}

// Bounded scans reconcile a verified snapshot. Normal recovery protects newer
// API revisions and cannot infer absence from an unreadable complete export.
// Selected catalogs instead require positive edition evidence; unreadable
// dependent records already needed by a selected edition are marked as seen.
func (s *Store) PruneBookSnapshot(ctx context.Context) (bool, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer rollback(tx)
	// Every chunk commits its resume boundary. Bound both lock acquisition and
	// execution on PostgreSQL itself so an interrupted client cannot leave a
	// cleanup statement holding shared metadata state for hours.
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='60s'`); err != nil {
		return false, 0, err
	}
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return false, 0, err
	}
	var generation, skipped int64
	var index int
	var after string
	var at *time.Time
	var loaded, rebuild bool
	var phase int
	if err = tx.QueryRow(ctx, `SELECT generation,snapshot_at,prune_table,prune_after,skipped,data_complete,selection_rebuild,export_phase FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&generation, &at, &index, &after, &skipped, &loaded, &rebuild, &phase); err != nil {
		return false, 0, err
	}
	if !loaded {
		return false, 0, model.ErrConflict
	}
	filtered := phase > 0
	if at == nil || skipped > 0 && !filtered || index >= len(bookSnapshotTables) {
		return true, 0, tx.Commit(ctx)
	}
	table := bookSnapshotTables[index]
	where := ""
	if table.table == "aio_works" {
		where = " AND media_type='book' AND openlibrary_id<>''"
	}
	extra := "FALSE"
	args := []any{after}
	if filtered && table.table == "ol_editions" {
		config, err := readAIOConfig(ctx, tx)
		if err != nil {
			return false, 0, err
		}
		if len(config.OpenLibraryLanguages) > 0 {
			extra = "NOT(languages && $2::text[])"
			args = append(args, config.OpenLibraryLanguages)
		}
	}
	query := fmt.Sprintf(`SELECT %s,ol_revision,ol_seen,ol_modified_at,%s FROM ingest.%s WHERE %s>$1%s ORDER BY %s LIMIT 1000`, table.key, extra, table.table, table.key, where, table.key)
	if filtered && table.table == "aio_works" {
		query = bookSnapshotWorkBatchSQL
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return false, 0, err
	}
	keys := []string{}
	revisions := []int64{}
	last := after
	var inspected int64
	for rows.Next() {
		inspected++
		var id string
		var revision, seen int64
		var modified *time.Time
		var excluded bool
		if err = rows.Scan(&id, &revision, &seen, &modified, &excluded); err != nil {
			rows.Close()
			return false, 0, err
		}
		last = id
		if excluded || seen != generation && (rebuild || modified == nil || !modified.After(*at)) {
			keys = append(keys, id)
			revisions = append(revisions, revision)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, 0, err
	}
	if len(keys) > 0 {
		if _, err = tx.Exec(ctx, `WITH affected AS (
 UPDATE ingest.aio_works w SET revision=revision+1,updated_at=now() WHERE media_type='book' AND openlibrary_id<>'' AND (
 openlibrary_id=ANY($1::text[]) OR (book_data->'author_keys') ?| $1::text[]
 OR openlibrary_id IN(SELECT ingest.ol_resolve(unnest(e.work_keys)) FROM ingest.ol_editions e WHERE e.id=ANY($1::text[]))) RETURNING id
 ) INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN affected a ON a.id=m.work_id
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, keys); err != nil {
			return false, 0, err
		}
		if !filtered {
			if _, err = tx.Exec(ctx, `INSERT INTO ingest.ol_deleted(id,ol_revision,ol_modified_at) SELECT * FROM unnest($1::text[],$2::bigint[],array_fill($3::timestamptz,ARRAY[cardinality($1::text[])]))
 ON CONFLICT(id) DO UPDATE SET ol_revision=GREATEST(ingest.ol_deleted.ol_revision,EXCLUDED.ol_revision),ol_modified_at=GREATEST(ingest.ol_deleted.ol_modified_at,EXCLUDED.ol_modified_at)`, keys, revisions, at); err != nil {
				return false, 0, err
			}
		}
		if _, err = tx.Exec(ctx, "DELETE FROM ingest."+table.table+" WHERE "+table.key+"=ANY($1::text[])", keys); err != nil {
			return false, 0, err
		}
		if table.counter >= 0 {
			column := []string{"works", "editions", "authors"}[table.counter]
			if _, err = tx.Exec(ctx, "UPDATE ingest.ol_import_state SET "+column+"="+column+"-$1 WHERE singleton", len(keys)); err != nil {
				return false, 0, err
			}
		}
	}
	if last == after {
		index++
		last = ""
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET prune_table=$1,prune_after=$2 WHERE singleton`, index, last); err != nil {
		return false, 0, err
	}
	return index >= len(bookSnapshotTables), inspected, databaseError("reconcile book snapshot", tx.Commit(ctx))
}

// Resolve aliases once per bounded batch. A correlated EXISTS can choose an
// early-exit sequential scan and evaluate the alias function for every edition
// of every work. The shared key set makes the GIN lookup independent of each
// candidate while preserving positive edition evidence, including redirects.
const bookSnapshotWorkBatchSQL = `WITH batch AS MATERIALIZED (
 SELECT openlibrary_id,ol_revision,ol_seen,ol_modified_at FROM ingest.aio_works
 WHERE openlibrary_id>$1 AND media_type='book' AND openlibrary_id<>''
 ORDER BY openlibrary_id LIMIT 1000
), aliases AS MATERIALIZED (
 SELECT openlibrary_id,unnest(ingest.ol_aliases(openlibrary_id)) AS key FROM batch
), selected AS MATERIALIZED (
 SELECT DISTINCT unnest(e.work_keys) AS key FROM ingest.ol_editions e
 WHERE e.work_keys && ARRAY(SELECT DISTINCT key FROM aliases)
)
SELECT b.*,NOT EXISTS(SELECT 1 FROM aliases a JOIN selected s USING(key)
 WHERE a.openlibrary_id=b.openlibrary_id) FROM batch b ORDER BY b.openlibrary_id`

const resetBookChangesSQL = `TRUNCATE ingest.ol_change_jobs,ingest.ol_deleted,ingest.ol_import_references;
 UPDATE ingest.ol_changes_state SET anchor_id='',anchor_at=NULL,tail_since=NULL,tail_start_id='',bootstrap_day=NULL,bootstrap_complete=FALSE,changes_through=NULL,last_sync=NULL,checked_at=NULL,retry_at=NULL,needs_dump_after=NULL,dump_checked_at=NULL,error='' WHERE singleton;
 UPDATE ingest.ol_import_state SET generation=0,snapshot_at=NULL,prune_table=0,prune_after='',data_complete=FALSE,sync_failures=0,
 selection='',export_phase=0,phase_snapshot='',phase_etag='',phase_position=0,filtered=0,selection_rebuild=FALSE WHERE singleton;`
