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

// Bounded keyset scans retire entries absent from a verified snapshot. An API
// update newer than the snapshot is preserved even if the export lacks its key.
// An export with unreadable records is never used as evidence of absence.
func (s *Store) PruneBookSnapshot(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return false, err
	}
	var generation, skipped int64
	var index int
	var after string
	var at *time.Time
	var loaded bool
	if err = tx.QueryRow(ctx, `SELECT generation,snapshot_at,prune_table,prune_after,skipped,data_complete FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&generation, &at, &index, &after, &skipped, &loaded); err != nil {
		return false, err
	}
	if !loaded {
		return false, model.ErrConflict
	}
	if at == nil || skipped > 0 || index >= len(bookSnapshotTables) {
		return true, tx.Commit(ctx)
	}
	table := bookSnapshotTables[index]
	where := ""
	if table.table == "aio_works" {
		where = " AND media_type='book' AND openlibrary_id<>''"
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT %s,ol_revision,ol_seen,ol_modified_at FROM ingest.%s WHERE %s>$1%s ORDER BY %s LIMIT 1000`, table.key, table.table, table.key, where, table.key), after)
	if err != nil {
		return false, err
	}
	keys := []string{}
	revisions := []int64{}
	last := after
	for rows.Next() {
		var id string
		var revision, seen int64
		var modified *time.Time
		if err = rows.Scan(&id, &revision, &seen, &modified); err != nil {
			rows.Close()
			return false, err
		}
		last = id
		if seen != generation && (modified == nil || !modified.After(*at)) {
			keys = append(keys, id)
			revisions = append(revisions, revision)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	if len(keys) > 0 {
		if _, err = tx.Exec(ctx, `WITH affected AS (
 UPDATE ingest.aio_works w SET revision=revision+1,updated_at=now() WHERE media_type='book' AND (
 openlibrary_id=ANY($1::text[]) OR (book_data->'author_keys') ?| $1::text[]
 OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.id=ANY($1::text[]) AND e.work_keys && ingest.ol_aliases(w.openlibrary_id))) RETURNING id
 ) INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN affected a ON a.id=m.work_id
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, keys); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.ol_deleted(id,ol_revision,ol_modified_at) SELECT * FROM unnest($1::text[],$2::bigint[],array_fill($3::timestamptz,ARRAY[cardinality($1::text[])]))
 ON CONFLICT(id) DO UPDATE SET ol_revision=GREATEST(ingest.ol_deleted.ol_revision,EXCLUDED.ol_revision),ol_modified_at=GREATEST(ingest.ol_deleted.ol_modified_at,EXCLUDED.ol_modified_at)`, keys, revisions, at); err != nil {
			return false, err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM ingest."+table.table+" WHERE "+table.key+"=ANY($1::text[])", keys); err != nil {
			return false, err
		}
		if table.counter >= 0 {
			column := []string{"works", "editions", "authors"}[table.counter]
			if _, err = tx.Exec(ctx, "UPDATE ingest.ol_import_state SET "+column+"="+column+"-$1 WHERE singleton", len(keys)); err != nil {
				return false, err
			}
		}
	}
	if last == after {
		index++
		last = ""
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET prune_table=$1,prune_after=$2 WHERE singleton`, index, last); err != nil {
		return false, err
	}
	return index >= len(bookSnapshotTables), databaseError("reconcile book snapshot", tx.Commit(ctx))
}

const resetBookChangesSQL = `TRUNCATE ingest.ol_change_jobs,ingest.ol_deleted;
 UPDATE ingest.ol_changes_state SET anchor_id='',anchor_at=NULL,tail_since=NULL,tail_start_id='',bootstrap_day=NULL,bootstrap_complete=FALSE,changes_through=NULL,last_sync=NULL,checked_at=NULL,retry_at=NULL,needs_dump_after=NULL,dump_checked_at=NULL,error='' WHERE singleton;
 UPDATE ingest.ol_import_state SET generation=0,snapshot_at=NULL,prune_table=0,prune_after='',data_complete=FALSE WHERE singleton;`
