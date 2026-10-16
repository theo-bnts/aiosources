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
const catalogTitleColumns = `w.id,w.media_type,COALESCE(NULLIF(w.display_title,''),
(SELECT title FROM ingest.aio_titles WHERE work_id=w.id ORDER BY title LIMIT 1),'Untitled'),w.years,w.tmdb_id,w.imdb_id,
(SELECT count(*)` + catalogReleaseSource + `),ARRAY(SELECT DISTINCT t.provider_id` + catalogReleaseSource + ` ORDER BY t.provider_id)`

func scanCatalogTitle(row scanner) (model.CatalogTitle, error) {
	var title model.CatalogTitle
	err := row.Scan(&title.ID, &title.Type, &title.Title, &title.Years, &title.TMDB, &title.IMDb, &title.Releases, &title.Providers)
	return title, err
}

func (s *Store) CatalogTitles(ctx context.Context, opts model.ListOptions, normalized, kind string) (model.List[model.CatalogTitle], error) {
	opts = pageOptions(opts)
	result := model.List[model.CatalogTitle]{Items: []model.CatalogTitle{}, Limit: opts.Limit, Offset: opts.Offset}
	if !validSearchText(opts.Query, 500) || len([]rune(normalized)) < 2 || (kind != "" && kind != "movie" && kind != "tv") {
		return result, model.ErrInvalid
	}
	args := []any{strings.TrimSpace(opts.Query), kind}
	terms := []string{}
	for _, word := range strings.Fields(normalized) {
		args = append(args, "%"+escapeSearchFragment.Replace(word)+"%")
		terms = append(terms, fmt.Sprintf(`normalized LIKE $%d ESCAPE '\'`, len(args)))
	}
	filter := ` FROM (SELECT id FROM ingest.aio_works WHERE imdb_id=$1 OR tmdb_id=$1
UNION SELECT work_id FROM ingest.aio_titles WHERE ` + strings.Join(terms, " AND ") + `) candidates
JOIN ingest.aio_works w ON w.id=candidates.id WHERE NOT w.adult AND ($2='' OR w.media_type=$2)
AND EXISTS(SELECT 1` + catalogReleaseSource + `)`
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin catalog search", err)
	}
	defer rollback(tx)
	if err = tx.QueryRow(ctx, `SELECT count(*)`+filter, args...).Scan(&result.Total); err != nil {
		return result, databaseError("count catalog titles", err)
	}
	args = append(args, opts.Limit, opts.Offset)
	rows, err := tx.Query(ctx, `SELECT `+catalogTitleColumns+filter+fmt.Sprintf(` ORDER BY lower(COALESCE(NULLIF(w.display_title,''),(SELECT title FROM ingest.aio_titles WHERE work_id=w.id ORDER BY title LIMIT 1))),w.id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
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

func (s *Store) CatalogTitle(ctx context.Context, id int64) (model.CatalogTitle, error) {
	title, err := scanCatalogTitle(s.pool.QueryRow(ctx, `SELECT `+catalogTitleColumns+` FROM ingest.aio_works w WHERE w.id=$1 AND NOT w.adult AND EXISTS(SELECT 1`+catalogReleaseSource+`)`, id))
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
