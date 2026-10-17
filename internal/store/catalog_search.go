package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const aioPublishedMatch = `m.status='matched' AND m.fingerprint=t.fields_fingerprint AND m.revision=w.revision
AND NOT EXISTS(SELECT 1 FROM ingest.aio_match_queue q WHERE q.provider_id=t.provider_id AND q.source_id=t.source_id) AND NOT w.adult`
const catalogReleaseSource = ` FROM ingest.aio_matches m JOIN ingest.torrents t USING(provider_id,source_id) WHERE m.work_id=w.id AND ` + aioPublishedMatch

// Localized labels do not change correspondence. A matching local alias is the
// query-language fallback, followed by the English primary title.
func catalogTitleLabel(locale, alias string) string {
	return `COALESCE(NULLIF(w.localized_titles->>` + locale + `,''),` + alias + `,NULLIF(w.localized_titles->>'en',''),NULLIF(w.display_title,''),
 (SELECT title FROM ingest.aio_titles WHERE work_id=w.id ORDER BY title LIMIT 1),'Untitled')`
}

func catalogTitleColumns(locale, alias string) string {
	return `w.id,w.media_type,` + catalogTitleLabel(locale, alias) + `,w.years,w.tmdb_id,w.imdb_id,
 (SELECT count(*)` + catalogReleaseSource + `),ARRAY(SELECT DISTINCT t.provider_id` + catalogReleaseSource + ` ORDER BY t.provider_id)`
}

func catalogYearFilter(year string) string {
	// Film years are alternatives. A series spans all locally known work,
	// season and episode years, including gaps between seasons.
	return `(` + year + `=ANY(w.years) OR (w.media_type='tv' AND EXISTS(
 SELECT 1 FROM (SELECT unnest(w.years) AS year UNION ALL SELECT year FROM ingest.aio_seasons WHERE work_id=w.id UNION ALL SELECT year FROM ingest.aio_episodes WHERE work_id=w.id) known_years
 HAVING ` + year + ` BETWEEN min(year) AND max(year))))`
}

func scanCatalogTitle(row scanner) (model.CatalogTitle, error) {
	var title model.CatalogTitle
	err := row.Scan(&title.ID, &title.Type, &title.Title, &title.Years, &title.TMDB, &title.IMDb, &title.Releases, &title.Providers)
	return title, err
}

func (s *Store) CatalogTitles(ctx context.Context, opts model.ListOptions, normalized string, filters model.CatalogSearchFilters) (model.List[model.CatalogTitle], error) {
	opts = pageOptions(opts)
	result := model.List[model.CatalogTitle]{Items: []model.CatalogTitle{}, Limit: opts.Limit, Offset: opts.Offset}
	if !validSearchText(opts.Query, 500) || len([]rune(normalized)) < 2 || (filters.Type != "" && filters.Type != "movie" && filters.Type != "tv") || (filters.Year != 0 && (filters.Year < 1000 || filters.Year > 9999)) {
		return result, model.ErrInvalid
	}
	locale := "en"
	if filters.Language != "" {
		locale = filters.Language
	}
	args := []any{strings.TrimSpace(opts.Query), filters.Type, normalized}
	terms := []string{}
	for _, word := range strings.Fields(normalized) {
		args = append(args, "%"+escapeSearchFragment.Replace(word)+"%")
		terms = append(terms, fmt.Sprintf(`normalized LIKE $%d ESCAPE '\'`, len(args)))
	}
	matching := strings.Join(terms, " AND ")
	filter := ` FROM (SELECT id,max(precision) AS precision,max(relevance) AS relevance FROM (
 SELECT id,5 AS precision,1::real AS relevance FROM ingest.aio_works WHERE imdb_id=$1 OR tmdb_id=$1
 UNION ALL SELECT work_id,CASE WHEN normalized=$3 THEN 4 ELSE 1 END,
 public.similarity($3,normalized) FROM ingest.aio_titles WHERE ` + matching + `) matches GROUP BY id) candidates
 JOIN ingest.aio_works w ON w.id=candidates.id WHERE NOT w.adult AND ($2='' OR w.media_type=$2)
 AND EXISTS(SELECT 1` + catalogReleaseSource + `)`
	if filters.Year != 0 {
		args = append(args, filters.Year)
		year := fmt.Sprintf("$%d::int", len(args))
		filter += ` AND ` + catalogYearFilter(year)
	}
	alias := `(SELECT title FROM ingest.aio_titles WHERE work_id=w.id AND ` + matching + ` ORDER BY public.similarity($3,normalized) DESC,length(normalized),title LIMIT 1)`
	columns := catalogTitleColumns(fmt.Sprintf("$%d", len(args)+1), alias)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin catalog search", err)
	}
	defer rollback(tx)
	if err = tx.QueryRow(ctx, `SELECT count(*)`+filter, args...).Scan(&result.Total); err != nil {
		return result, databaseError("count catalog titles", err)
	}
	args = append(args, locale, opts.Limit, opts.Offset)
	rows, err := tx.Query(ctx, `SELECT `+columns+filter+fmt.Sprintf(` ORDER BY candidates.precision DESC,candidates.relevance DESC,COALESCE((SELECT min(y) FROM unnest(w.years) y),0) DESC,w.id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return result, databaseError("search catalog titles", err)
	}
	for rows.Next() {
		title, e := scanCatalogTitle(rows)
		if e != nil {
			rows.Close()
			return result, databaseError("read catalog title", e)
		}
		result.Items = append(result.Items, title)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, databaseError("search catalog titles", err)
	}
	return result, databaseError("finish catalog search", tx.Commit(ctx))
}

func (s *Store) CatalogTitle(ctx context.Context, id int64, locales ...string) (model.CatalogTitle, error) {
	locale := "en"
	if len(locales) > 0 && locales[0] != "" {
		locale = locales[0]
	}
	args := []any{id, locale}
	alias := "NULL"
	if len(locales) > 1 && locales[1] != "" && len(locales[1]) <= 500 {
		normalized := locales[1]
		args = append(args, normalized)
		terms := []string{}
		for _, word := range strings.Fields(normalized) {
			args = append(args, "%"+escapeSearchFragment.Replace(word)+"%")
			terms = append(terms, fmt.Sprintf(`normalized LIKE $%d ESCAPE '\'`, len(args)))
		}
		if len(terms) > 0 {
			alias = `(SELECT title FROM ingest.aio_titles WHERE work_id=w.id AND ` + strings.Join(terms, " AND ") + ` ORDER BY public.similarity($3,normalized) DESC,length(normalized),title LIMIT 1)`
		}
	}
	title, err := scanCatalogTitle(s.pool.QueryRow(ctx, `SELECT `+catalogTitleColumns("$2", alias)+` FROM ingest.aio_works w WHERE w.id=$1 AND NOT w.adult AND EXISTS(SELECT 1`+catalogReleaseSource+`)`, args...))
	return title, databaseError("read catalog title", err)
}

type CatalogRecord struct {
	model.AIORecord
	OccurrenceID string
}

func (s *Store) CatalogRecords(ctx context.Context, id int64) ([]CatalogRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+aioRecordColumns+`,t.occurrence_id,w.imdb_id,w.tmdb_id,w.tvdb_id FROM ingest.aio_works w JOIN ingest.aio_matches m ON m.work_id=w.id JOIN ingest.torrents t USING(provider_id,source_id) WHERE w.id=$1 AND `+aioPublishedMatch+` ORDER BY COALESCE(t.search_published_at,t.first_seen_at) DESC,t.provider_id,t.source_id`, id)
	if err != nil {
		return nil, databaseError("find catalog releases", err)
	}
	out := []CatalogRecord{}
	records := []model.AIORecord{}
	for rows.Next() {
		var occurrence, imdb, tmdb, tvdb string
		record, e := scanAIORecord(aioStreamRow{scanner: rows, ids: []any{&occurrence, &imdb, &tmdb, &tvdb}})
		if e != nil {
			rows.Close()
			return nil, databaseError("read catalog release", e)
		}
		record.IMDb, record.TMDB, record.TVDB = imdb, tmdb, tvdb
		out = append(out, CatalogRecord{AIORecord: record, OccurrenceID: occurrence})
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, databaseError("find catalog releases", err)
	}
	if err = s.applyAIOEpisodeReviews(ctx, records); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].AIORecord = records[i]
	}
	return out, nil
}
