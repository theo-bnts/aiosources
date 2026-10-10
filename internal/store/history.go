package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

const publicationChangeColumns = "id::text,COALESCE(run_id,''),kind,occurred_at,before_fields,after_fields,origin,before_origin"

func ValidOccurrenceID(id string) bool {
	if len(id) != 64 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func scanPublicationChange(row scanner, occurrence bool) (model.OccurrenceChange, error) {
	var item model.OccurrenceChange
	var before, after, origin, beforeOrigin []byte
	values := []any{&item.ID, &item.RunID, &item.Kind, &item.OccurredAt, &before, &after, &origin, &beforeOrigin}
	if occurrence {
		values = append(values, &item.OccurrenceID, &item.ProviderID, &item.SourceID)
	}
	if err := row.Scan(values...); err != nil {
		return item, err
	}
	var err error
	if len(before) > 0 {
		item.Before, err = decodeObject(before)
		if err != nil {
			return item, err
		}
	}
	if len(after) > 0 {
		item.After, err = decodeObject(after)
		if err != nil {
			return item, err
		}
	}
	if len(origin) > 0 {
		if err = json.Unmarshal(origin, &item.Origin); err != nil {
			return item, model.ErrInvalid
		}
	}
	if len(beforeOrigin) > 0 {
		if err = json.Unmarshal(beforeOrigin, &item.BeforeOrigin); err != nil {
			return item, model.ErrInvalid
		}
	}
	return item, nil
}

type occurrenceState struct {
	ProviderID string
	SourceID   string
	Origin     *model.CatalogOrigin
	Hash       *string
}

func readOccurrenceState(ctx context.Context, tx pgx.Tx, id string) (occurrenceState, error) {
	var state occurrenceState
	var origin []byte
	// The original provider/source pair remains addressable after deletion.
	// Baseline tombstones use the last known journal fields for related links,
	// without representing that old state as a newly observed transition.
	err := tx.QueryRow(ctx, `SELECT provider_id,source_id,origin,ingest.torrent_hash(fields->'info_hash') FROM (
    (SELECT provider_id,source_id,ingest.catalog_origin(origin,provider_id,source_id) AS origin,fields,0 AS priority
     FROM ingest.torrents WHERE occurrence_id=$1)
    UNION ALL
    (SELECT provider_id,source_id,origin,COALESCE(after_fields,before_fields) AS fields,1 AS priority
     FROM ingest.torrent_history WHERE occurrence_id=$1 ORDER BY id DESC LIMIT 1)
    UNION ALL
    (SELECT baseline.provider_id,baseline.source_id,baseline.origin,
        COALESCE(baseline.fields,(SELECT journal.fields FROM ingest.catalog_journal AS journal
          WHERE journal.provider_id=baseline.provider_id AND journal.source_id=baseline.source_id AND NOT journal.deleted
          ORDER BY journal.sequence DESC LIMIT 1)) AS fields,2 AS priority
     FROM ingest.torrent_history_baselines AS baseline WHERE occurrence_id=$1)
) AS states ORDER BY priority LIMIT 1`, id).Scan(&state.ProviderID, &state.SourceID, &origin, &state.Hash)
	if err != nil {
		return state, databaseError("read occurrence identity", err)
	}
	if err := json.Unmarshal(origin, &state.Origin); err != nil {
		return state, model.ErrInvalid
	}
	return state, nil
}

func (s *Store) TorrentHistory(ctx context.Context, id string, options model.ListOptions) (model.OccurrenceHistory, error) {
	options = pageOptions(options)
	result := model.OccurrenceHistory{OccurrenceID: id, Items: []model.PublicationChange{}, Limit: options.Limit, Offset: options.Offset}
	if !ValidOccurrenceID(id) {
		return result, model.ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin occurrence history", err)
	}
	defer rollback(tx)
	state, err := readOccurrenceState(ctx, tx, id)
	if err != nil {
		return result, err
	}
	result.ProviderID = state.ProviderID
	result.SourceID = state.SourceID
	result.Origin = state.Origin
	if err := tx.QueryRow(ctx, "SELECT history_since FROM ingest.torrent_history_metadata WHERE singleton").Scan(&result.HistorySince); err != nil {
		return result, databaseError("read history coverage", err)
	}
	var baseline model.HistoryBaseline
	var fields, origin []byte
	err = tx.QueryRow(ctx, "SELECT fields,origin,deleted,recorded_at FROM ingest.torrent_history_baselines WHERE occurrence_id=$1", id).Scan(&fields, &origin, &baseline.Deleted, &baseline.RecordedAt)
	if err != nil && err != pgx.ErrNoRows {
		return result, databaseError("read history baseline", err)
	}
	if err == nil {
		if len(fields) > 0 {
			baseline.Fields, err = decodeObject(fields)
			if err != nil {
				return result, err
			}
		}
		if err := json.Unmarshal(origin, &baseline.Origin); err != nil {
			return result, model.ErrInvalid
		}
		result.Baseline = &baseline
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM ingest.torrent_history WHERE occurrence_id=$1", id).Scan(&result.Total); err != nil {
		return result, databaseError("count occurrence changes", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+publicationChangeColumns+" FROM ingest.torrent_history WHERE occurrence_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3", id, options.Limit, options.Offset)
	if err != nil {
		return result, databaseError("list occurrence changes", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanPublicationChange(rows, false)
		if err != nil {
			return result, databaseError("read occurrence change", err)
		}
		result.Items = append(result.Items, item.PublicationChange)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list occurrence changes", err)
	}
	return result, databaseError("finish occurrence history", tx.Commit(ctx))
}

func (s *Store) RelatedTorrents(ctx context.Context, id string, options model.ListOptions) (model.List[model.Torrent], error) {
	options = pageOptions(options)
	result := model.List[model.Torrent]{Items: []model.Torrent{}, Limit: options.Limit, Offset: options.Offset}
	if !ValidOccurrenceID(id) {
		return result, model.ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin related occurrences", err)
	}
	defer rollback(tx)
	state, err := readOccurrenceState(ctx, tx, id)
	if err != nil {
		return result, err
	}
	if state.Hash == nil {
		return result, databaseError("finish related occurrences", tx.Commit(ctx))
	}
	const filter = " FROM ingest.torrents WHERE search_info_hash=$1 AND occurrence_id<>$2"
	if err := tx.QueryRow(ctx, "SELECT count(*)"+filter, *state.Hash, id).Scan(&result.Total); err != nil {
		return result, databaseError("count related occurrences", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+torrentColumns+filter+" ORDER BY provider_id,source_id LIMIT $3 OFFSET $4", *state.Hash, id, options.Limit, options.Offset)
	if err != nil {
		return result, databaseError("list related occurrences", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanTorrent(rows)
		if err != nil {
			return result, databaseError("read related occurrence", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list related occurrences", err)
	}
	return result, databaseError("finish related occurrences", tx.Commit(ctx))
}

func (s *Store) RunChanges(ctx context.Context, id string, options model.ListOptions) (model.RunChanges, error) {
	options = pageOptions(options)
	result := model.RunChanges{Items: []model.OccurrenceChange{}, Limit: options.Limit, Offset: options.Offset}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin run changes", err)
	}
	defer rollback(tx)
	if err := tx.QueryRow(ctx, `SELECT metadata.history_since,COALESCE(run.started_at,run.created_at)>=metadata.history_since
FROM ingest.runs AS run CROSS JOIN ingest.torrent_history_metadata AS metadata WHERE run.id=$1 AND metadata.singleton`, id).Scan(&result.HistorySince, &result.HistoryComplete); err != nil {
		return result, databaseError("read run history coverage", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE kind='added'),count(*) FILTER (WHERE kind='updated'),count(*) FILTER (WHERE kind='deleted')
FROM ingest.torrent_history WHERE run_id=$1`, id).Scan(&result.Total, &result.Counts.Added, &result.Counts.Updated, &result.Counts.Deleted); err != nil {
		return result, databaseError("count run changes", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+publicationChangeColumns+",occurrence_id,provider_id,source_id FROM ingest.torrent_history WHERE run_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3", id, options.Limit, options.Offset)
	if err != nil {
		return result, databaseError("list run changes", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanPublicationChange(rows, true)
		if err != nil {
			return result, databaseError("read run change", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list run changes", err)
	}
	return result, databaseError("finish run changes", tx.Commit(ctx))
}
