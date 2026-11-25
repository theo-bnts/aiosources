package store

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const trendingCategoryCase = `CASE
 WHEN w.media_type IN ('movie','tv') THEN 'videos'
 WHEN w.media_type='game' THEN 'games'
 WHEN w.media_type='software' THEN 'software'
 WHEN w.media_type='music' THEN 'music'
 WHEN w.media_type='book' THEN 'books'
END`

type trendingFlight struct {
	done chan struct{}
	err  error
}

type trendingSnapshotRow struct {
	category  string
	rank      int32
	workID    int64
	revision  int64
	torrents  int64
	episodes  int64
	seasons   int64
	score     float64
	scoreUnit string
}

var trendingPeriodsOrder = []string{
	model.TrendingPeriodDay,
	model.TrendingPeriodWeek,
	model.TrendingPeriodMonth,
	model.TrendingPeriodQuarter,
	model.TrendingPeriodYear,
}

func trendingSnapshotTTL(period string) time.Duration {
	switch period {
	case model.TrendingPeriodDay:
		return 5 * time.Minute
	case model.TrendingPeriodWeek:
		return 15 * time.Minute
	case model.TrendingPeriodMonth:
		return 30 * time.Minute
	case model.TrendingPeriodQuarter:
		return time.Hour
	case model.TrendingPeriodYear:
		return 2 * time.Hour
	default:
		return 15 * time.Minute
	}
}

// RefreshCatalogTrending computes and stores the top-50 per-category trending
// snapshot for period. Concurrent callers for the same period coalesce onto one
// in-flight refresh, and the aggregation runs without parallel gather workers
// so large quarter/year windows never exhaust small container /dev/shm limits.
func (s *Store) RefreshCatalogTrending(ctx context.Context, period string) error {
	if _, ok := model.TrendingPeriodDays(period); !ok {
		return model.ErrInvalid
	}
	flight, err := s.startTrendingRefresh(period)
	if err != nil {
		return err
	}
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) refreshCatalogTrendingAsync(period string) {
	_, _ = s.startTrendingRefresh(period)
}

func (s *Store) startTrendingRefresh(period string) (*trendingFlight, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	s.trendingMu.Lock()
	if s.trendingFlights == nil {
		s.trendingFlights = map[string]*trendingFlight{}
	}
	if existing, ok := s.trendingFlights[period]; ok {
		s.trendingMu.Unlock()
		s.mu.Unlock()
		return existing, nil
	}
	flight := &trendingFlight{done: make(chan struct{})}
	s.trendingFlights[period] = flight
	s.listeners.Add(1)
	s.trendingMu.Unlock()
	s.mu.Unlock()

	go func() {
		defer s.listeners.Done()
		defer func() {
			s.trendingMu.Lock()
			delete(s.trendingFlights, period)
			s.trendingMu.Unlock()
			close(flight.done)
		}()
		parent := s.background
		if parent == nil {
			parent = context.Background()
		}
		refreshCtx, cancel := context.WithTimeout(parent, 3*time.Minute)
		defer cancel()
		flight.err = s.computeAndStoreTrending(refreshCtx, period)
	}()
	return flight, nil
}

// RefreshStaleCatalogTrending warms missing or expired period snapshots in the
// background whenever identified matches exist.
func (s *Store) RefreshStaleCatalogTrending(ctx context.Context) error {
	var hasMatches bool
	if err := s.workerPool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.aio_matches WHERE status='matched' AND work_id IS NOT NULL)`).Scan(&hasMatches); err != nil {
		return databaseError("check trending matches", err)
	}
	if !hasMatches {
		return nil
	}
	for _, period := range trendingPeriodsOrder {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		computedAt, hasItems, err := s.trendingSnapshotStatus(ctx, period)
		if err != nil {
			return err
		}
		if hasItems && time.Since(computedAt) <= trendingSnapshotTTL(period) {
			continue
		}
		if err := s.RefreshCatalogTrending(ctx, period); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) trendingSnapshotStatus(ctx context.Context, period string) (time.Time, bool, error) {
	var computedAt time.Time
	var hasItems bool
	err := s.pool.QueryRow(ctx, `SELECT st.computed_at, EXISTS(SELECT 1 FROM aiosources.aio_trending_snapshots sp WHERE sp.period=$1)
 FROM aiosources.aio_trending_state st
 WHERE st.period=$1`, period).Scan(&computedAt, &hasItems)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, databaseError("read trending snapshot status", err)
	}
	return computedAt, hasItems, nil
}

func (s *Store) computeAndStoreTrending(ctx context.Context, period string) error {
	days, ok := model.TrendingPeriodDays(period)
	if !ok {
		return model.ErrInvalid
	}
	started := time.Now()
	pool := s.workerPool()

	readTx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return databaseError("begin trending snapshot computation", err)
	}
	defer rollback(readTx)
	// Disable parallel gather workers for the large window aggregation so
	// PostgreSQL uses process-private work_mem/temp storage instead of dynamic
	// shared memory in container /dev/shm (commonly capped at 64 MiB in Docker).
	if _, err := readTx.Exec(ctx, `SET LOCAL max_parallel_workers_per_gather = 0`); err != nil {
		return databaseError("configure trending snapshot parallelism", err)
	}
	if _, err := readTx.Exec(ctx, `SET LOCAL statement_timeout = '180s'`); err != nil {
		return databaseError("configure trending snapshot timeout", err)
	}

	query := `WITH matched_rows AS (
 SELECT m.work_id,m.revision,
  COALESCE(t.search_published_at,t.first_seen_at) AS published_at,
  CASE
   WHEN rev.provider_id IS NOT NULL THEN
    CASE
     WHEN rev.season IS NOT NULL AND rev.episode IS NOT NULL THEN 'e:' || rev.season::text || ':' || rev.episode::text
     WHEN rev.season IS NOT NULL THEN 's:' || rev.season::text
     ELSE ''
    END
   ELSE COALESCE(ek.episode_key,'')
  END AS ep_key
 FROM aiosources.torrents t
 JOIN aiosources.aio_matches m
  ON m.provider_id=t.provider_id
  AND m.source_id=t.source_id
  AND m.status='matched'
  AND m.work_id IS NOT NULL
  AND COALESCE(m.fingerprint=t.fields_fingerprint,false)
 LEFT JOIN aiosources.aio_match_queue q
  ON q.provider_id=t.provider_id
  AND q.source_id=t.source_id
 LEFT JOIN aiosources.torrent_episode_keys ek
  ON ek.provider_id=t.provider_id
  AND ek.source_id=t.source_id
 LEFT JOIN aiosources.aio_match_reviews rev
  ON rev.provider_id=t.provider_id
  AND rev.source_id=t.source_id
  AND rev.active
  AND rev.override_episode
 WHERE COALESCE(t.search_published_at,t.first_seen_at)>=now()-make_interval(days=>$1)
  AND q.provider_id IS NULL
),
per_key AS (
 SELECT work_id,revision,ep_key,
  count(*)::bigint AS torrents,
  max(published_at) AS latest_at
 FROM matched_rows
 GROUP BY work_id,revision,ep_key
),
period_counts AS (
 SELECT work_id,revision,
  sum(torrents)::bigint AS torrents,
  COALESCE(sum(torrents) FILTER (WHERE ep_key LIKE 'e:%'),0)::bigint AS episode_torrents,
  count(*) FILTER (WHERE ep_key LIKE 'e:%')::bigint AS episode_count,
  count(*) FILTER (WHERE ep_key LIKE 's:%')::bigint AS season_count,
  max(latest_at) AS latest_at
 FROM per_key
 GROUP BY work_id,revision
),
scored AS (
 SELECT w.id,w.revision,` + trendingCategoryCase + ` AS category,
  c.torrents,
  CASE WHEN w.media_type='tv' THEN c.episode_count ELSE 0 END AS episodes,
  CASE WHEN w.media_type='tv' THEN c.season_count ELSE 0 END AS seasons,
  CASE
   WHEN w.media_type<>'tv' THEN c.torrents::double precision
   WHEN c.episode_count>0 AND c.episode_count>=c.season_count THEN
    c.torrents::double precision / c.episode_count::double precision
   WHEN c.season_count>0 AND c.episode_count>0 THEN
    GREATEST(
     c.episode_torrents::double precision / c.episode_count::double precision,
     c.torrents::double precision / c.season_count::double precision
    )
   WHEN c.season_count>0 THEN
    c.torrents::double precision / c.season_count::double precision
   ELSE c.torrents::double precision
  END AS score,
  CASE
   WHEN w.media_type<>'tv' THEN ''
   WHEN c.episode_count>0 AND c.episode_count>=c.season_count THEN 'episode'
   WHEN c.season_count>0 AND c.episode_count>0 THEN
    CASE
     WHEN c.episode_torrents::double precision / c.episode_count::double precision >= c.torrents::double precision / c.season_count::double precision THEN 'episode'
     ELSE 'season'
    END
   WHEN c.season_count>0 THEN 'season'
   ELSE ''
  END AS score_unit,
  c.latest_at
 FROM period_counts c
 JOIN aiosources.aio_works w ON w.id=c.work_id AND w.revision=c.revision
 WHERE NOT w.adult
  AND w.media_type IN ('movie','tv','game','software','music','book')
),
ranked AS (
 SELECT id,revision,category,torrents,episodes,seasons,score,score_unit,
  row_number() OVER (
   PARTITION BY category
   ORDER BY score DESC,torrents DESC,latest_at DESC,id ASC
  )::int4 AS rn
 FROM scored
)
SELECT category,rn,id,revision,torrents,episodes,seasons,score,score_unit
FROM ranked
WHERE rn<=$2
ORDER BY category,rn`

	rows, err := readTx.Query(ctx, query, days, model.MaxTrendingLimit)
	if err != nil {
		return databaseError("compute trending snapshot", err)
	}
	entries := make([]trendingSnapshotRow, 0, len(model.TrendingCategories)*model.DefaultTrendingLimit)
	for rows.Next() {
		var entry trendingSnapshotRow
		if err := rows.Scan(
			&entry.category,
			&entry.rank,
			&entry.workID,
			&entry.revision,
			&entry.torrents,
			&entry.episodes,
			&entry.seasons,
			&entry.score,
			&entry.scoreUnit,
		); err != nil {
			rows.Close()
			return databaseError("read trending snapshot row", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return databaseError("compute trending snapshot", err)
	}
	rows.Close()
	if err := readTx.Commit(ctx); err != nil {
		return databaseError("finish trending snapshot read", err)
	}

	writeTx, err := pool.Begin(ctx)
	if err != nil {
		return databaseError("begin trending snapshot save", err)
	}
	defer rollback(writeTx)
	if _, err := writeTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, migrationLockNamespace, trendingSnapshotLockID); err != nil {
		return databaseError("lock trending snapshot", err)
	}
	if _, err := writeTx.Exec(ctx, `DELETE FROM aiosources.aio_trending_snapshots WHERE period=$1`, period); err != nil {
		return databaseError("clear trending snapshot", err)
	}
	if len(entries) > 0 {
		categories := make([]string, len(entries))
		ranks := make([]int32, len(entries))
		workIDs := make([]int64, len(entries))
		revisions := make([]int64, len(entries))
		torrents := make([]int64, len(entries))
		episodes := make([]int64, len(entries))
		seasons := make([]int64, len(entries))
		scores := make([]float64, len(entries))
		scoreUnits := make([]string, len(entries))
		for i, e := range entries {
			categories[i] = e.category
			ranks[i] = e.rank
			workIDs[i] = e.workID
			revisions[i] = e.revision
			torrents[i] = e.torrents
			episodes[i] = e.episodes
			seasons[i] = e.seasons
			scores[i] = e.score
			scoreUnits[i] = e.scoreUnit
		}
		if _, err := writeTx.Exec(ctx, `INSERT INTO aiosources.aio_trending_snapshots(
 period,category,rank,work_id,revision,torrents,episodes,seasons,score,score_unit
)
SELECT $1,u.category,u.rank,u.work_id,u.revision,u.torrents,u.episodes,u.seasons,u.score,u.score_unit
FROM unnest($2::text[],$3::int4[],$4::int8[],$5::int8[],$6::int8[],$7::int8[],$8::int8[],$9::float8[],$10::text[])
 AS u(category,rank,work_id,revision,torrents,episodes,seasons,score,score_unit)
JOIN aiosources.aio_works w ON w.id=u.work_id AND w.revision=u.revision AND NOT w.adult`,
			period, categories, ranks, workIDs, revisions, torrents, episodes, seasons, scores, scoreUnits,
		); err != nil {
			return databaseError("save trending snapshot", err)
		}
	}
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > math.MaxInt32 {
		elapsed = math.MaxInt32
	}
	if _, err := writeTx.Exec(ctx, `INSERT INTO aiosources.aio_trending_state(period,computed_at,duration_ms)
 VALUES($1,now(),$2)
 ON CONFLICT(period) DO UPDATE SET computed_at=EXCLUDED.computed_at,duration_ms=EXCLUDED.duration_ms`,
		period, int32(elapsed),
	); err != nil {
		return databaseError("save trending snapshot state", err)
	}
	return databaseError("commit trending snapshot", writeTx.Commit(ctx))
}

// CatalogTrending ranks identified works in each catalog category by published
// matched torrent activity within the selected rolling window. TV series are
// ranked by average torrents per active episode (or per active season when
// dominated by multi-season packs) so long-running/daily series do not crowd
// out higher-demand titles. Rankings are served from per-period precomputed
// snapshots with stale-while-revalidate refresh, while localized title, cover
// and author projections are resolved live from aio_works for the final winners.
func (s *Store) CatalogTrending(ctx context.Context, opts model.CatalogTrendingOptions) (model.CatalogTrending, error) {
	period := opts.Period
	if period == "" {
		period = model.DefaultTrendingPeriod
	}
	if _, ok := model.TrendingPeriodDays(period); !ok {
		return model.CatalogTrending{}, model.ErrInvalid
	}
	limit := opts.Limit
	if limit == 0 {
		limit = model.DefaultTrendingLimit
	}
	if limit < 1 || limit > model.MaxTrendingLimit {
		return model.CatalogTrending{}, model.ErrInvalid
	}
	locale := "en"
	if opts.Language != "" {
		locale = opts.Language
	}

	result := model.CatalogTrending{
		Period:     period,
		Categories: make([]model.CatalogTrendingCategory, len(model.TrendingCategories)),
	}
	byCategory := make(map[string]int, len(model.TrendingCategories))
	for i, id := range model.TrendingCategories {
		result.Categories[i] = model.CatalogTrendingCategory{
			ID:    id,
			Items: []model.CatalogTrendingItem{},
		}
		byCategory[id] = i
	}

	computedAt, hasItems, err := s.trendingSnapshotStatus(ctx, period)
	if err != nil {
		return result, err
	}
	if !hasItems {
		if err := s.RefreshCatalogTrending(ctx, period); err != nil {
			return result, err
		}
	} else if time.Since(computedAt) > trendingSnapshotTTL(period) {
		s.refreshCatalogTrendingAsync(period)
	}

	query := `SELECT s.category,s.torrents,s.episodes,s.seasons,s.score,s.score_unit,w.id,w.media_type,` + catalogTitleLabel("$3", "NULL") + `,` + bookWorkYears + `,
 COALESCE(NULLIF(w.localized_posters->>split_part($3,'-',1),''),NULLIF(w.localized_posters->>'en',''),w.poster_path,''),
 COALESCE(` + bookEditionLocalizedCover("$3") + `,CASE WHEN (w.book_data->>'cover') ~ '^[0-9]{1,18}$' THEN NULLIF((w.book_data->>'cover')::bigint,0) END,0),
 CASE WHEN w.media_type='book' THEN ARRAY(SELECT a.name FROM aiosources.ol_authors a WHERE a.id=ANY(ARRAY(SELECT aiosources.ol_resolve(jsonb_array_elements_text(CASE WHEN jsonb_typeof(w.book_data->'author_keys')='array' THEN w.book_data->'author_keys' ELSE '[]'::jsonb END)))) ORDER BY a.id) ELSE '{}'::text[] END,
 w.music_data,w.software_data
FROM aiosources.aio_trending_snapshots s
JOIN aiosources.aio_works w ON w.id=s.work_id AND w.revision=s.revision
WHERE s.period=$1 AND s.rank<=$2 AND NOT w.adult
ORDER BY s.category,s.rank`

	rows, err := s.pool.Query(ctx, query, period, limit, locale)
	if err != nil {
		return result, databaseError("list trending catalog titles", err)
	}
	defer rows.Close()

	for rows.Next() {
		var category, scoreUnit, poster string
		var cover int64
		var score float64
		var musicData, softwareData []byte
		var item model.CatalogTrendingItem
		if err := rows.Scan(
			&category,
			&item.Torrents,
			&item.Episodes,
			&item.Seasons,
			&score,
			&scoreUnit,
			&item.ID,
			&item.Type,
			&item.Title,
			&item.Years,
			&poster,
			&cover,
			&item.Authors,
			&musicData,
			&softwareData,
		); err != nil {
			return result, databaseError("read trending catalog title", err)
		}
		if item.Type == "tv" {
			rounded := math.Round(score*100) / 100
			switch scoreUnit {
			case "episode":
				if item.Episodes > 0 {
					item.TorrentsPerEpisode = rounded
				}
			case "season":
				if item.Seasons > 1 {
					item.TorrentsPerSeason = rounded
				}
			}
		}
		if item.Years == nil {
			item.Years = []int{}
		}
		if len(item.Authors) == 0 {
			item.Authors = nil
		}
		if item.Type == "music" && len(musicData) > 0 && string(musicData) != "null" {
			var info model.MusicInformation
			if err := json.Unmarshal(musicData, &info); err != nil {
				return result, databaseError("decode trending music metadata", err)
			}
			item.Artists = info.Artists
			if len(item.Artists) == 0 && info.ArtistCredit != "" {
				item.Artists = []string{info.ArtistCredit}
			}
			item.MusicType = info.PrimaryType
		}
		if item.Type == "software" && len(softwareData) > 0 && string(softwareData) != "null" {
			var info model.SoftwareInformation
			if err := json.Unmarshal(softwareData, &info); err != nil {
				return result, databaseError("decode trending software metadata", err)
			}
			item.Vendor = info.Vendor
		}
		item.Cover = model.CoverURL(item.Type, poster, cover)
		if idx, exists := byCategory[category]; exists {
			result.Categories[idx].Items = append(result.Categories[idx].Items, item)
		}
	}
	return result, databaseError("list trending catalog titles", rows.Err())
}
