package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"strconv"
	"strings"
)

func BookSelectionKey(c model.AIOConfig) string {
	languages, _ := books.ImportLanguages(c.OpenLibraryLanguages)
	return strconv.FormatBool(c.OpenLibraryRelevantOnly) + "|" + strings.Join(languages, ",")
}

// A scope change starts a new resumable snapshot. Existing information stays
// available until the selected snapshot has been verified and reconciled.
func (s *Store) PrepareBookImport(ctx context.Context, c model.AIOConfig) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err = prepareBookImport(ctx, tx, c); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func prepareBookImport(ctx context.Context, tx pgx.Tx, c model.AIOConfig) error {
	var err error
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return err
	}
	var old string
	if err = tx.QueryRow(ctx, `SELECT selection FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&old); err != nil {
		return err
	}
	key := BookSelectionKey(c)
	if old == key {
		return nil
	}
	filtered := c.OpenLibraryRelevantOnly || len(c.OpenLibraryLanguages) > 0
	if old == "" && !filtered {
		_, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET selection=$1 WHERE singleton`, key)
	} else {
		phase := 0
		if filtered {
			phase = 1
		}
		if _, err = tx.Exec(ctx, `TRUNCATE ingest.ol_import_references;`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET selection=$1,export_phase=$2,snapshot='',etag='',position=0,phase_snapshot='',phase_etag='',phase_position=0,
 imported=0,filtered=0,skipped=0,complete=FALSE,data_complete=FALSE,selection_rebuild=TRUE,snapshot_at=NULL,prune_table=0,prune_after='',checked_at=NULL,retry_at=NULL,error='' WHERE singleton;
`, key, phase)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE ingest.ol_changes_state SET bootstrap_day=NULL,bootstrap_complete=FALSE,changes_through=NULL,last_sync=NULL WHERE singleton;`)
	}
	return err
}

func (s *Store) BeginBookExportPart(ctx context.Context, phase int, snapshot, etag string) error {
	at, ok := books.ExportDate(snapshot)
	if !ok || phase < 1 || phase > 4 || len(snapshot) > 2048 || len(etag) > 512 {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return err
	}
	var current int
	var master, prior, identity string
	var complete bool
	if err = tx.QueryRow(ctx, `SELECT export_phase,snapshot,phase_snapshot,phase_etag,complete FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&current, &master, &prior, &identity, &complete); err != nil {
		return err
	}
	if phase == 1 && complete {
		if date, valid := books.ExportDate(master); valid && at.Before(date) {
			return model.ErrConflict
		}
		if _, err = tx.Exec(ctx, `TRUNCATE ingest.ol_import_references;
UPDATE ingest.ol_import_state SET export_phase=1,position=0,phase_snapshot='',phase_etag='',phase_position=0,imported=0,filtered=0,skipped=0,data_complete=FALSE,complete=FALSE,prune_table=0,prune_after='' WHERE singleton;`); err != nil {
			return err
		}
		current, master, prior, identity = 1, "", "", ""
	}
	if phase != current || (prior != "" && (prior != snapshot || identity != etag)) {
		return model.ErrConflict
	}
	if master != "" {
		if date, valid := books.ExportDate(master); !valid || !date.Equal(at) {
			return model.ErrConflict
		}
	}
	if phase == 1 && master == "" {
		_, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET snapshot=$1,etag=$2,snapshot_at=$3,generation=generation+1 WHERE singleton`, snapshot, etag, at)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET phase_snapshot=$1,phase_etag=$2,error='' WHERE singleton`, snapshot, etag)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SelectBookExportKeys(ctx context.Context, keys []string) (map[string]bool, error) {
	selected := map[string]bool{}
	if len(keys) > 0 {
		rows, err := s.pool.Query(ctx, `SELECT id FROM ingest.ol_import_references WHERE id=ANY($1::text[])`, keys)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				rows.Close()
				return nil, err
			}
			selected[key] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func (s *Store) FinishBookExportPart(ctx context.Context, phase int, position int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET export_phase=export_phase+1,phase_snapshot='',phase_etag='',phase_position=0,data_complete=($1=4) WHERE singleton AND export_phase=$1 AND phase_position=$2`, phase, position)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

// References absent from the export are resolved by the same durable change
// queue as later updates (including redirects), without per-book bulk requests.
func (s *Store) FinishBookImportReferences(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ingest.ol_change_jobs(id,revision,changed_at)
 SELECT r.id,0,COALESCE(i.snapshot_at,now()) FROM ingest.ol_import_references r CROSS JOIN ingest.ol_import_state i
 WHERE i.singleton AND NOT EXISTS(SELECT 1 FROM ingest.aio_works WHERE openlibrary_id=r.id AND openlibrary_id<>'')
 AND NOT EXISTS(SELECT 1 FROM ingest.ol_authors WHERE id=r.id) AND NOT EXISTS(SELECT 1 FROM ingest.ol_editions WHERE id=r.id)
 AND NOT EXISTS(SELECT 1 FROM ingest.ol_redirects WHERE source=r.id) AND NOT EXISTS(SELECT 1 FROM ingest.ol_deleted WHERE id=r.id)
 ON CONFLICT(id) DO NOTHING;`)
	return err
}
