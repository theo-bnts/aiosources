package store

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const reviewColumns = `active,revision,fingerprint,media_type,tmdb_id,season,episode,override_episode,updated_at`

func readAIOReview(ctx context.Context, tx pgx.Tx, record model.AIORecord) (*model.AIOMatchReview, error) {
	var review model.AIOMatchReview
	err := tx.QueryRow(ctx, `SELECT `+reviewColumns+`,evidence_fingerprint<>md5(ingest.aio_review_evidence(t.fields)::text) FROM ingest.aio_match_reviews r JOIN ingest.torrents t USING(provider_id,source_id) WHERE r.provider_id=$1 AND r.source_id=$2`, record.Provider, record.SourceID).Scan(&review.Active, &review.Revision, &review.Fingerprint, &review.Type, &review.TMDB, &review.Season, &review.Episode, &review.OverrideEpisode, &review.UpdatedAt, &review.SourceChanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &review, databaseError("read identification review", err)
}

func (s *Store) AIOReviewDetail(ctx context.Context, occurrence string) (model.AIOReviewDetail, error) {
	var detail model.AIOReviewDetail
	if !ValidOccurrenceID(occurrence) {
		return detail, model.ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return detail, databaseError("begin identification review", err)
	}
	defer rollback(tx)
	detail.Record, err = scanAIORecord(tx.QueryRow(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t WHERE occurrence_id=$1`, occurrence))
	if errors.Is(err, pgx.ErrNoRows) {
		return detail, model.ErrNotFound
	}
	if err != nil {
		return detail, databaseError("read review torrent", err)
	}
	if detail.Review, err = readAIOReview(ctx, tx, detail.Record); err != nil {
		return detail, err
	}
	w, err := scanAIOWork(tx.QueryRow(ctx, `SELECT `+aioWorkColumns+` FROM ingest.aio_matches m JOIN ingest.aio_works w ON w.id=m.work_id WHERE m.provider_id=$1 AND m.source_id=$2 AND NOT w.adult AND m.status='matched' AND m.fingerprint=$3 AND m.revision=w.revision AND NOT EXISTS(SELECT 1 FROM ingest.aio_match_queue q WHERE q.provider_id=m.provider_id AND q.source_id=m.source_id)`, detail.Record.Provider, detail.Record.SourceID, detail.Record.Fingerprint))
	if err == nil {
		detail.Work = &w
		compactReviewWork(detail.Work)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return detail, databaseError("read identified work", err)
	}
	return detail, databaseError("finish identification review", tx.Commit(ctx))
}

// Episodes and season years are used internally, but the chooser needs only
// titles, dates and verified identifiers. Keep even very long series compact.
func compactReviewWork(w *model.AIOWork) {
	w.Titles = w.Titles[:min(6, len(w.Titles))]
	w.SeasonYears, w.EpisodeYears = nil, nil
}

var reviewIMDb = regexp.MustCompile(`^tt[0-9]{1,15}$`)
var reviewNumeric = regexp.MustCompile(`^[0-9]{1,15}$`)

func (s *Store) AIOReviewCandidates(ctx context.Context, query, normalized, kind string) ([]model.AIOWork, error) {
	out := []model.AIOWork{}
	query = strings.TrimSpace(query)
	if len(query) > 200 || (kind != "" && kind != "movie" && kind != "tv") {
		return out, model.ErrInvalid
	}
	if len([]rune(normalized)) < 3 && !reviewIMDb.MatchString(query) && !reviewNumeric.MatchString(query) {
		return out, model.ErrInvalid
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(normalized) + "%"
	rows, err := s.pool.Query(ctx, `WITH candidates AS (
 SELECT id FROM ingest.aio_works WHERE imdb_id=$1 OR tmdb_id=$1 OR tvdb_id=$1
 UNION SELECT work_id FROM ingest.aio_titles WHERE $2<>'' AND normalized LIKE $3 ESCAPE '\'
) SELECT `+aioWorkColumns+` FROM candidates c JOIN ingest.aio_works w ON w.id=c.id
 WHERE NOT w.adult AND ($4='' OR w.media_type=$4)
 ORDER BY CASE WHEN w.imdb_id=$1 OR w.tmdb_id=$1 OR w.tvdb_id=$1 THEN 0 ELSE 1 END,w.id LIMIT 40`, query, normalized, pattern, kind)
	if err != nil {
		return out, databaseError("search review candidates", err)
	}
	defer rows.Close()
	for rows.Next() {
		w, err := scanAIOWork(rows)
		if err != nil {
			return out, databaseError("read review candidate", err)
		}
		compactReviewWork(&w)
		out = append(out, w)
	}
	return out, databaseError("search review candidates", rows.Err())
}

// Caller owns the metadata generation lock and the torrent row lock. A source
// edit never silently carries a reviewed decision over to different evidence.
func reviewedAIOMatch(ctx context.Context, tx pgx.Tx, record model.AIORecord) (*model.AIOMatchResult, error) {
	review, err := readAIOReview(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	return reviewedAIOResult(ctx, tx, record, review)
}

func reviewedAIOResult(ctx context.Context, tx pgx.Tx, record model.AIORecord, review *model.AIOMatchReview) (*model.AIOMatchResult, error) {
	if review == nil || !review.Active {
		return nil, nil
	}
	result := &model.AIOMatchResult{Record: record, Status: "ambiguous", Reason: "reviewed information unavailable"}
	if review.SourceChanged {
		result.Reason = "reviewed source changed"
		return result, nil
	}
	w, err := scanAIOWork(tx.QueryRow(ctx, `SELECT `+aioWorkColumns+` FROM ingest.aio_works w WHERE media_type=$1 AND tmdb_id=$2 AND NOT adult`, review.Type, review.TMDB))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, databaseError("verify reviewed information", err)
	}
	result.Status, result.Reason, result.Work = "matched", "manually reviewed", &w
	return result, nil
}

func (s *Store) SaveAIOReview(ctx context.Context, occurrence string, input model.AIOReviewInput) error {
	if !ValidOccurrenceID(occurrence) || len(input.Fingerprint) != 32 || input.Revision < 0 ||
		(input.Automatic && (input.WorkID != 0 || input.WorkRevision != 0 || input.OverrideEpisode || input.Season != nil || input.Episode != nil)) ||
		(!input.Automatic && (input.WorkID < 1 || input.WorkRevision < 1)) ||
		(!input.OverrideEpisode && (input.Season != nil || input.Episode != nil)) ||
		(input.Season != nil && (*input.Season < 0 || *input.Season > 999)) ||
		(input.Episode != nil && (*input.Episode < 1 || *input.Episode > 99999 || input.Season == nil)) {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin review correction", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR SHARE`); err != nil {
		return databaseError("lock reviewed information", err)
	}
	record, err := scanAIORecord(tx.QueryRow(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t WHERE occurrence_id=$1 FOR UPDATE`, occurrence))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return databaseError("lock review torrent", err)
	}
	if record.Fingerprint != input.Fingerprint {
		return model.ErrConflict
	}
	review, err := readAIOReview(ctx, tx, record)
	if err != nil {
		return err
	}
	version := int64(0)
	if review != nil {
		version = review.Revision
	}
	if version != input.Revision {
		return model.ErrConflict
	}
	if input.Automatic {
		if review == nil || !review.Active {
			return model.ErrConflict
		}
		if _, err = tx.Exec(ctx, `UPDATE ingest.aio_match_reviews SET active=FALSE,revision=revision+1,updated_at=now() WHERE provider_id=$1 AND source_id=$2`, record.Provider, record.SourceID); err != nil {
			return databaseError("undo identification review", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_match_queue(provider_id,source_id) VALUES($1,$2) ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, record.Provider, record.SourceID); err != nil {
			return databaseError("queue automatic identification", err)
		}
	} else {
		w, err := scanAIOWork(tx.QueryRow(ctx, `SELECT `+aioWorkColumns+` FROM ingest.aio_works w WHERE id=$1 AND NOT adult`, input.WorkID))
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrConflict
		}
		if err != nil {
			return databaseError("verify review candidate", err)
		}
		if w.Revision != input.WorkRevision {
			return model.ErrConflict
		}
		if w.Type != "tv" && (input.Season != nil || input.Episode != nil) {
			return model.ErrInvalid
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,override_episode,season,episode)
 SELECT $1,$2,$3,md5(ingest.aio_review_evidence(fields)::text),$4,$5,$6,$7,$8 FROM ingest.torrents WHERE provider_id=$1 AND source_id=$2
 ON CONFLICT(provider_id,source_id) DO UPDATE SET active=TRUE,revision=ingest.aio_match_reviews.revision+1,fingerprint=EXCLUDED.fingerprint,evidence_fingerprint=EXCLUDED.evidence_fingerprint,media_type=EXCLUDED.media_type,tmdb_id=EXCLUDED.tmdb_id,override_episode=EXCLUDED.override_episode,season=EXCLUDED.season,episode=EXCLUDED.episode,updated_at=now()`, record.Provider, record.SourceID, record.Fingerprint, w.Type, w.TMDB, input.OverrideEpisode, input.Season, input.Episode); err != nil {
			return databaseError("save identification review", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 VALUES($1,$2,$3,$4,$5,'matched','manually reviewed') ON CONFLICT(provider_id,source_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,work_id=EXCLUDED.work_id,revision=EXCLUDED.revision,status='matched',reason='manually reviewed',updated_at=now()`, record.Provider, record.SourceID, record.Fingerprint, w.ID, w.Revision); err != nil {
			return databaseError("publish reviewed identification", err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM ingest.aio_match_queue WHERE provider_id=$1 AND source_id=$2`, record.Provider, record.SourceID); err != nil {
			return databaseError("finish reviewed identification", err)
		}
		if input.Season != nil && w.Type == "tv" {
			if err = requestAIOSeasons(ctx, tx, []model.AIOSeasonRequest{{TMDB: w.TMDB, Season: *input.Season}}); err != nil {
				return databaseError("queue reviewed season", err)
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,provider_id,service,data)
 VALUES('review-'||$1||'-'||($2::bigint)::text,'info','metadata.identification_reviewed',$3,$4,'',jsonb_build_object('automatic',$5::boolean))`, occurrence, version+1, map[bool]string{true: "Identification returned to automatic matching.", false: "Identification reviewed and corrected."}[input.Automatic], record.Provider, input.Automatic)
	if err != nil {
		return databaseError("record identification correction", err)
	}
	return databaseError("commit identification correction", tx.Commit(ctx))
}

// Apply only a review made against this exact published record. The file list
// remains the final authority for playback, even after correcting numbering.
func (s *Store) applyAIOEpisodeReviews(ctx context.Context, records []model.AIORecord) error {
	if len(records) == 0 {
		return nil
	}
	providers, ids := []string{}, []string{}
	for _, record := range records {
		providers = append(providers, record.Provider)
		ids = append(ids, record.SourceID)
	}
	rows, err := s.pool.Query(ctx, `SELECT r.provider_id,r.source_id,t.fields_fingerprint,r.season,r.episode FROM ingest.aio_match_reviews r JOIN ingest.torrents t USING(provider_id,source_id)
 WHERE r.active AND r.override_episode AND r.evidence_fingerprint=md5(ingest.aio_review_evidence(t.fields)::text) AND (r.provider_id,r.source_id) IN(SELECT * FROM unnest($1::text[],$2::text[]))`, providers, ids)
	if err != nil {
		return databaseError("read reviewed episode numbers", err)
	}
	defer rows.Close()
	for rows.Next() {
		var provider, id, fingerprint string
		var season, episode *int
		if err = rows.Scan(&provider, &id, &fingerprint, &season, &episode); err != nil {
			return err
		}
		for i := range records {
			if records[i].Provider == provider && records[i].SourceID == id && records[i].Fingerprint == fingerprint {
				records[i].Season, records[i].Episode = season, episode
				records[i].ReviewedEpisode = true
			}
		}
	}
	return databaseError("apply reviewed episode numbers", rows.Err())
}
