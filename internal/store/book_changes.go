package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

type BookChangesState struct {
	TailID, AnchorID                       string
	AnchorAt, TailSince, BootstrapDay      *time.Time
	ChangesThrough, LastSync, CheckedAt    *time.Time
	RetryAt, NeedsDumpAfter, DumpCheckedAt *time.Time
	ProviderRetryAt                        *time.Time
	BootstrapComplete                      bool
	Error                                  string
}

type BookChangeJob struct {
	ID                string
	Revision, Version int64
	ChangedAt         time.Time
	Attempts          int
}

func (s *Store) BookChangesState(ctx context.Context) (BookChangesState, error) {
	var v BookChangesState
	err := s.pool.QueryRow(ctx, `SELECT anchor_id,anchor_at,tail_since,bootstrap_day,bootstrap_complete,changes_through,last_sync,checked_at,retry_at,needs_dump_after,dump_checked_at,error,tail_start_id,provider_retry_at FROM ingest.ol_changes_state WHERE singleton`).Scan(&v.AnchorID, &v.AnchorAt, &v.TailSince, &v.BootstrapDay, &v.BootstrapComplete, &v.ChangesThrough, &v.LastSync, &v.CheckedAt, &v.RetryAt, &v.NeedsDumpAfter, &v.DumpCheckedAt, &v.Error, &v.TailID, &v.ProviderRetryAt)
	return v, databaseError("read book change progress", err)
}

func openLibraryVersions(ctx context.Context, tx pgx.Tx, keys []string) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT key,max(revision) FROM (
 SELECT openlibrary_id AS key,ol_revision AS revision FROM ingest.aio_works WHERE openlibrary_id<>'' AND openlibrary_id=ANY($1::text[])
 UNION ALL SELECT id,ol_revision FROM ingest.ol_editions WHERE id=ANY($1::text[])
 UNION ALL SELECT id,ol_revision FROM ingest.ol_authors WHERE id=ANY($1::text[])
 UNION ALL SELECT source,ol_revision FROM ingest.ol_redirects WHERE source=ANY($1::text[])
 UNION ALL SELECT id,ol_revision FROM ingest.ol_deleted WHERE id=ANY($1::text[])
 ) versions GROUP BY key`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := map[string]int64{}
	for rows.Next() {
		var id string
		var revision int64
		if err = rows.Scan(&id, &revision); err != nil {
			return nil, err
		}
		versions[id] = revision
	}
	return versions, rows.Err()
}

func queueOpenLibraryAliases(ctx context.Context, tx pgx.Tx, records []books.Record) error {
	keys := []string{}
	identifiers := []string{}
	for _, record := range records {
		if record.Key() != "" {
			keys = append(keys, record.Key())
			identifiers = append(identifiers, "ol:"+record.Key())
		}
		if record.Edition != nil {
			for _, isbn := range record.Edition.ISBN {
				if canonical := books.ISBN(isbn); canonical != "" {
					identifiers = append(identifiers, "isbn:"+canonical[:12])
				}
			}
		}
	}
	if err := tx.QueryRow(ctx, `SELECT ARRAY(SELECT DISTINCT unnest(ingest.ol_aliases(ingest.ol_resolve(id))) FROM unnest($1::text[]) id)`, keys).Scan(&keys); err != nil {
		return err
	}
	for _, key := range keys {
		identifiers = append(identifiers, "ol:"+key)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.5',true)`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `WITH aliases AS (
 SELECT book_normalized AS normalized FROM ingest.aio_works WHERE media_type='book' AND (openlibrary_id=ANY($1::text[]) OR (book_data->'author_keys') ?| $1::text[])
 UNION SELECT normalized FROM ingest.ol_editions WHERE id=ANY($1::text[]) OR work_keys && $1::text[] OR author_keys && $1::text[]
 ) INSERT INTO ingest.aio_match_queue(provider_id,source_id)
 SELECT DISTINCT m.provider_id,m.source_id FROM aliases a JOIN ingest.aio_matches m
 ON m.normalized=a.normalized OR (m.normalized OPERATOR(public.%) a.normalized AND public.similarity(m.normalized,a.normalized)>=0.5)
 WHERE a.normalized<>''
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, keys)
	if err != nil {
		return databaseError("queue changed book titles", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.aio_match_queue(provider_id,source_id)
 SELECT provider_id,source_id FROM ingest.torrents WHERE (`+nativeBookAssertionPredicate+`)
 AND ingest.aio_native_book_candidates(fields) && ARRAY(
  SELECT unnest($2::text[])
  UNION SELECT 'isbn:'||left(unnest(isbn),12) FROM ingest.ol_editions
   WHERE id=ANY($1::text[]) OR work_keys && $1::text[] OR author_keys && $1::text[]
 )
 ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, keys, identifiers)
	return databaseError("queue changed book identifiers", err)
}

// Queue pages durably before advancing the feed anchor. Replayed pages only
// replace a job when they contain a newer revision of the same document.
func (s *Store) QueueBookChanges(ctx context.Context, keys []books.ChangeKey) error {
	if len(keys) > 250 {
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
	batch := &pgx.Batch{}
	ids := []string{}
	for _, key := range keys {
		ids = append(ids, key.ID)
	}
	versions, err := openLibraryVersions(ctx, tx, ids)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if books.Path(key.ID) == "" || key.Revision < 0 || key.At.IsZero() {
			return model.ErrInvalid
		}
		if revision, exists := versions[key.ID]; exists && revision >= key.Revision {
			continue
		}
		batch.Queue(`INSERT INTO ingest.ol_change_jobs(id,revision,changed_at) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET revision=GREATEST(ingest.ol_change_jobs.revision,EXCLUDED.revision),changed_at=GREATEST(ingest.ol_change_jobs.changed_at,EXCLUDED.changed_at),version=ingest.ol_change_jobs.version+1,attempts=0,next_attempt=now()
 WHERE EXCLUDED.revision>ingest.ol_change_jobs.revision`, key.ID, key.Revision, key.At)
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return databaseError("save book change jobs", err)
	}
	return databaseError("commit book change jobs", tx.Commit(ctx))
}

func (s *Store) AdvanceBookAnchor(ctx context.Context, expected string, head books.Change) error {
	if head.ID == "" || head.At.IsZero() {
		return model.ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET anchor_id=$2,anchor_at=$3,tail_start_id=CASE WHEN tail_since IS NULL THEN $2 ELSE tail_start_id END,tail_since=COALESCE(tail_since,$3),checked_at=now(),retry_at=NULL,error='' WHERE singleton AND anchor_id=$1`, expected, head.ID, head.At)
	if err == nil && tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return databaseError("advance book change anchor", err)
}

func (s *Store) RequireBookExport(ctx context.Context, after time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET needs_dump_after=GREATEST(needs_dump_after,$1),bootstrap_complete=FALSE,dump_checked_at=NULL WHERE singleton`, after)
	return databaseError("mark book catalog recovery", err)
}

func (s *Store) RestartBookAnchor(ctx context.Context, expected string, head books.Change) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET anchor_id=$2,anchor_at=$3,tail_since=$3,tail_start_id=$2,checked_at=now(),retry_at=NULL,error='',needs_dump_after=GREATEST(needs_dump_after,$3),bootstrap_complete=FALSE,dump_checked_at=NULL WHERE singleton AND anchor_id=$1`, expected, head.ID, head.At)
	if err == nil && tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return databaseError("recover book change continuity", err)
}

func (s *Store) SetBookSnapshotDate(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_import_state SET snapshot_at=$1 WHERE singleton AND snapshot_at IS NULL`, at)
	return err
}

func (s *Store) BookFeedFailed(ctx context.Context, retry time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET retry_at=GREATEST(retry_at,$1),error='Book changes could not be retrieved. Saved progress is preserved.' WHERE singleton`, retry)
	return err
}

func (s *Store) BookExportChecked(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET dump_checked_at=now() WHERE singleton`)
	return err
}

func (s *Store) BeginBookCatchup(ctx context.Context, snapshot time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET bootstrap_day=($1::timestamptz AT TIME ZONE 'UTC')::date,
 bootstrap_complete=(tail_since IS NOT NULL AND tail_since<=$1),
 needs_dump_after=CASE WHEN needs_dump_after<=$1 THEN NULL ELSE needs_dump_after END
 WHERE singleton`, snapshot)
	return err
}

func (s *Store) AdvanceBookCatchup(ctx context.Context, expected, next time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET bootstrap_day=($2::timestamptz AT TIME ZONE 'UTC')::date,
 bootstrap_complete=(tail_since IS NOT NULL AND ($2::timestamptz AT TIME ZONE 'UTC')::date>=(tail_since AT TIME ZONE 'UTC')::date+1)
 WHERE singleton AND bootstrap_day=($1::timestamptz AT TIME ZONE 'UTC')::date`, expected, next)
	if err == nil && tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return err
}

func (s *Store) BookChangeJobs(ctx context.Context, limit int) ([]BookChangeJob, error) {
	return s.bookChangeJobs(ctx, limit, false)
}

func (s *Store) OldestBookChangeJobs(ctx context.Context, limit int) ([]BookChangeJob, error) {
	return s.bookChangeJobs(ctx, limit, true)
}

func (s *Store) bookChangeJobs(ctx context.Context, limit int, oldest bool) ([]BookChangeJob, error) {
	if limit < 1 || limit > 250 {
		return nil, model.ErrInvalid
	}
	order := "changed_at DESC,id DESC"
	if oldest {
		order = "changed_at,id"
	}
	rows, err := s.pool.Query(ctx, `SELECT id,revision,version,changed_at,attempts FROM ingest.ol_change_jobs WHERE next_attempt<=now() ORDER BY `+order+` LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []BookChangeJob{}
	for rows.Next() {
		var job BookChangeJob
		if err = rows.Scan(&job.ID, &job.Revision, &job.Version, &job.ChangedAt, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) AcknowledgeKnownBookChange(ctx context.Context, job BookChangeJob) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ingest.ol_change_jobs WHERE id=$1 AND version=$2 AND EXISTS(
 SELECT 1 FROM ingest.aio_works WHERE openlibrary_id=$1 AND openlibrary_id<>'' AND ol_revision>=$3
 UNION ALL SELECT 1 FROM ingest.ol_editions WHERE id=$1 AND ol_revision>=$3
 UNION ALL SELECT 1 FROM ingest.ol_authors WHERE id=$1 AND ol_revision>=$3
 UNION ALL SELECT 1 FROM ingest.ol_redirects WHERE source=$1 AND ol_revision>=$3
 UNION ALL SELECT 1 FROM ingest.ol_deleted WHERE id=$1 AND ol_revision>=$3
 )`, job.ID, job.Version, job.Revision)
	return tag.RowsAffected() > 0, err
}

func (s *Store) DeferBookChange(ctx context.Context, job BookChangeJob, delay time.Duration) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_change_jobs SET attempts=attempts+1,next_attempt=now()+make_interval(secs=>$3) WHERE id=$1 AND version=$2`, job.ID, job.Version, delay.Seconds())
	return err
}

// A changed record and its versioned job acknowledgement publish atomically.
// The collector may concurrently replace the job with a newer revision.
func (s *Store) ApplyBookChange(ctx context.Context, job BookChangeJob, record books.Record) error {
	if record.Key() != job.ID || record.Revision < job.Revision || (record.Revision < 1 && (job.Revision != 0 || record.Deleted == "" || job.Attempts < 1)) {
		return model.ErrInvalid
	}
	if record.Modified == nil || record.Modified.Before(job.ChangedAt) {
		record.Modified = &job.ChangedAt
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return err
	}
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM ingest.ol_import_state WHERE singleton FOR UPDATE`).Scan(&generation); err != nil {
		return err
	}
	delta, _, _, err := saveOpenLibraryRecords(ctx, tx, []books.Record{record}, true, generation)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.ol_import_state SET works=works+$1,editions=editions+$2,authors=authors+$3 WHERE singleton`, delta[0], delta[1], delta[2]); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.ol_change_jobs WHERE id=$1 AND version=$2`, job.ID, job.Version); err != nil {
		return err
	}
	return databaseError("publish book change", tx.Commit(ctx))
}

func (s *Store) BookDependencies(ctx context.Context, record books.Record) ([]string, error) {
	keys := []string{}
	switch {
	case record.Work != nil:
		keys = append(keys, record.Work.Book.AuthorKeys...)
	case record.Edition != nil:
		keys = append(keys, record.Edition.Works...)
		keys = append(keys, record.Edition.Authors...)
	case record.Redirect != nil:
		keys = append(keys, record.Redirect.Target)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	versions, err := openLibraryVersions(ctx, tx, keys)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, key := range keys {
		if key != record.Key() {
			if _, exists := versions[key]; !exists {
				out = append(out, key)
			}
		}
	}
	return out, tx.Commit(ctx)
}

func (s *Store) FinishBookChanges(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE ingest.ol_changes_state SET changes_through=anchor_at,last_sync=now()
 WHERE singleton AND bootstrap_complete AND needs_dump_after IS NULL AND error='' AND anchor_at IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM ingest.ol_change_jobs);
 UPDATE ingest.ol_import_state SET error='',retry_at=NULL,sync_failures=0 WHERE singleton;
 UPDATE ingest.ol_changes_state SET provider_retry_at=NULL WHERE provider_retry_at<=now();`)
	return databaseError("finish book change update", err)
}
