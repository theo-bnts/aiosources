package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

func scanEvent(row scanner) (model.Event, error) {
	var event model.Event
	var data []byte
	if err := row.Scan(&event.ID, &event.RunID, &event.Kind, &event.Message, &data, &event.CreatedAt); err != nil {
		return event, err
	}
	var err error
	event.Data, err = decodeObject(data)
	return event, err
}

func (s *Store) AddEvent(ctx context.Context, runID, kind, message string, data map[string]any) (model.Event, error) {
	encoded, err := jsonObject(data)
	if err != nil {
		return model.Event{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Event{}, databaseError("begin collection event", err)
	}
	defer rollback(tx)
	// Serialize event ID allocation with this run's other transactions so
	// chronological pages keep their order as new events commit.
	var id string
	if err := tx.QueryRow(ctx, "SELECT id FROM ingest.runs WHERE id=$1 FOR UPDATE", runID).Scan(&id); err != nil {
		return model.Event{}, databaseError("read event collection", err)
	}
	event, err := scanEvent(tx.QueryRow(ctx, `INSERT INTO ingest.events(run_id,kind,message,data)
VALUES($1,$2,$3,$4) RETURNING id,run_id,kind,message,data,created_at`, runID, kind, message, encoded))
	if err != nil {
		return model.Event{}, databaseError("add collection event", err)
	}
	if err := notify(ctx, tx); err != nil {
		return model.Event{}, databaseError("notify collection event", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Event{}, databaseError("commit collection event", err)
	}
	return event, nil
}

func (s *Store) Events(ctx context.Context, runID string, options model.ListOptions) (model.List[model.Event], error) {
	options = pageOptions(options)
	result := model.List[model.Event]{Items: []model.Event{}, Limit: options.Limit, Offset: options.Offset}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, databaseError("begin collection events query", err)
	}
	defer rollback(tx)
	// Count and rows share a snapshot; a missing run is not an empty event list.
	if err := tx.QueryRow(ctx, "SELECT (SELECT count(*) FROM ingest.events WHERE run_id=$1) FROM ingest.runs WHERE id=$1", runID).Scan(&result.Total); err != nil {
		return result, databaseError("count collection events", err)
	}
	rows, err := tx.Query(ctx, "SELECT id,run_id,kind,message,data,created_at FROM ingest.events WHERE run_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3", runID, options.Limit, options.Offset)
	if err != nil {
		return result, databaseError("list collection events", err)
	}
	defer rows.Close()
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return result, databaseError("read collection event", err)
		}
		result.Items = append(result.Items, event)
	}
	if err := rows.Err(); err != nil {
		return result, databaseError("list collection events", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, databaseError("finish collection events query", err)
	}
	return result, nil
}
