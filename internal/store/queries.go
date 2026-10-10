package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

func decodeObject(data []byte) (map[string]any, error) {
	value := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("stored structured fields are invalid")
	}
	if value == nil {
		value = make(map[string]any)
	}
	return value, nil
}

const rawColumns = `record.id,record.run_id,record.provider_id,record.source_id,record.page_index,record.content_type,
COALESCE(payload.fields,record.fields),record.error,record.ignored,record.auxiliary,record.created_at,record.page_id,record.payload_retained`

const rawSource = ` FROM ingest.raw_records AS record
LEFT JOIN ingest.raw_records AS payload ON payload.id=record.payload_id`

func scanRaw(row scanner, includeBytes bool) (model.RawRecord, error) {
	var record model.RawRecord
	var fields []byte
	values := []any{&record.ID, &record.RunID, &record.ProviderID, &record.SourceID, &record.Page, &record.ContentType, &fields, &record.Error, &record.Ignored, &record.Auxiliary, &record.CreatedAt, &record.PageID, &record.PayloadRetained}
	if includeBytes {
		values = append(values, &record.Raw)
	}
	if err := row.Scan(values...); err != nil {
		return record, err
	}
	var err error
	record.Fields, err = decodeObject(fields)
	return record, err
}

func (s *Store) ListRaw(ctx context.Context, opts model.ListOptions) (model.RawListing, error) {
	opts = pageOptions(opts)
	result := model.RawListing{List: model.List[model.RawRecord]{Items: []model.RawRecord{}, Limit: opts.Limit, Offset: opts.Offset}}
	if opts.Status != "" && opts.Status != "accepted" && opts.Status != "error" && opts.Status != "ignored" && opts.Status != "enrichment" {
		return result, model.ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin raw record listing", err)
	}
	defer rollback(tx)
	selection, args := rawSelection(opts)
	const processing = `CASE WHEN record.error<>'' THEN 'error' WHEN record.auxiliary THEN 'enrichment' WHEN record.ignored THEN 'ignored' ELSE 'accepted' END`
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE `+processing+`='accepted'),count(*) FILTER(WHERE `+processing+`='error'),count(*) FILTER(WHERE `+processing+`='ignored'),count(*) FILTER(WHERE `+processing+`='enrichment') FROM (`+selection+`) AS record`, args...).Scan(&result.Counts.Accepted, &result.Counts.Errors, &result.Counts.Ignored, &result.Counts.Enrichment); err != nil {
		return result, databaseError("count raw records", err)
	}
	switch opts.Status {
	case "accepted":
		result.Total = result.Counts.Accepted
	case "error":
		result.Total = result.Counts.Errors
	case "ignored":
		result.Total = result.Counts.Ignored
	case "enrichment":
		result.Total = result.Counts.Enrichment
	default:
		result.Total = result.Counts.Accepted + result.Counts.Errors + result.Counts.Ignored + result.Counts.Enrichment
	}
	statusFilter := ""
	if opts.Status != "" {
		args = append(args, opts.Status)
		statusFilter = " WHERE " + processing + "=$" + strconv.Itoa(len(args))
	}
	args = append(args, opts.Limit, opts.Offset)
	page := "SELECT id FROM (" + selection + ") AS record" + statusFilter + " ORDER BY id DESC LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	// Resolve archived fields only for the selected page, rather than joining
	// every observation to its payload before searching and counting.
	rows, err := tx.Query(ctx, "SELECT "+rawColumns+rawSource+" JOIN ("+page+") AS selected ON selected.id=record.id ORDER BY record.id DESC", args...)
	if err != nil {
		return result, databaseError("list raw records", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanRaw(rows, false)
		if err != nil {
			return result, databaseError("read raw record metadata", err)
		}
		result.Items = append(result.Items, record)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list raw records", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, databaseError("finish raw record listing", err)
	}
	return result, nil
}

func rawSelection(opts model.ListOptions) (string, []any) {
	var args []any
	bind := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	var filters []string
	if opts.ProviderID != "" {
		filters = append(filters, "record.provider_id="+bind(opts.ProviderID))
	}
	if opts.RunID != "" {
		filters = append(filters, "record.run_id="+bind(opts.RunID))
	}
	const columns = "SELECT record.id,record.error,record.ignored,record.auxiliary FROM ingest.raw_records AS record"
	where := func(extra string) string {
		terms := append([]string{}, filters...)
		if extra != "" {
			terms = append(terms, extra)
		}
		if len(terms) == 0 {
			return ""
		}
		return " WHERE " + strings.Join(terms, " AND ")
	}
	if opts.Query == "" {
		return columns + where(""), args
	}
	pattern, literal := bind("%"+escapeSearchFragment.Replace(opts.Query)+"%"), bind(opts.Query)
	direct := "ingest.raw_search_text(record.source_id,record.fields,record.error) ILIKE " + pattern +
		" AND (strpos(lower(record.source_id),lower(" + literal + "))>0 OR (record.payload_id IS NULL AND strpos(lower(record.fields::text),lower(" + literal + "))>0) OR strpos(lower(record.error),lower(" + literal + "))>0)"
	referenced := "record.payload_id IS NOT NULL AND ingest.raw_search_text(payload.source_id,payload.fields,payload.error) ILIKE " + pattern +
		" AND strpos(lower(payload.fields::text),lower(" + literal + "))>0"
	// UNION preserves one observation when its own identity/error and the
	// referenced fields both match. Source/run filters apply to the observation.
	return columns + where(direct) + " UNION " + columns + " JOIN ingest.raw_records AS payload ON payload.id=record.payload_id" + where(referenced), args
}

func (s *Store) Raw(ctx context.Context, id int64) (model.RawRecord, error) {
	record, err := scanRaw(s.pool.QueryRow(ctx, "SELECT "+rawColumns+",COALESCE(payload.raw,record.raw)"+rawSource+" WHERE record.id=$1", id), true)
	return record, databaseError("read raw record", err)
}

func (s *Store) RawPage(ctx context.Context, id int64) ([]byte, string, error) {
	var body []byte
	var contentType string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(payload.body,page.body),page.content_type
FROM ingest.pages AS page LEFT JOIN ingest.pages AS payload ON payload.id=page.payload_id
WHERE page.id=$1 AND page.payload_retained`, id).Scan(&body, &contentType)
	return body, contentType, databaseError("read response page", err)
}

const torrentColumns = `provider_id,source_id,fields,raw_id,first_seen_at,last_seen_at,historical,origin,occurrence_id`

func scanTorrent(row scanner) (model.Torrent, error) {
	var torrent model.Torrent
	var fields []byte
	var origin []byte
	if err := row.Scan(&torrent.ProviderID, &torrent.SourceID, &fields, &torrent.RawID, &torrent.FirstSeenAt, &torrent.LastSeenAt, &torrent.Historical, &origin, &torrent.OccurrenceID); err != nil {
		return torrent, err
	}
	if len(origin) != 0 {
		if err := json.Unmarshal(origin, &torrent.Origin); err != nil {
			return torrent, errors.New("stored catalogue provenance is invalid")
		}
	}
	var err error
	torrent.Fields, err = decodeObject(fields)
	return torrent, err
}

func (s *Store) ListTorrents(ctx context.Context, opts model.ListOptions) (model.List[model.Torrent], error) {
	var err error
	opts, err = torrentSearchOptions(opts)
	if err != nil {
		return model.List[model.Torrent]{}, err
	}
	result := model.List[model.Torrent]{Items: []model.Torrent{}, Limit: opts.Limit, Offset: opts.Offset}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin torrent listing", err)
	}
	defer rollback(tx)
	filter, order, filterArgs, args := torrentSearchSQL(opts)
	if err := tx.QueryRow(ctx, "SELECT count(*)"+filter, filterArgs...).Scan(&result.Total); err != nil {
		return result, databaseError("count torrents", err)
	}
	limitParameter := "$" + strconv.Itoa(len(args)+1)
	offsetParameter := "$" + strconv.Itoa(len(args)+2)
	args = append(args, opts.Limit, opts.Offset)
	rows, err := tx.Query(ctx, "SELECT "+torrentColumns+filter+order+" LIMIT "+limitParameter+" OFFSET "+offsetParameter, args...)
	if err != nil {
		return result, databaseError("list torrents", err)
	}
	defer rows.Close()
	for rows.Next() {
		torrent, err := scanTorrent(rows)
		if err != nil {
			return result, databaseError("read torrent", err)
		}
		result.Items = append(result.Items, torrent)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list torrents", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, databaseError("finish torrent listing", err)
	}
	return result, nil
}
