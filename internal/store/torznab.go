package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Torznab reads the current publication, not synchronization history. A single
// repeatable snapshot keeps totals and rows consistent during a Full publish.
// Episode filtering reuses the playback parser supplied by the API. It is only
// accepted with a title or identifier and streams candidates in constant space.
func (d *CatalogDownload) Torznab(ctx context.Context, q model.TorznabQuery, episodeMatch func(model.AIORecord) bool) (model.List[model.TorznabRecord], error) {
	ctx, cancel := context.WithDeadline(ctx, d.deadline)
	defer cancel()
	result := model.List[model.TorznabRecord]{Items: []model.TorznabRecord{}, Limit: q.Limit, Offset: q.Offset}
	if d.format != "torznab" {
		return result, ErrCatalogAuthentication
	}
	policy := TorznabVisibility{}
	tx, err := d.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin Torznab query", err)
	}
	defer rollback(tx)
	var scope string
	var sources, media []string
	var encodedPreferences []byte
	err = tx.QueryRow(ctx, `SELECT scope,source_ids,media_types,torznab_preferences FROM ingest.shares s WHERE id=$1 AND format='torznab' AND enabled AND revision=$2 AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM ingest.share_requests r WHERE r.share_id=s.id AND r.id=$3 AND lease_until>clock_timestamp())`, d.shareID, d.revision, d.id).Scan(&scope, &sources, &media, &encodedPreferences)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrCatalogAuthentication
	}
	if err != nil {
		return result, databaseError("authorize Torznab query", err)
	}
	var preferences model.TorznabPreferences
	if json.Unmarshal(encodedPreferences, &preferences) != nil || !preferences.Valid() {
		return result, model.ErrInvalid
	}
	q = q.WithPreferences(preferences)
	if q.CachedOnly {
		config, e := readAIOConfig(ctx, tx)
		if e != nil {
			return result, e
		}
		policy.CacheAfter = time.Now().Add(-time.Duration(config.CacheMinutes) * time.Minute)
	}
	result, err = torznabRows(ctx, tx, q, scope, sources, media, policy, episodeMatch)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, databaseError("finish Torznab query", err)
	}
	// Write access time after releasing the repeatable-read snapshot. Updating
	// its grant inside that snapshot makes concurrent searches fail with 40001.
	// Recheck the revision and live admission so a changed policy fails closed.
	tag, err := d.store.pool.Exec(ctx, `UPDATE ingest.shares s SET last_access_at=clock_timestamp()
WHERE id=$1 AND format='torznab' AND enabled AND revision=$2 AND (expires_at IS NULL OR expires_at>clock_timestamp())
AND EXISTS(SELECT 1 FROM ingest.share_requests r WHERE r.share_id=s.id AND r.id=$3 AND lease_until>clock_timestamp())`, d.shareID, d.revision, d.id)
	if err != nil {
		return result, databaseError("record Torznab access", err)
	}
	if tag.RowsAffected() != 1 {
		return result, ErrCatalogAuthentication
	}
	return result, nil
}

// TorznabVisibility is a request-scoped policy. Unlisted sources are private;
// only an administrator's explicit public classification or a visitor's own
// currently permitted announce can authorize uncached results.
type TorznabVisibility struct {
	Public          bool
	UncachedSources []string
	CacheAfter      time.Time
}

func (s *Store) PublicTorznabCatalog(ctx context.Context, q model.TorznabQuery, sources, media []string, policy TorznabVisibility, episodeMatch func(model.AIORecord) bool) (model.List[model.TorznabRecord], error) {
	result := model.List[model.TorznabRecord]{Items: []model.TorznabRecord{}, Limit: q.Limit, Offset: q.Offset}
	if !policy.Public {
		return result, model.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin public Torznab query", err)
	}
	defer rollback(tx)
	scope := "all"
	if len(sources) > 0 {
		scope = "selected"
	}
	result, err = torznabRows(ctx, tx, q, scope, sources, media, policy, episodeMatch)
	if err != nil {
		return result, err
	}
	return result, databaseError("finish public Torznab query", tx.Commit(ctx))
}

func torznabRows(ctx context.Context, tx pgx.Tx, q model.TorznabQuery, scope string, sources, media []string, policy TorznabVisibility, episodeMatch func(model.AIORecord) bool) (model.List[model.TorznabRecord], error) {
	result := model.List[model.TorznabRecord]{Items: []model.TorznabRecord{}, Limit: q.Limit, Offset: q.Offset}
	filters, err := NormalizeSearchFilters(q.SearchFilters)
	if err != nil || q.Limit < 0 || q.Limit > 200 || q.Offset < 0 || q.Offset > 2147483647 || ((q.Season != nil || q.Episode != nil) && (episodeMatch == nil || (filters.Query == "" && q.IMDb == "" && q.TMDB == "" && q.TVDB == ""))) {
		return result, model.ErrInvalid
	}
	if (policy.Public || q.CachedOnly) && policy.CacheAfter.IsZero() {
		return result, model.ErrInvalid
	}
	args := []any{}
	bind := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	clauses := []string{`length(t.search_info_hash)=40`, `jsonb_typeof(t.fields->'title')='string' AND t.fields->>'title'<>''`, `(t.search_size IS NULL OR t.search_size BETWEEN 0 AND 9223372036854775807)`}
	if scope == "selected" {
		clauses = append(clauses, "t.provider_id=ANY("+bind(sources)+"::text[])")
	}
	if len(filters.Providers) > 0 {
		clauses = append(clauses, "t.provider_id=ANY("+bind(filters.Providers)+"::text[])")
	}
	if policy.Public || q.CachedOnly {
		services := q.Services
		if services == nil {
			services = model.DebridServices()
		}
		for _, service := range services {
			if !model.IsDebridService(service) {
				return result, model.ErrInvalid
			}
		}
		cached := "EXISTS(SELECT 1 FROM ingest.aio_shared_availability availability WHERE availability.hash=t.search_info_hash AND availability.service=ANY(" + bind(services) + "::text[]) AND availability.cached AND availability.checked_at>" + bind(policy.CacheAfter) + " AND availability.checked_at<=now())"

		if policy.Public {
			clauses = append(clauses, "(t.provider_id=ANY("+bind(policy.UncachedSources)+"::text[]) OR "+cached+")")
		}
		if q.CachedOnly {
			clauses = append(clauses, cached)
		}
	}
	category := `CASE w.media_type WHEN 'movie' THEN 2000 WHEN 'tv' THEN 5000 ELSE 8000 END`
	if len(media) > 0 {
		cats := []int{}
		for _, pair := range []struct {
			typ string
			cat int
		}{{"movie", 2000}, {"tv", 5000}, {"other", 8000}} {
			if slices.Contains(media, pair.typ) {
				cats = append(cats, pair.cat)
			}
		}
		clauses = append(clauses, category+"=ANY("+bind(cats)+"::int[])")
	}
	if q.Mode == "movie" {
		clauses = append(clauses, "w.media_type='movie'")
	}
	if q.Mode == "tvsearch" {
		clauses = append(clauses, "w.media_type='tv'")
	}
	if q.Categories != nil {
		clauses = append(clauses, category+"=ANY("+bind(q.Categories)+"::int[])")
	}
	if len(q.MediaTypes) > 0 {
		types := []int{}
		for _, typ := range q.MediaTypes {
			switch typ {
			case "movie":
				types = append(types, 2000)
			case "tv":
				types = append(types, 5000)
			case "other":
				types = append(types, 8000)
			default:
				return result, model.ErrInvalid
			}
		}
		clauses = append(clauses, category+"=ANY("+bind(types)+"::int[])")
	}
	for _, f := range []struct{ column, value string }{{"imdb_id", q.IMDb}, {"tmdb_id", q.TMDB}, {"tvdb_id", q.TVDB}} {
		if f.value != "" {
			clauses = append(clauses, "w."+f.column+"="+bind(f.value))
		}
	}
	if q.Year != 0 {
		clauses = append(clauses, bind(q.Year)+"::int=ANY(w.years)")
	}
	if filters.Query != "" && q.IMDb == "" && q.TMDB == "" && q.TVDB == "" {
		clauses = append(clauses, torrentTextSearchSQLFor("t", filters.Query, bind))
	}
	for _, f := range []struct{ column, op, value string }{{"t.search_size", ">=", filters.MinSize}, {"t.search_size", "<=", filters.MaxSize}, {"t.search_published_at", ">=", filters.PublishedAfter}, {"t.search_published_at", "<=", filters.PublishedBefore}} {
		if f.value != "" {
			typ := "::text::numeric"
			if strings.Contains(f.column, "published") {
				typ = "::text::timestamptz"
			}
			clauses = append(clauses, f.column+f.op+bind(f.value)+typ)
		}
	}
	from := ` FROM ingest.torrents t LEFT JOIN ingest.aio_matches m ON m.provider_id=t.provider_id AND m.source_id=t.source_id AND m.status='matched' AND m.fingerprint=t.fields_fingerprint
LEFT JOIN ingest.aio_works w ON w.id=m.work_id AND w.revision=m.revision AND NOT w.adult AND NOT EXISTS(SELECT 1 FROM ingest.aio_match_queue q WHERE q.provider_id=t.provider_id AND q.source_id=t.source_id)
LEFT JOIN ingest.aio_match_reviews review ON review.provider_id=t.provider_id AND review.source_id=t.source_id AND review.active AND review.override_episode AND review.media_type=w.media_type AND review.tmdb_id=w.tmdb_id AND review.evidence_fingerprint=md5(ingest.aio_review_evidence(t.fields)::text)
WHERE ` + strings.Join(clauses, " AND ")
	filtered := q.Season != nil || q.Episode != nil
	if !filtered {
		if err = tx.QueryRow(ctx, "SELECT count(*)"+from, args...).Scan(&result.Total); err != nil {
			return result, databaseError("count Torznab results", err)
		}
	}
	// Project only fields understood by the shared record decoder. No arbitrary
	// JSON, source links, announce URLs, raw identities or secrets cross this boundary.
	columns := `t.provider_id,''::text,jsonb_build_object('title',left(t.fields->>'title',4096),'info_hash',t.search_info_hash,'size',t.search_size,'published_at',COALESCE(t.search_published_at,t.first_seen_at),
'season',CASE WHEN review.provider_id IS NOT NULL THEN to_jsonb(review.season) ELSE COALESCE(t.fields->'season',t.fields#>'{attributes,season}') END,'episode',CASE WHEN review.provider_id IS NOT NULL THEN to_jsonb(review.episode) ELSE COALESCE(t.fields->'episode',t.fields#>'{attributes,episode}') END,
'languages',CASE WHEN pg_column_size(t.fields->'languages')<=4096 THEN t.fields->'languages' END,'language',CASE WHEN pg_column_size(t.fields->'language')<=256 THEN t.fields->'language' END,
'audiolangs',CASE WHEN pg_column_size(t.fields->'audiolangs')<=4096 THEN t.fields->'audiolangs' END,
'attributes',jsonb_build_object('language',CASE WHEN pg_column_size(t.fields#>'{attributes,language}')<=4096 THEN t.fields#>'{attributes,language}' END,'languages',CASE WHEN pg_column_size(t.fields#>'{attributes,languages}')<=4096 THEN t.fields#>'{attributes,languages}' END,'audiolangs',CASE WHEN pg_column_size(t.fields#>'{attributes,audiolangs}')<=4096 THEN t.fields#>'{attributes,audiolangs}' END)),t.fields_fingerprint,(SELECT instance_id FROM ingest.catalog_identity WHERE singleton)||':'||t.occurrence_id,` + category + `,COALESCE(w.imdb_id,''),COALESCE(w.tmdb_id,''),COALESCE(w.tvdb_id,''),review.provider_id IS NOT NULL`
	query := "SELECT " + columns + from + ` ORDER BY COALESCE(t.search_published_at,t.first_seen_at) DESC,t.provider_id,t.source_id`
	if !filtered {
		query += " LIMIT " + bind(q.Limit) + " OFFSET " + bind(q.Offset)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return result, databaseError("search Torznab catalog", err)
	}
	for rows.Next() {
		var item model.TorznabRecord
		var imdb, tmdb, tvdb string
		var reviewed bool
		rec, e := scanAIORecord(aioStreamRow{scanner: rows, ids: []any{&item.OccurrenceID, &item.Category, &imdb, &tmdb, &tvdb, &reviewed}})
		if e != nil {
			rows.Close()
			return result, e
		}
		rec.IMDb, rec.TMDB, rec.TVDB, rec.ReviewedEpisode = imdb, tmdb, tvdb, reviewed
		item.AIORecord = rec
		if filtered {
			if !episodeMatch(rec) {
				continue
			}
			result.Total++
			if result.Total <= int64(q.Offset) || len(result.Items) >= q.Limit {
				continue
			}
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, databaseError("read Torznab results", err)
	}
	return result, nil
}
