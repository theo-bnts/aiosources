package store

import (
	"context"
	"strings"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Incremental updates obey the same selection as the initial export. Unknown
// editions are inspected; unrelated work/author payloads are not retained.
func (s *Store) BookChangeRelevant(ctx context.Context, record books.Record) (bool, error) {
	c, err := s.AIOConfig(ctx)
	if err != nil {
		return false, err
	}
	selection := books.ImportSelection{RelevantOnly: c.OpenLibraryRelevantOnly, Languages: c.OpenLibraryLanguages}
	if !selection.Filtered() || record.Deleted != "" {
		return true, nil
	}
	if record.Edition != nil {
		e := record.Edition
		if selection.Allows(*e) {
			return true, nil
		}
		languageOnly := selection
		languageOnly.RelevantOnly = false
		if !languageOnly.Allows(*e) {
			return false, nil
		}
		ids := []string{"ol:" + e.ID}
		for _, key := range e.Works {
			ids = append(ids, "ol:"+key)
		}
		for _, isbn := range e.ISBN {
			if len(isbn) == 13 {
				ids = append(ids, "isbn:"+isbn[:12])
			}
		}
		var local bool
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.aio_matches WHERE normalized=$1 AND normalized<>'')
 OR EXISTS(SELECT 1 FROM ingest.torrents WHERE (`+nativeBookAssertionPredicate+`) AND ingest.aio_native_book_candidates(fields) && $2::text[])
 OR EXISTS(SELECT 1 FROM ingest.aio_match_reviews WHERE active AND media_type='book' AND (openlibrary_id=ANY($3::text[]) OR ingest.ol_resolve(openlibrary_id)=ANY($3::text[])))`, e.Normalized, ids, append([]string{e.ID}, e.Works...)).Scan(&local)
		return local, err
	}
	var selected bool
	if record.Work != nil {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.ol_editions WHERE work_keys && ingest.ol_aliases($1))
 OR EXISTS(SELECT 1 FROM ingest.ol_import_references WHERE id=$1 AND NOT (SELECT complete FROM ingest.ol_import_state WHERE singleton))`, record.Key()).Scan(&selected)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.ol_import_references WHERE id=$1)
 OR EXISTS(SELECT 1 FROM ingest.ol_redirects WHERE source=$1)`, record.Key()).Scan(&selected)
	}
	return selected, err
}

func (s *Store) ExcludeBookChange(ctx context.Context, job BookChangeJob, record books.Record) error {
	if record.Key() != job.ID || record.Revision < job.Revision {
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
	var removed int64
	keys := []string{}
	if record.Edition != nil {
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT work_keys FROM ingest.ol_editions WHERE id=$1 AND ol_revision<=$2),'{}'::text[])`, job.ID, record.Revision).Scan(&keys); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ingest.ol_editions WHERE id=$1 AND ol_revision<=$2`, job.ID, record.Revision)
		if err != nil {
			return err
		}
		removed = tag.RowsAffected()
	} else if record.Work != nil {
		keys = append(keys, record.Key())
	}
	if len(keys) > 0 {
		if _, err = tx.Exec(ctx, `WITH affected AS(UPDATE ingest.aio_works SET revision=revision+1,updated_at=now()
 WHERE media_type='book' AND openlibrary_id=ANY($1::text[]) RETURNING id)
 INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN affected a ON a.id=m.work_id
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, keys); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM ingest.aio_works w WHERE media_type='book' AND openlibrary_id=ANY($1::text[])
 AND NOT EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.work_keys && ingest.ol_aliases(w.openlibrary_id))`, keys)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET works=GREATEST(0,works-$1) WHERE singleton`, tag.RowsAffected()); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET editions=GREATEST(0,editions-$1) WHERE singleton`, removed); err != nil {
		return err
	}
	if removed > 0 || len(keys) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.ol_change_jobs WHERE id=$1 AND version=$2`, job.ID, job.Version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func bookRecordReferences(record books.Record) []string {
	keys := []string{}
	switch {
	case record.Edition != nil:
		keys = append(keys, record.Edition.Works...)
		keys = append(keys, record.Edition.Authors...)
	case record.Work != nil:
		keys = append(keys, record.Work.Book.AuthorKeys...)
	case record.Redirect != nil:
		keys = append(keys, record.Redirect.Target)
	}
	out := keys[:0]
	for _, key := range keys {
		if key != record.Key() && strings.HasPrefix(key, "OL") {
			out = append(out, key)
		}
	}
	return out
}
