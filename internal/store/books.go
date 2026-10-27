package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *Store) RecordBookInformationActivity(ctx context.Context, updateErr error, processed, skipped int64) error {
	level, kind, message := "info", "openlibrary.sync_completed", "Book information is up to date."
	if updateErr != nil {
		level, kind, message = "error", "openlibrary.sync_failed", "Book information update failed. Saved progress is preserved."
		if errors.Is(updateErr, context.Canceled) {
			level, kind, message = "warn", "openlibrary.sync_interrupted", "Book information update interrupted. Saved progress is preserved."
		}
	}
	if updateErr == nil && skipped > 0 {
		level, kind, message = "warn", "openlibrary.records_skipped", "Book information updated. Unreadable or oversized records were skipped; review the update history."
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,title,service,data) VALUES($1,$2,$3,$4,'Book information update','openlibrary',jsonb_build_object('records',$5::bigint))`, "activity-"+id, level, kind, message, processed)
	return databaseError("record book information activity", err)
}

func (s *Store) OpenLibraryStatus(ctx context.Context) (model.OpenLibraryStatus, error) {
	var out model.OpenLibraryStatus
	err := s.pool.QueryRow(ctx, `SELECT snapshot,position,imported,works,editions,authors,last_sync,error,complete,skipped FROM ingest.ol_import_state WHERE singleton`).Scan(&out.Snapshot, &out.Position, &out.Imported, &out.Works, &out.Editions, &out.Authors, &out.LastSync, &out.Error, &out.Complete, &out.Skipped)
	return out, databaseError("read book information progress", err)
}

func (s *Store) OpenLibraryCheckpoint(ctx context.Context) (string, string, int64, error) {
	var snapshot, etag string
	var position int64
	err := s.pool.QueryRow(ctx, `SELECT snapshot,etag,position FROM ingest.ol_import_state WHERE singleton`).Scan(&snapshot, &etag, &position)
	return snapshot, etag, position, err
}

func (s *Store) OpenLibraryDue(ctx context.Context, days int) (bool, error) {
	if days < 1 || days > 366 {
		return false, model.ErrInvalid
	}
	var due bool
	err := s.pool.QueryRow(ctx, `SELECT (retry_at IS NULL OR retry_at<=now()) AND (NOT complete OR last_sync IS NULL OR COALESCE(checked_at,last_sync)+make_interval(days=>$1)<=now()) FROM ingest.ol_import_state WHERE singleton`, days).Scan(&due)
	return due, err
}

func (s *Store) BeginOpenLibrary(ctx context.Context, snapshot, etag string) (int64, error) {
	if len(snapshot) > 2048 || len(etag) > 512 || snapshot == "" {
		return 0, model.ErrInvalid
	}
	var position int64
	err := s.pool.QueryRow(ctx, `UPDATE ingest.ol_import_state SET position=CASE WHEN snapshot=$1 AND etag=$2 THEN position ELSE 0 END,
 imported=CASE WHEN snapshot=$1 AND etag=$2 THEN imported ELSE 0 END,skipped=CASE WHEN snapshot=$1 AND etag=$2 THEN skipped ELSE 0 END,complete=FALSE,snapshot=$1,etag=$2,error='' WHERE singleton RETURNING position`, snapshot, etag).Scan(&position)
	return position, databaseError("begin book information import", err)
}

// A batch and its checkpoint commit together. Replaying an export after a
// restart cannot skip uncommitted rows or create duplicate works/editions.
func (s *Store) SaveOpenLibraryBatch(ctx context.Context, snapshot string, expected, position int64, records []books.Record) error {
	if len(records) > 250 || position <= expected {
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
	var current int64
	var source string
	var refresh bool
	if err = tx.QueryRow(ctx, `SELECT snapshot,position,last_sync IS NOT NULL FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&source, &current, &refresh); err != nil {
		return err
	}
	if source != snapshot || current != expected {
		return model.ErrConflict
	}
	keys := []string{}
	for _, r := range records {
		switch {
		case r.Work != nil:
			keys = append(keys, r.Work.OpenLibrary)
		case r.Edition != nil:
			keys = append(keys, r.Edition.ID)
		case r.Author != nil:
			keys = append(keys, r.Author.ID)
		case r.Redirect != nil:
			keys = append(keys, r.Redirect.Source)
		case r.Deleted != "":
			keys = append(keys, r.Deleted)
		}
	}
	counts := func() ([3]int64, error) {
		var n [3]int64
		err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works WHERE openlibrary_id<>'' AND openlibrary_id=ANY($1::text[])),(SELECT count(*) FROM ingest.ol_editions WHERE id=ANY($1::text[])),(SELECT count(*) FROM ingest.ol_authors WHERE id=ANY($1::text[]))`, keys).Scan(&n[0], &n[1], &n[2])
		return n, err
	}
	before, err := counts()
	if err != nil {
		return err
	}
	batch := &pgx.Batch{}
	saveEntity := func(statement, columns, related string, args ...any) {
		if !refresh {
			batch.Queue(statement, args...)
			return
		}
		batch.Queue(`WITH changed AS (`+statement+` RETURNING `+columns+`),affected AS (
       UPDATE ingest.aio_works w SET revision=revision+1,updated_at=now() WHERE w.media_type='book' AND EXISTS(SELECT 1 FROM changed) AND (`+related+`) RETURNING w.id
      ) INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN affected a ON a.id=m.work_id ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, args...)
	}
	invalidate := func(key string) {
		if !refresh {
			return
		}
		batch.Queue(`WITH affected AS (
     UPDATE ingest.aio_works w SET revision=revision+1,updated_at=now() WHERE w.media_type='book' AND (
      w.openlibrary_id=$1 OR (w.book_data->'author_keys') ?| ingest.ol_aliases($1)
      OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.id=$1 AND e.work_keys && ingest.ol_aliases(w.openlibrary_id))
     ) RETURNING id
    ) INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN affected a ON a.id=m.work_id ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, key)
	}
	nw, ne, na, skipped := int64(0), int64(0), int64(0), int64(0)
	for _, r := range records {
		switch {
		case r.Skipped:
			skipped++
		case r.Redirect != nil:
			rd := r.Redirect
			invalidate(rd.Source)
			batch.Queue(`INSERT INTO ingest.ol_redirects(source,target) VALUES($1,$2) ON CONFLICT(source) DO UPDATE SET target=EXCLUDED.target WHERE ingest.ol_redirects.target<>EXCLUDED.target`, rd.Source, rd.Target)
			batch.Queue(`INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.aio_matches WHERE work_id=(SELECT id FROM ingest.aio_works WHERE openlibrary_id=$1) ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, rd.Source)
			batch.Queue(`DELETE FROM ingest.aio_works WHERE openlibrary_id=$1`, rd.Source)
			batch.Queue(`DELETE FROM ingest.ol_editions WHERE id=$1`, rd.Source)
			batch.Queue(`DELETE FROM ingest.ol_authors WHERE id=$1`, rd.Source)
		case r.Author != nil:
			a := r.Author
			na++
			saveEntity(`INSERT INTO ingest.ol_authors(id,name,aliases,normalized) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,aliases=EXCLUDED.aliases,normalized=EXCLUDED.normalized WHERE (ingest.ol_authors.name,ingest.ol_authors.aliases,ingest.ol_authors.normalized) IS DISTINCT FROM (EXCLUDED.name,EXCLUDED.aliases,EXCLUDED.normalized)`, "id", `(w.book_data->'author_keys') ?| ARRAY(SELECT unnest(ingest.ol_aliases(id)) FROM changed)`, a.ID, a.Name, a.Aliases, books.Normalize(strings.Join(append([]string{a.Name}, a.Aliases...), " ")))
		case r.Work != nil:
			w := r.Work
			nw++
			data, _ := json.Marshal(w.Book)
			batch.Queue(`WITH changed AS (INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,years,display_title,book_data,book_normalized) VALUES('book',NULL,$1,$2,$3,$4,$5)
 ON CONFLICT(openlibrary_id) WHERE openlibrary_id<>'' DO UPDATE SET years=EXCLUDED.years,display_title=EXCLUDED.display_title,book_data=EXCLUDED.book_data,book_normalized=EXCLUDED.book_normalized,
 revision=ingest.aio_works.revision+1,updated_at=now() WHERE (ingest.aio_works.years,ingest.aio_works.display_title,ingest.aio_works.book_data) IS DISTINCT FROM (EXCLUDED.years,EXCLUDED.display_title,EXCLUDED.book_data) RETURNING id) INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN changed ON changed.id=m.work_id ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, w.OpenLibrary, w.Years, w.DisplayTitle, data, books.Normalize(w.DisplayTitle))
		case r.Edition != nil:
			e := r.Edition
			ne++
			if len(e.Works) > 0 && !(len(e.Works) == 1 && e.Works[0] == e.ID) {
				// An orphan edition may later be assigned to a canonical work.
				// Retire its former standalone entry instead of duplicating it.
				if len(e.Works) == 1 {
					batch.Queue(`INSERT INTO ingest.ol_redirects(source,target) SELECT $1,$2 WHERE EXISTS(SELECT 1 FROM ingest.aio_works WHERE openlibrary_id=$1) ON CONFLICT(source) DO UPDATE SET target=EXCLUDED.target`, e.ID, e.Works[0])
				}
				batch.Queue(`INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.aio_matches WHERE work_id=(SELECT id FROM ingest.aio_works WHERE openlibrary_id=$1) ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, e.ID)
				batch.Queue(`DELETE FROM ingest.aio_works WHERE openlibrary_id=$1`, e.ID)
			}
			saveEntity(`INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,isbn,author_keys,languages,year,format,cover) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT(id) DO UPDATE SET work_keys=EXCLUDED.work_keys,title=EXCLUDED.title,normalized=EXCLUDED.normalized,isbn=EXCLUDED.isbn,author_keys=EXCLUDED.author_keys,languages=EXCLUDED.languages,year=EXCLUDED.year,format=EXCLUDED.format,cover=EXCLUDED.cover
 WHERE (ingest.ol_editions.work_keys,ingest.ol_editions.title,ingest.ol_editions.isbn,ingest.ol_editions.author_keys,ingest.ol_editions.languages,ingest.ol_editions.year,ingest.ol_editions.format,ingest.ol_editions.cover)
 IS DISTINCT FROM (EXCLUDED.work_keys,EXCLUDED.title,EXCLUDED.isbn,EXCLUDED.author_keys,EXCLUDED.languages,EXCLUDED.year,EXCLUDED.format,EXCLUDED.cover)`, "work_keys", `EXISTS(SELECT 1 FROM changed c WHERE c.work_keys && ingest.ol_aliases(w.openlibrary_id)) OR EXISTS(SELECT 1 FROM ingest.ol_editions old WHERE old.id=$1 AND old.work_keys && ingest.ol_aliases(w.openlibrary_id))`, e.ID, e.Works, e.Title, e.Normalized, e.ISBN, e.Authors, e.Languages, e.Year, e.Format, e.Cover)
			if len(e.Works) == 1 && e.Works[0] == e.ID {
				data, _ := json.Marshal(model.BookInformation{AuthorKeys: e.Authors, Cover: e.Cover, Languages: e.Languages})
				years := []int{}
				if e.Year != 0 {
					years = append(years, e.Year)
				}
				batch.Queue(`INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,years,display_title,book_data,book_normalized) VALUES('book',NULL,$1,$2,$3,$4,$5) ON CONFLICT(openlibrary_id) WHERE openlibrary_id<>'' DO UPDATE SET years=EXCLUDED.years,display_title=EXCLUDED.display_title,book_data=EXCLUDED.book_data,book_normalized=EXCLUDED.book_normalized,revision=ingest.aio_works.revision+1 WHERE (ingest.aio_works.years,ingest.aio_works.display_title,ingest.aio_works.book_data) IS DISTINCT FROM (EXCLUDED.years,EXCLUDED.display_title,EXCLUDED.book_data)`, e.ID, years, e.Title, data, e.Normalized)
			}
		case r.Deleted != "":
			invalidate(r.Deleted)
			batch.Queue(`INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.aio_matches WHERE work_id=(SELECT id FROM ingest.aio_works WHERE openlibrary_id=$1) ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, r.Deleted)
			batch.Queue(`DELETE FROM ingest.aio_works WHERE openlibrary_id=$1`, r.Deleted)
			batch.Queue(`DELETE FROM ingest.ol_editions WHERE id=$1`, r.Deleted)
			batch.Queue(`DELETE FROM ingest.ol_authors WHERE id=$1`, r.Deleted)
		}
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return databaseError("save book information batch", err)
	}
	after, err := counts()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET position=$1,imported=imported+$2,works=works+$3,editions=editions+$4,authors=authors+$5,skipped=skipped+$6 WHERE singleton`, position, nw+ne+na, after[0]-before[0], after[1]-before[1], after[2]-before[2], skipped)
	if err != nil {
		return err
	}
	return databaseError("commit book information batch", tx.Commit(ctx))
}

func (s *Store) FinishOpenLibrary(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
INSERT INTO ingest.aio_match_queue(provider_id,source_id)
SELECT t.provider_id,t.source_id FROM ingest.torrents t LEFT JOIN ingest.aio_matches m USING(provider_id,source_id) LEFT JOIN ingest.aio_works w ON w.id=m.work_id
WHERE w.media_type='book' OR COALESCE(t.fields->>'media_type',t.fields->>'type','') IN ('book','ebook','audiobook') OR COALESCE(t.fields->'categories',t.fields->'category','null')::text ~ '\m(70[0-9]{2}|3030)\M' OR COALESCE(t.fields->>'category_name','') ~* '\m(books?|livres?|ebooks?|comics?|mangas?)\M' OR t.fields ?| ARRAY['isbn','isbn_10','isbn_13','author','authors'] OR t.fields->>'title' ~* '\m(epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|audiobook|ebook|livre audio)\M'
ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1;
UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1;
UPDATE ingest.ol_import_state SET complete=TRUE,last_sync=now(),checked_at=now(),retry_at=NULL,error='' WHERE singleton;`)
	return databaseError("finish book information import", err)
}

func (s *Store) FailOpenLibrary(ctx context.Context, message string, retryAt time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET error=$1,retry_at=$2 WHERE singleton`, message, retryAt)
	return err
}

func searchBookCandidates(ctx context.Context, tx pgx.Tx, f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
	q := `WITH keys AS (
 SELECT openlibrary_id AS key FROM ingest.aio_works WHERE openlibrary_id<>'' AND openlibrary_id=ingest.ol_resolve($3)
 UNION SELECT unnest(work_keys) FROM ingest.ol_editions WHERE id=ingest.ol_resolve($3)
 UNION SELECT unnest(work_keys) FROM ingest.ol_editions WHERE $2<>'' AND isbn @> ARRAY[$2::text]
 UNION SELECT openlibrary_id FROM ingest.aio_works WHERE $2='' AND $3='' AND media_type='book' AND (book_normalized=ANY($5::text[]) OR (NOT $4 AND book_normalized OPERATOR(public.%) $1))
 UNION SELECT unnest(work_keys) FROM ingest.ol_editions WHERE $2='' AND $3='' AND (normalized=ANY($5::text[]) OR (NOT $4 AND normalized OPERATOR(public.%) $1))
) SELECT ` + aioWorkColumns + ` FROM ingest.aio_works w WHERE w.openlibrary_id IN(SELECT ingest.ol_resolve(key) FROM keys) AND NOT w.adult
 AND ($2='' OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.work_keys && ingest.ol_aliases(w.openlibrary_id) AND e.isbn @> ARRAY[$2::text]))
 AND ($3='' OR w.openlibrary_id=ingest.ol_resolve($3) OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.id=ingest.ol_resolve($3) AND e.work_keys && ingest.ol_aliases(w.openlibrary_id)))
 ORDER BY w.id LIMIT 101`
	rows, err := tx.Query(ctx, q, f.Title, f.ISBN, f.OpenLibrary, f.ExactOnly, append(f.Titles, f.Title))
	if err != nil {
		return nil, false, err
	}
	out := []model.AIOWork{}
	for rows.Next() {
		w, e := scanAIOWork(rows)
		if e != nil {
			return nil, false, e
		}
		out = append(out, w)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, false, databaseError("find book candidates", rows.Err())
	}
	// Fetch aliases relevant to this query rather than an arbitrary first set
	// of editions. A frequently translated work may have thousands of editions.
	for i := range out {
		w := &out[i]
		w.Book.YearVerified = f.Year != nil && slices.Contains(w.Years, *f.Year)
		aliases, err := tx.Query(ctx, `SELECT e.title,e.year,ARRAY(SELECT name FROM ingest.ol_authors WHERE id=ANY(ARRAY(SELECT ingest.ol_resolve(key) FROM unnest(e.author_keys) key)))
FROM ingest.ol_editions e WHERE e.work_keys && ingest.ol_aliases($1)
AND (e.normalized=ANY($2::text[]) OR e.normalized OPERATOR(public.%) $3 OR ($4::int IS NOT NULL AND e.year=$4))
ORDER BY (e.normalized=ANY($2::text[])) DESC,e.id LIMIT 101`, w.OpenLibrary, append(f.Titles, f.Title), f.Title, f.Year)
		if err != nil {
			return nil, false, err
		}
		for aliases.Next() {
			var title string
			var year int
			var authors []string
			if err = aliases.Scan(&title, &year, &authors); err != nil {
				aliases.Close()
				return nil, false, err
			}
			if !slices.Contains(w.Titles, title) {
				w.Titles = append(w.Titles, title)
			}
			if f.Year != nil && year == *f.Year {
				w.Book.YearVerified = true
			}
			for _, a := range authors {
				if !slices.Contains(w.Book.Authors, a) {
					w.Book.Authors = append(w.Book.Authors, a)
				}
			}
		}
		aliases.Close()
		if aliases.Err() != nil {
			return nil, false, aliases.Err()
		}
	}
	return out, len(out) > aioCandidateLimit, nil
}

// Metadata identifiers stay in their own namespace. In particular, an OL ID
// can never be returned to a client in an IMDb or TMDB field.
func validBookQueryType(s string) bool {
	return s == "" || s == "book" || s == "ebook" || s == "audiobook" || s == "movie" || s == "tv"
}

func bookReleaseKindSQL(alias string) string {
	return `CASE WHEN COALESCE(` + alias + `.fields->>'media_type',` + alias + `.fields->>'type','')='audiobook' OR COALESCE(` + alias + `.fields->'categories',` + alias + `.fields->'category','null')::text ~ '\m3030\M' OR COALESCE(` + alias + `.fields->>'category_name','') ~* '\m(audiobooks?|livres?[ ._-]?audio)\M' OR ` + alias + `.fields->>'title' ~* '\m(audiobook|audio[ ._-]?book|livre[ ._-]?audio|m4b|aax)\M' THEN 'audiobook' ELSE 'ebook' END`
}

func (s *Store) OpenLibraryUnchanged(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET last_sync=now(),checked_at=now(),retry_at=NULL,error='' WHERE singleton`)
	return err
}

func (s *Store) ResetBookInformation(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT m.provider_id,m.source_id FROM ingest.aio_matches m JOIN ingest.aio_works w ON w.id=m.work_id WHERE w.media_type='book' ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1;
DELETE FROM ingest.aio_works WHERE media_type='book';
TRUNCATE ingest.ol_authors,ingest.ol_editions,ingest.ol_redirects;
UPDATE ingest.ol_import_state SET snapshot='',etag='',position=0,imported=0,works=0,editions=0,authors=0,skipped=0,last_sync=NULL,checked_at=NULL,retry_at=NULL,error='',complete=FALSE WHERE singleton;
UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1;`)
	return databaseError("reset book information", err)
}
