package store

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

type AIOMetadataPublication struct {
	Work       model.AIOWork
	Normalized []string
	Job        *AIOJob
}

type aioMetadataRow struct {
	pgx.Row
	aliases *[]string
}

func (r aioMetadataRow) Scan(dest ...any) error { return r.Row.Scan(append(dest, r.aliases)...) }

func aioWorkIdentity(w model.AIOWork) string { return w.Type + ":" + w.TMDB }

// Only decision-relevant data advances the revision. A routine refresh, alias
// ordering or duplicated release year must not withdraw working streams.
func aioWorkSignature(w model.AIOWork, normalized []string) string {
	aliases := append([]string{}, normalized...)
	sort.Strings(aliases)
	aliases = deduplicateStrings(aliases)
	titles := append([]string{}, w.Titles...)
	sort.Strings(titles)
	titles = deduplicateStrings(titles)
	years := append([]int{}, w.Years...)
	sort.Ints(years)
	uniqueYears := years[:0]
	for _, year := range years {
		if len(uniqueYears) == 0 || uniqueYears[len(uniqueYears)-1] != year {
			uniqueYears = append(uniqueYears, year)
		}
	}
	if w.SeasonYears == nil {
		w.SeasonYears = map[int]int{}
	}
	if w.EpisodeYears == nil {
		w.EpisodeYears = map[string]int{}
	}
	b, _ := json.Marshal(struct {
		IMDb, TVDB string
		Adult      bool
		Titles     []string
		Normalized []string
		Years      []int
		Seasons    map[int]int
		Episodes   map[string]int
	}{w.IMDb, w.TVDB, w.Adult, titles, aliases, uniqueYears, w.SeasonYears, w.EpisodeYears})
	return string(b)
}

func deduplicateStrings(sorted []string) []string {
	out := sorted[:0]
	for _, value := range sorted {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

// Metadata, affected matching jobs and versioned acknowledgements publish in
// one small transaction. The state row is always locked before works, queue
// rows or torrents; matching takes the shared side of this same fence.
func (s *Store) SaveAIOMetadataBatch(ctx context.Context, inputs []AIOMetadataPublication) error {
	if len(inputs) == 0 {
		return nil
	}
	if len(inputs) > 100 {
		return model.ErrInvalid
	}
	keys := make([]string, len(inputs))
	types, tmdbIDs := []string{}, []string{}
	seen := map[string]bool{}
	for i := range inputs {
		w := &inputs[i].Work
		keys[i] = aioWorkIdentity(*w)
		if w.TMDB == "" || (w.Type != "movie" && w.Type != "tv") || len(w.Titles) != len(inputs[i].Normalized) || seen[keys[i]] {
			return model.ErrInvalid
		}
		seen[keys[i]] = true
		types, tmdbIDs = append(types, w.Type), append(tmdbIDs, w.TMDB)
		if w.Years == nil {
			w.Years = []int{}
		}
		if job := inputs[i].Job; job != nil && (job.Type != w.Type || job.ID != w.TMDB) {
			return model.ErrInvalid
		}
		for key := range w.EpisodeYears {
			parts := strings.Split(key, ":")
			if len(parts) != 2 {
				return model.ErrInvalid
			}
			if _, err := strconv.Atoi(parts[0]); err != nil {
				return model.ErrInvalid
			}
			if _, err := strconv.Atoi(parts[1]); err != nil {
				return model.ErrInvalid
			}
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin metadata publication", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT metadata_generation FROM ingest.aio_state FOR UPDATE`); err != nil {
		return databaseError("lock metadata publication", err)
	}
	rows, err := tx.Query(ctx, `SELECT `+aioWorkColumns+`,ARRAY(SELECT normalized FROM ingest.aio_titles WHERE work_id=w.id) FROM ingest.aio_works w WHERE (w.media_type,w.tmdb_id) IN(SELECT * FROM unnest($1::text[],$2::text[]))`, types, tmdbIDs)
	if err != nil {
		return databaseError("read prior metadata", err)
	}
	previous := map[string]string{}
	previousAliases := map[string][]string{}
	for rows.Next() {
		var aliases []string
		w, err := scanAIOWork(aioMetadataRow{Row: rows, aliases: &aliases})
		if err != nil {
			rows.Close()
			return databaseError("read prior metadata", err)
		}
		key := aioWorkIdentity(w)
		previous[key], previousAliases[key] = aioWorkSignature(w, aliases), aliases
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return databaseError("read prior metadata", err)
	}
	changedKeys := []string{}
	changed := map[string]bool{}
	works := make([]model.AIOWork, len(inputs))
	for i, input := range inputs {
		works[i] = input.Work
		if previous[keys[i]] != aioWorkSignature(input.Work, input.Normalized) {
			changedKeys = append(changedKeys, keys[i])
			changed[keys[i]] = true
		}
	}
	body, err := json.Marshal(works)
	if err != nil {
		return model.ErrInvalid
	}
	rows, err = tx.Query(ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,tvdb_id,years,adult)
SELECT type,tmdb,imdb,tvdb,years,adult FROM jsonb_to_recordset($1::jsonb) AS i(type text,tmdb text,imdb text,tvdb text,years int[],adult boolean) ORDER BY type,tmdb
ON CONFLICT(media_type,tmdb_id) DO UPDATE SET imdb_id=EXCLUDED.imdb_id,tvdb_id=EXCLUDED.tvdb_id,years=EXCLUDED.years,adult=EXCLUDED.adult,
revision=ingest.aio_works.revision+CASE WHEN (EXCLUDED.media_type||':'||EXCLUDED.tmdb_id)=ANY($2::text[]) THEN 1 ELSE 0 END,updated_at=now()
RETURNING id,media_type,tmdb_id`, body, changedKeys)
	if err != nil {
		return databaseError("save metadata batch", err)
	}
	byKey := map[string]int64{}
	for rows.Next() {
		var id int64
		var typ, tmdb string
		if err := rows.Scan(&id, &typ, &tmdb); err != nil {
			rows.Close()
			return databaseError("read metadata identity", err)
		}
		byKey[typ+":"+tmdb] = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return databaseError("read metadata identities", err)
	}
	changedIDs := []int64{}
	for _, key := range changedKeys {
		changedIDs = append(changedIDs, byKey[key])
	}
	for _, table := range []string{"aio_titles", "aio_seasons", "aio_episodes"} {
		if len(changedIDs) > 0 {
			if _, err = tx.Exec(ctx, "DELETE FROM ingest."+table+" WHERE work_id=ANY($1::bigint[])", changedIDs); err != nil {
				return databaseError("replace metadata details", err)
			}
		}
	}
	titles, seasons, episodes := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for i, input := range inputs {
		if !changed[keys[i]] {
			continue
		}
		id := byKey[keys[i]]
		for n, title := range input.Work.Titles {
			titles = append(titles, map[string]any{"work_id": id, "title": title, "normalized": input.Normalized[n]})
		}
		for season, year := range input.Work.SeasonYears {
			seasons = append(seasons, map[string]any{"work_id": id, "season": season, "year": year})
		}
		for key, year := range input.Work.EpisodeYears {
			parts := strings.Split(key, ":")
			season, _ := strconv.Atoi(parts[0])
			episode, _ := strconv.Atoi(parts[1])
			episodes = append(episodes, map[string]any{"work_id": id, "season": season, "episode": episode, "year": year})
		}
	}
	for _, detail := range []struct {
		table string
		rows  []map[string]any
	}{{"aio_titles", titles}, {"aio_seasons", seasons}, {"aio_episodes", episodes}} {
		if len(detail.rows) == 0 {
			continue
		}
		encoded, err := json.Marshal(detail.rows)
		if err != nil {
			return model.ErrInvalid
		}
		if _, err = tx.Exec(ctx, "INSERT INTO ingest."+detail.table+" SELECT * FROM jsonb_populate_recordset(NULL::ingest."+detail.table+",$1::jsonb) ON CONFLICT DO NOTHING", encoded); err != nil {
			return databaseError("save metadata details", err)
		}
	}
	if len(changedKeys) > 0 {
		aliases := []string{}
		for i, input := range inputs {
			if changed[keys[i]] {
				aliases = append(aliases, previousAliases[keys[i]]...)
				aliases = append(aliases, input.Normalized...)
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1`); err != nil {
			return databaseError("advance metadata generation", err)
		}
		if err = queueAIOManyAliases(ctx, tx, changedIDs, aliases); err != nil {
			return databaseError("queue affected correspondences", err)
		}
	}
	jobs := []AIOJob{}
	for _, input := range inputs {
		if input.Job != nil {
			jobs = append(jobs, *input.Job)
		}
	}
	if err = finishAIOJobs(ctx, tx, jobs); err != nil {
		return databaseError("acknowledge metadata batch", err)
	}
	return databaseError("publish metadata batch", tx.Commit(ctx))
}

func finishAIOJobs(ctx context.Context, tx pgx.Tx, jobs []AIOJob) error {
	if len(jobs) == 0 {
		return nil
	}
	types, ids, versions := []string{}, []string{}, []int64{}
	for _, job := range jobs {
		types, ids, versions = append(types, job.Type), append(ids, job.ID), append(versions, job.Version)
	}
	_, err := tx.Exec(ctx, `DELETE FROM ingest.aio_metadata_jobs j USING unnest($1::text[],$2::text[],$3::bigint[]) AS done(media_type,tmdb_id,version)
WHERE j.media_type=done.media_type AND j.tmdb_id=done.tmdb_id AND j.version=done.version`, types, ids, versions)
	return err
}

func queueAIOManyAliases(ctx context.Context, tx pgx.Tx, workIDs []int64, aliases []string) error {
	sort.Strings(aliases)
	aliases = deduplicateStrings(aliases)
	if _, err := tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.5',true)`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO ingest.aio_match_queue(provider_id,source_id)
SELECT provider_id,source_id FROM (
 SELECT provider_id,source_id FROM ingest.aio_matches WHERE work_id=ANY($1::bigint[]) AND status='matched'
 UNION SELECT r.provider_id,r.source_id FROM ingest.aio_match_reviews r JOIN ingest.aio_works w ON w.media_type=r.media_type AND w.tmdb_id=r.tmdb_id WHERE r.active AND w.id=ANY($1::bigint[])
 UNION SELECT provider_id,source_id FROM ingest.aio_matches WHERE normalized=ANY($2::text[])
 UNION SELECT m.provider_id,m.source_id FROM unnest($2::text[]) a JOIN ingest.aio_matches m
 ON m.normalized OPERATOR(public.%) a AND public.similarity(m.normalized,a)>=0.5
) affected ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, workIDs, aliases)
	return err
}

// Keep v1's bounded freshness audit alongside changes and daily inventory.
func (s *Store) QueueAIORefresh(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ingest.aio_metadata_jobs(media_type,tmdb_id)
SELECT media_type,tmdb_id FROM ingest.aio_works w WHERE updated_at<now()-interval '30 days'
AND NOT EXISTS(SELECT 1 FROM ingest.aio_metadata_jobs j WHERE j.media_type=w.media_type AND j.tmdb_id=w.tmdb_id)
ORDER BY updated_at,media_type,tmdb_id LIMIT 100 ON CONFLICT DO NOTHING`)
	return databaseError("queue metadata freshness audit", err)
}
