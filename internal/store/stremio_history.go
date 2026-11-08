package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const stremioResultLimit = 1000

var stremioResourceRE = regexp.MustCompile(`^(imdb:tt[0-9]+|tmdb:[0-9]+|tvdb:[0-9]+|mal:[0-9]+|kitsu:[0-9]+|anilist:[0-9]+)(:[0-9]+){0,2}$`)

func (s *Store) RecordStremioRequest(ctx context.Context, item model.StremioRequest) error {
	if item.Origin == "" {
		item.Origin = "private"
	}
	if item.Kind == "" {
		item.Kind = "stream"
	}
	if (item.Origin != "private" && item.Origin != "public") || (item.Kind != "stream" && item.Kind != "manifest" && item.Kind != "playback") {
		return model.ErrInvalid
	}
	if item.Service != "" && (item.Kind != "playback" || !model.IsDebridService(item.Service)) {
		return model.ErrInvalid
	}
	if len(item.Calls) > 32 {
		item.Calls = item.Calls[:32]
	}
	if item.Calls == nil {
		item.Calls = []model.ProviderCall{}
	}
	for _, call := range item.Calls {
		if !call.Valid() {
			return model.ErrInvalid
		}
	}
	if !stremioResourceRE.MatchString(item.Resource) && item.Resource != "Invalid resource" && !(item.Kind == "manifest" && item.Resource == "Manifest") {
		return model.ErrInvalid
	}
	if len(item.Resource) > 160 || item.DurationMS < 0 || item.LanguageExcluded < 0 || item.Matched < 0 || item.Eligible < 0 || item.InvalidHash < 0 || item.WrongEpisode < 0 || item.PrivateUnavailable < 0 || item.FileUnavailable < 0 || item.Returned < 0 ||
		(item.MediaType != "movie" && item.MediaType != "series" && item.MediaType != "anime" && item.MediaType != "invalid") ||
		(item.Outcome != "results" && item.Outcome != "empty" && item.Outcome != "failed" && item.Outcome != "cancelled" && item.Outcome != "invalid" && item.Outcome != "busy" && item.Outcome != "pending") {
		return model.ErrInvalid
	}
	if len(item.Results) > stremioResultLimit {
		item.Results = item.Results[:stremioResultLimit]
		item.Truncated = true
	}
	if item.Results == nil {
		item.Results = []model.StremioResult{}
	}
	for i := range item.Results {
		result := &item.Results[i]
		// Some adapters use a URL as their native identity. Keep the display
		// summary, without copying that URL into request history.
		if strings.Contains(result.SourceID, "://") || strings.HasPrefix(result.SourceID, "magnet:") {
			result.SourceID = ""
		}
		if (result.Service != "direct" && !model.IsDebridService(result.Service)) || (result.Cache != "direct" && result.Cache != "cached" && result.Cache != "download" && result.Cache != "unknown") || result.Size < 0 {
			return model.ErrInvalid
		}
		result.Title = string([]rune(result.Title)[:min(1024, len([]rune(result.Title)))])
		result.Filename = string([]rune(result.Filename)[:min(1024, len([]rune(result.Filename)))])
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	var requestLimit int
	var requestResults bool
	var cutoff time.Time
	// Hot request paths need only three scalar values, not the entire history
	// configuration decoded into maps on every request.
	if err := s.pool.QueryRow(ctx, `SELECT request_limit,request_results,now()-make_interval(days=>`+stremioRetentionDays+`) FROM ingest.storage_retention_settings WHERE singleton`).Scan(&requestLimit, &requestResults, &cutoff); err != nil {
		return databaseError("read Stremio retention", err)
	}
	if !requestResults && len(item.Results) > 0 {
		item.Results = []model.StremioResult{}
		item.Truncated = true
	}
	encoded, err := json.Marshal(struct {
		model.StremioRequest
		Results []model.StremioResult `json:"results"`
		Calls   []model.ProviderCall  `json:"calls"`
	}{item, item.Results, item.Calls})
	if err != nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin Stremio history", err)
	}
	defer rollback(tx)
	// Serialize the bounded retention pass across concurrent addon calls.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, providerLockKey("stremio-history")); err != nil {
		return databaseError("lock Stremio history", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.stremio_requests(created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results,origin,kind,service,calls,language_excluded)
 SELECT created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,results,origin,kind,service,calls,language_excluded
 FROM jsonb_to_record($1::jsonb) AS x(created_at timestamptz,media_type text,resource text,duration_ms bigint,outcome text,matched int,eligible int,invalid_hash int,wrong_episode int,private_unavailable int,file_unavailable int,returned int,truncated boolean,results jsonb,origin text,kind text,service text,calls jsonb,language_excluded int)`, encoded)
	if err != nil {
		return databaseError("save Stremio request", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.stremio_requests WHERE id IN(SELECT id FROM ingest.stremio_requests WHERE created_at<$1 OR ($2>0 AND id<=COALESCE((SELECT id FROM ingest.stremio_requests ORDER BY id DESC OFFSET $2 LIMIT 1),0)) ORDER BY id LIMIT 5000)`, cutoff, requestLimit); err != nil {
		return databaseError("bound Stremio history", err)
	}
	return databaseError("commit Stremio history", tx.Commit(ctx))
}

const stremioSummaryColumns = `id,created_at,media_type,resource,duration_ms,outcome,matched,eligible,invalid_hash,wrong_episode,private_unavailable,file_unavailable,returned,truncated,origin,kind,service,language_excluded`

// Reads and cleanup use the same group override; increasing the default must
// not hide requests that are still retained by an explicit longer policy.
const stremioRetentionDays = `COALESCE(
 NULLIF((SELECT (days->>'requests')::int FROM ingest.storage_retention_settings WHERE singleton),-1),
 (SELECT (values->>'days')::int FROM ingest.runtime_settings WHERE section='history'))`

const stremioRetained = `created_at>=now()-make_interval(days=>` + stremioRetentionDays + `)`

func scanStremioRequest(row scanner, detail bool) (model.StremioRequest, error) {
	var item model.StremioRequest
	var results, calls []byte
	fields := []any{&item.ID, &item.CreatedAt, &item.MediaType, &item.Resource, &item.DurationMS, &item.Outcome, &item.Matched, &item.Eligible, &item.InvalidHash, &item.WrongEpisode, &item.PrivateUnavailable, &item.FileUnavailable, &item.Returned, &item.Truncated, &item.Origin, &item.Kind, &item.Service, &item.LanguageExcluded}
	if detail {
		fields = append(fields, &results, &calls)
	}
	if err := row.Scan(fields...); err != nil {
		return item, err
	}
	if detail {
		if err := json.Unmarshal(results, &item.Results); err != nil {
			return item, err
		}
		if err := json.Unmarshal(calls, &item.Calls); err != nil {
			return item, err
		}
	}
	return item, nil
}

func (s *Store) ListStremioRequests(ctx context.Context, opts model.ListOptions) (model.List[model.StremioRequest], error) {
	return s.ListStremioRequestsFiltered(ctx, opts, "", "")
}
func (s *Store) ListStremioRequestsFiltered(ctx context.Context, opts model.ListOptions, origin, kind string) (model.List[model.StremioRequest], error) {
	opts = pageOptions(opts)
	out := model.List[model.StremioRequest]{Items: []model.StremioRequest{}, Limit: opts.Limit, Offset: opts.Offset}
	if !model.ValidSelection(origin, func(value string) bool { return value == "private" || value == "public" }) || !model.ValidSelection(kind, func(value string) bool { return value == "stream" || value == "manifest" || value == "playback" }) {
		return out, model.ErrInvalid
	}
	if len(opts.Query) > 160 || !model.ValidSelection(opts.Status, func(value string) bool {
		return value == "results" || value == "empty" || value == "failed" || value == "cancelled" || value == "invalid" || value == "busy" || value == "pending"
	}) {
		return out, model.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, databaseError("begin Stremio request list", err)
	}
	defer rollback(tx)
	const filter = ` WHERE ` + stremioRetained + ` AND ($1='' OR outcome=ANY(string_to_array($1,','))) AND ($2='' OR strpos(lower(resource),lower($2))>0) AND ($3='' OR origin=ANY(string_to_array($3,','))) AND ($4='' OR kind=ANY(string_to_array($4,',')))`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM ingest.stremio_requests`+filter, opts.Status, opts.Query, origin, kind).Scan(&out.Total); err != nil {
		return out, databaseError("count Stremio requests", err)
	}
	rows, err := tx.Query(ctx, `SELECT `+stremioSummaryColumns+` FROM ingest.stremio_requests`+filter+` ORDER BY created_at DESC,id DESC LIMIT $5 OFFSET $6`, opts.Status, opts.Query, origin, kind, opts.Limit, opts.Offset)
	if err != nil {
		return out, databaseError("list Stremio requests", err)
	}
	for rows.Next() {
		item, err := scanStremioRequest(rows, false)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, databaseError("read Stremio requests", err)
	}
	return out, databaseError("finish Stremio request list", tx.Commit(ctx))
}

func (s *Store) StremioRequest(ctx context.Context, id int64) (model.StremioRequest, error) {
	item, err := scanStremioRequest(s.pool.QueryRow(ctx, `SELECT `+stremioSummaryColumns+`,results,calls FROM ingest.stremio_requests WHERE id=$1 AND `+stremioRetained, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, model.ErrNotFound
	}
	return item, databaseError("read Stremio request", err)
}

func (s *Store) StremioStatistics(ctx context.Context) (model.StremioStatistics, error) {
	var out model.StremioStatistics
	err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE outcome='empty'),count(*) FILTER(WHERE outcome IN ('failed','cancelled','invalid','busy')),COALESCE(sum(returned),0),COALESCE(avg(duration_ms),0)::bigint,min(created_at) FROM ingest.stremio_requests WHERE `+stremioRetained).Scan(&out.Requests, &out.Empty, &out.Failed, &out.Results, &out.AverageMS, &out.Since)
	return out, databaseError("read Stremio statistics", err)
}
