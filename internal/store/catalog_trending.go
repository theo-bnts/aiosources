package store

import (
	"context"
	"encoding/json"

	"github.com/theo-bnts/aiosources/internal/model"
)

const trendingCategoryCase = `CASE
 WHEN w.media_type IN ('movie','tv') THEN 'videos'
 WHEN w.media_type='game' THEN 'games'
 WHEN w.media_type='software' THEN 'software'
 WHEN w.media_type='music' THEN 'music'
 WHEN w.media_type='book' THEN 'books'
END`

// CatalogTrending ranks identified works in each catalog category by the number
// of published matched torrents within the selected rolling window.
// Grouping occurs before touching aio_works, and localized title/cover/author
// projections run only on the final per-category top-N winners.
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

	query := `WITH period_counts AS (
 SELECT m.work_id,m.revision,count(*)::bigint AS torrents,
  max(COALESCE(t.search_published_at,t.first_seen_at)) AS latest_at
 FROM aiosources.torrents t
 JOIN aiosources.aio_matches m
  ON m.provider_id=t.provider_id
  AND m.source_id=t.source_id
  AND m.status='matched'
  AND m.work_id IS NOT NULL
  AND m.fingerprint=t.fields_fingerprint
 LEFT JOIN aiosources.aio_match_queue q
  ON q.provider_id=t.provider_id
  AND q.source_id=t.source_id
 WHERE COALESCE(t.search_published_at,t.first_seen_at)>=now()-make_interval(days=>$1)
  AND q.provider_id IS NULL
 GROUP BY m.work_id,m.revision
),
ranked AS (
 SELECT w.id,` + trendingCategoryCase + ` AS category,c.torrents,c.latest_at,
  row_number() OVER (
   PARTITION BY ` + trendingCategoryCase + `
   ORDER BY c.torrents DESC,c.latest_at DESC,w.id ASC
  ) AS rn
 FROM period_counts c
 JOIN aiosources.aio_works w ON w.id=c.work_id AND w.revision=c.revision
 WHERE NOT w.adult
)
SELECT r.category,r.torrents,w.id,w.media_type,` + catalogTitleLabel("$2", "NULL") + `,` + bookWorkYears + `,
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
		var category, poster string
		var cover int64
		var musicData, softwareData []byte
		var item model.CatalogTrendingItem
		if err := rows.Scan(
			&category,
			&item.Torrents,
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
