package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

type AIOCandidateSearch func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error)

const aioCandidateLimit = 100

// A bounded transaction shares candidate lookups between release variants.
// Keep batches small so metadata publication can acquire its exclusive fence.
const AIOMaxMatchBatchSize = 100

func searchAIOCandidates(ctx context.Context, tx pgx.Tx, filter model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
	rows, err := tx.Query(ctx, aioCandidatesQuery, filter.Title, filter.TMDB, filter.IMDb, filter.TVDB, filter.Type, filter.Year, filter.Seasons, filter.Episode, filter.ExactOnly)
	if err != nil {
		return nil, false, databaseError("find metadata candidates", err)
	}
	defer rows.Close()
	out := []model.AIOWork{}
	for rows.Next() {
		w, err := scanAIOWork(rows)
		if err != nil {
			return nil, false, databaseError("read metadata candidate", err)
		}
		out = append(out, w)
	}
	return out, len(out) > aioCandidateLimit, databaseError("find metadata candidates", rows.Err())
}

// Matching keeps a small, shared metadata snapshot through discovery and
// publication. Workers partition their picks, and SKIP LOCKED handles source
// edits without holding an HTTP request or losing a queued observation.
func (s *Store) MatchAIOBatch(ctx context.Context, picks []model.AIORecord, decide func(model.AIORecord, AIOCandidateSearch) (model.AIOMatchResult, error)) ([]model.AIOMatchResult, error) {
	if len(picks) == 0 {
		return nil, nil
	}
	if len(picks) > AIOMaxMatchBatchSize || decide == nil {
		return nil, model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, databaseError("begin correspondence batch", err)
	}
	defer rollback(tx)
	var generation int64
	if err = tx.QueryRow(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR SHARE`).Scan(&generation); err != nil {
		return nil, databaseError("lock candidate generation", err)
	}
	var policy model.MatchingPolicy
	if err = tx.QueryRow(ctx, `SELECT (values->>'minimum_similarity')::int,(values->>'ambiguity_margin')::int FROM ingest.runtime_settings WHERE section='matching'`).Scan(&policy.MinimumSimilarity, &policy.AmbiguityMargin); err != nil {
		return nil, databaseError("read identification policy", err)
	}
	if !policy.Valid() {
		return nil, model.ErrInvalid
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.5',true)`); err != nil {
		return nil, err
	}
	providers, ids := []string{}, []string{}
	for _, pick := range picks {
		providers, ids = append(providers, pick.Provider), append(ids, pick.SourceID)
	}
	rows, err := tx.Query(ctx, `SELECT `+aioRecordColumns+`,q.version,
 r.active IS TRUE,COALESCE(r.revision,0),COALESCE(r.fingerprint,''),COALESCE(r.media_type,''),COALESCE(r.tmdb_id,''),r.season,r.episode,
 COALESCE(r.override_episode,FALSE),COALESCE(r.updated_at,t.last_seen_at),
 CASE WHEN r.active THEN r.evidence_fingerprint<>md5(ingest.aio_review_evidence(t.fields)::text) ELSE FALSE END
FROM ingest.aio_match_queue q JOIN ingest.torrents t USING(provider_id,source_id)
LEFT JOIN ingest.aio_match_reviews r USING(provider_id,source_id)
WHERE (t.provider_id,t.source_id) IN(SELECT * FROM unnest($1::text[],$2::text[]))
ORDER BY t.provider_id,t.source_id FOR UPDATE OF t SKIP LOCKED`, providers, ids)
	if err != nil {
		return nil, databaseError("lock queued correspondences", err)
	}
	records := []model.AIORecord{}
	reviews := []*model.AIOMatchReview{}
	for rows.Next() {
		var review model.AIOMatchReview
		r, err := scanAIORecordQueue(aioStreamRow{scanner: rows, ids: []any{&review.Active, &review.Revision, &review.Fingerprint, &review.Type, &review.TMDB, &review.Season, &review.Episode, &review.OverrideEpisode, &review.UpdatedAt, &review.SourceChanged}}, true)
		if err != nil {
			rows.Close()
			return nil, err
		}
		r.CandidateGeneration = generation
		r.MatchingPolicy = policy
		records = append(records, r)
		reviews = append(reviews, &review)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, databaseError("read locked correspondences", err)
	}
	// Repeated releases often ask for the same candidates. Reuse only within
	// this small batch's metadata fence, never across commits or generations.
	// Every filter field is in the key, including numbering and missing years.
	type candidateResult struct {
		works    []model.AIOWork
		overflow bool
	}
	candidates := map[string]candidateResult{}
	search := func(filter model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
		key, err := json.Marshal(filter)
		if err != nil {
			return nil, false, err
		}
		if previous, ok := candidates[string(key)]; ok {
			return previous.works, previous.overflow, nil
		}
		works, overflow, err := searchAIOCandidates(ctx, tx, filter)
		if err == nil {
			candidates[string(key)] = candidateResult{works, overflow}
		}
		return works, overflow, err
	}
	results := make([]model.AIOMatchResult, 0, len(records))
	seasons := []model.AIOSeasonRequest{}
	for i, record := range records {
		reviewed, err := reviewedAIOResult(ctx, tx, record, reviews[i])
		if err != nil {
			return nil, err
		}
		var result model.AIOMatchResult
		if reviewed != nil {
			result = *reviewed
		} else {
			result, err = decide(record, search)
		}
		if err != nil {
			return nil, err
		}
		if result.Record.Provider != record.Provider || result.Record.SourceID != record.SourceID || result.Record.Fingerprint != record.Fingerprint || result.Record.QueueVersion != record.QueueVersion || result.Record.CandidateGeneration != generation || (result.Status == "matched") != (result.Work != nil) {
			return nil, model.ErrInvalid
		}
		if result.Work != nil {
			var revision int64
			var adult bool
			if err := tx.QueryRow(ctx, `SELECT revision,adult FROM ingest.aio_works WHERE id=$1`, result.Work.ID).Scan(&revision, &adult); err != nil || revision != result.Work.Revision || adult {
				return nil, model.ErrConflict
			}
		}
		results, seasons = append(results, result), append(seasons, result.Seasons...)
	}
	batch := &pgx.Batch{}
	for _, result := range results {
		r := result.Record
		var workID any
		var revision int64
		if result.Work != nil {
			workID, revision = result.Work.ID, result.Work.Revision
		}
		batch.Queue(`INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,normalized)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(provider_id,source_id) DO UPDATE SET
fingerprint=EXCLUDED.fingerprint,work_id=EXCLUDED.work_id,revision=EXCLUDED.revision,status=EXCLUDED.status,reason=EXCLUDED.reason,normalized=EXCLUDED.normalized,updated_at=now()`, r.Provider, r.SourceID, r.Fingerprint, workID, revision, result.Status, result.Reason, r.Normalized)
		batch.Queue(`DELETE FROM ingest.aio_match_queue WHERE provider_id=$1 AND source_id=$2 AND version=$3`, r.Provider, r.SourceID, r.QueueVersion)
	}
	if err = lockCatalogEntityWrites(ctx, tx); err != nil {
		return nil, err
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return nil, databaseError("save correspondence batch", err)
	}
	if err = requestAIOSeasons(ctx, tx, seasons); err != nil {
		return nil, databaseError("queue required season information", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, databaseError("publish correspondence batch", err)
	}
	return results, nil
}

// Leave room for API/recovery work. Admission locks use dedicated connections.
// Small database pools still retain at least one connection for publication.
func (s *Store) AIOMatchWorkers() int {
	return max(1, min(10, int(s.pool.Config().MaxConns)-3))
}

func requestAIOSeasons(ctx context.Context, tx pgx.Tx, requests []model.AIOSeasonRequest) error {
	if len(requests) == 0 {
		return nil
	}
	ids, seasons := []string{}, []int{}
	for _, request := range requests {
		if request.TMDB == "" || request.Season < 0 || request.Season > 999 {
			return model.ErrInvalid
		}
		ids, seasons = append(ids, request.TMDB), append(seasons, request.Season)
	}
	_, err := tx.Exec(ctx, `WITH inserted AS (
INSERT INTO ingest.aio_season_requests(tmdb_id,season) SELECT DISTINCT * FROM unnest($1::text[],$2::int[])
ON CONFLICT DO NOTHING RETURNING tmdb_id)
INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id) SELECT DISTINCT 'tv',tmdb_id FROM inserted
ON CONFLICT(media_type,tmdb_id) DO UPDATE SET version=ingest.aio_metadata_jobs.version+1,next_attempt=now()`, ids, seasons)
	return err
}
