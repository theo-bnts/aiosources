package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

type CatalogTorrentPolicy struct {
	Public          bool
	UncachedSources []string
	Services        []string
	CacheAfter      time.Time
	CachedOnly      bool
	SourceNames     map[string]string
}

// Direct Search reads published torrents without requiring a metadata match.
// Visibility and cache filtering happen before counting/pagination, so private
// catalog counts cannot leak through a page of otherwise hidden occurrences.
func (s *Store) CatalogTorrentRecords(ctx context.Context, opts model.ListOptions, sorting []model.ResultSort, policy CatalogTorrentPolicy) (model.List[CatalogRecord], error) {
	var err error
	base := opts
	base.Sort = "recent"
	opts, err = torrentSearchOptions(base)
	result := model.List[CatalogRecord]{Items: []CatalogRecord{}, Limit: opts.Limit, Offset: opts.Offset}
	if err != nil || len(strings.TrimSpace(opts.Query)) < 2 || policy.CacheAfter.IsZero() {
		return result, model.ErrInvalid
	}
	for _, service := range policy.Services {
		if !model.IsDebridService(service) {
			return result, model.ErrInvalid
		}
	}
	if !((model.ResultPreferences{Sorting: sorting}).Valid(true)) {
		return result, model.ErrInvalid
	}
	filter, _, filterArgs, _ := torrentSearchSQL(opts)
	filter = strings.Replace(filter, " FROM aiosources.torrents", " FROM aiosources.torrents AS t", 1)
	args := slices.Clone(filterArgs)
	bind := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	add := func(clause string) {
		if strings.Contains(filter, " WHERE ") {
			filter += " AND " + clause
		} else {
			filter += " WHERE " + clause
		}
	}
	add(`jsonb_typeof(t.fields->'title')='string' AND t.fields->>'title'<>''`)
	if policy.Public {
		add(`length(t.search_info_hash)=40`)
	}
	availability := ""
	known := func(condition string) string {
		if availability == "" {
			availability = `a.hash=t.search_info_hash AND a.service=ANY(` + bind(policy.Services) + `::text[]) AND a.checked_at>` + bind(policy.CacheAfter) + ` AND a.checked_at<=now()`
		}
		return `EXISTS(SELECT 1 FROM aiosources.aio_shared_availability a WHERE ` + availability + condition + `)`
	}
	if policy.Public {
		// Private direct results require a verified file list. No provider or
		// admin tracker credential is used to discover missing private entries.
		publicCached := known(` AND a.cached AND aiosources.catalog_has_download_file(a.files)`)
		add(`(t.provider_id=ANY(` + bind(policy.UncachedSources) + `::text[]) OR ` + publicCached + `)`)
	}
	if policy.CachedOnly {
		add(known(` AND a.cached AND (jsonb_array_length(a.files)=0 OR aiosources.catalog_has_download_file(a.files))`))
	}
	countArgs := slices.Clone(args)
	order := []string{}
	for _, rule := range sorting {
		column := ""
		direction := strings.ToUpper(rule.Direction)
		switch rule.Field {
		case "size":
			column = "t.search_size"
		case "name":
			column = `lower(t.fields->>'title') COLLATE "C"`
		case "source":
			names, e := json.Marshal(policy.SourceNames)
			if e != nil {
				return result, model.ErrInvalid
			}
			column = "lower(COALESCE(" + bind(string(names)) + `::jsonb->>t.provider_id,t.provider_id)) COLLATE "C"`
		case "published", "age":
			column = "t.search_published_at"
			if rule.Field == "age" {
				if direction == "ASC" {
					direction = "DESC"
				} else {
					direction = "ASC"
				}
			}
		case "cached":
			column = "(CASE WHEN " + known(" AND a.cached") + " THEN 2 WHEN " + known("") + " THEN 0 ELSE 1 END)"
		default:
			return result, model.ErrInvalid
		}
		order = append(order, column+" "+direction+" NULLS LAST")
	}
	if len(order) == 0 {
		order = append(order, "t.search_size DESC NULLS LAST")
	}
	order = append(order, "t.provider_id", "t.source_id")
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	// Bound display evidence independently of source-specific retained extras.
	// The original fingerprint still guards the complete current source record.
	columns := `t.provider_id,t.source_id,(SELECT COALESCE(jsonb_object_agg(key,value),'{}'::jsonb) FROM jsonb_each(t.fields) WHERE key=ANY(` + bind(model.CompactTorrentFields()) + `::text[]) AND pg_column_size(value)<=16384),t.fields_fingerprint,t.occurrence_id`
	limit, offset := bind(opts.Limit), bind(opts.Offset)
	rows, err := tx.Query(ctx, `SELECT count(*) OVER(),`+columns+filter+` ORDER BY `+strings.Join(order, ",")+` LIMIT `+limit+` OFFSET `+offset, args...)
	if err != nil {
		return result, databaseError("search direct torrents", err)
	}
	for rows.Next() {
		var occurrence string
		record, e := scanAIORecord(aioStreamRow{scanner: countedCatalogRow{row: rows, total: &result.Total}, ids: []any{&occurrence}})
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, CatalogRecord{AIORecord: record, OccurrenceID: occurrence})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, databaseError("read direct torrents", err)
	}
	if len(result.Items) == 0 && opts.Offset > 0 {
		if err = tx.QueryRow(ctx, "SELECT count(*)"+filter, countArgs...).Scan(&result.Total); err != nil {
			return result, databaseError("count direct torrents", err)
		}
	}
	return result, databaseError("finish direct torrent search", tx.Commit(ctx))
}

func (s *Store) CatalogTorrent(ctx context.Context, occurrence string) (CatalogRecord, error) {
	var result CatalogRecord
	if !ValidOccurrenceID(occurrence) {
		return result, model.ErrNotFound
	}
	r, err := scanAIORecord(aioStreamRow{scanner: s.pool.QueryRow(ctx, `SELECT `+aioRecordColumns+`,t.occurrence_id FROM aiosources.torrents t WHERE t.occurrence_id=$1`, occurrence), ids: []any{&result.OccurrenceID}})
	result.AIORecord = r
	return result, databaseError("read direct torrent", err)
}

func (s *Store) CatalogSourceTorrent(ctx context.Context, provider, source string) (CatalogRecord, error) {
	var result CatalogRecord
	r, err := scanAIORecord(aioStreamRow{scanner: s.pool.QueryRow(ctx, `SELECT `+aioRecordColumns+`,t.occurrence_id FROM aiosources.torrents t WHERE t.provider_id=$1 AND t.source_id=$2`, provider, source), ids: []any{&result.OccurrenceID}})
	result.AIORecord = r
	return result, databaseError("read direct torrent", err)
}
