package store

import (
	"context"
	"encoding/json"
	"math"

	"github.com/theo-bnts/aiosources/internal/model"
)

const trendingCategoryCase = `CASE
 WHEN w.media_type IN ('movie','tv') THEN 'videos'
 WHEN w.media_type='game' THEN 'games'
 WHEN w.media_type='software' THEN 'software'
 WHEN w.media_type='music' THEN 'music'
 WHEN w.media_type='book' THEN 'books'
END`

// CatalogTrending ranks identified works in each catalog category by published
// matched torrent activity within the selected rolling window. TV series are
// ranked by average torrents per active episode (or per active season when
// dominated by multi-season packs) so long-running/daily series do not crowd
// out higher-demand titles. Grouping runs in two hash-aggregate passes over
// torrents_trending_recent before touching aio_works, and localized title,
// cover and author projections run only on the final per-category top-N winners.
func (s *Store) CatalogTrending(ctx context.Context, opts model.CatalogTrendingOptions) (model.CatalogTrending, error) {
	period := opts.Period
	if period == "" {
		period = model.DefaultTrendingPeriod
	}
	days, ok := model.TrendingPeriodDays(period)
	if !ok {
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
 SELECT w.id,` + trendingCategoryCase + ` AS category,
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
 SELECT id,category,torrents,episodes,seasons,score,score_unit,
  row_number() OVER (
   PARTITION BY category
   ORDER BY score DESC,torrents DESC,latest_at DESC,id ASC
  ) AS rn
 FROM scored
)
SELECT r.category,r.torrents,r.episodes,r.seasons,r.score,r.score_unit,w.id,w.media_type,` + catalogTitleLabel("$2", "NULL") + `,` + bookWorkYears + `,
 COALESCE(NULLIF(w.localized_posters->>split_part($2,'-',1),''),NULLIF(w.localized_posters->>'en',''),w.poster_path),
 COALESCE(` + bookEditionLocalizedCover("$2") + `,NULLIF((w.book_data->>'cover')::bigint,0),0),
 CASE WHEN w.media_type='book' THEN ARRAY(SELECT a.name FROM aiosources.ol_authors a WHERE a.id=ANY(ARRAY(SELECT aiosources.ol_resolve(jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]'))))) ORDER BY a.id) ELSE '{}'::text[] END,
 w.music_data,w.software_data
FROM ranked r
JOIN aiosources.aio_works w ON w.id=r.id
WHERE r.rn<=$3
ORDER BY r.category,r.rn`

	rows, err := s.pool.Query(ctx, query, days, locale, limit)
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
