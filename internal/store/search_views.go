package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

const savedViewColumns = "id,name,filters,revision,created_at,updated_at"

func scanSavedView(row scanner) (model.SavedView, error) {
	var item model.SavedView
	var filters []byte
	if err := row.Scan(&item.ID, &item.Name, &filters, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal(filters, &item.Filters); err != nil {
		return item, model.ErrInvalid
	}
	return item, nil
}

func (s *Store) SavedViews(ctx context.Context) ([]model.SavedView, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+savedViewColumns+" FROM ingest.saved_views ORDER BY created_at,id")
	if err != nil {
		return nil, databaseError("list saved views", err)
	}
	defer rows.Close()
	items := []model.SavedView{}
	for rows.Next() {
		item, err := scanSavedView(rows)
		if err != nil {
			return nil, databaseError("read saved view", err)
		}
		items = append(items, item)
	}
	return items, databaseError("list saved views", rows.Err())
}

func (s *Store) SaveView(ctx context.Context, id string, input model.SavedViewInput) (model.SavedView, error) {
	if input.Name == "" || strings.TrimSpace(input.Name) != input.Name || !validSearchText(input.Name, 120) || (id == "" && input.Revision != 0) || (id != "" && input.Revision < 1) {
		return model.SavedView{}, model.ErrInvalid
	}
	filters, err := NormalizeSearchFilters(input.Filters)
	if err != nil {
		return model.SavedView{}, err
	}
	encoded, err := json.Marshal(filters)
	if err != nil {
		return model.SavedView{}, model.ErrInvalid
	}
	creating := id == ""
	if creating {
		id, err = newRunID()
		if err != nil {
			return model.SavedView{}, err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.SavedView{}, databaseError("begin saved view mutation", err)
	}
	defer rollback(tx)
	var item model.SavedView
	if creating {
		item, err = scanSavedView(tx.QueryRow(ctx, "INSERT INTO ingest.saved_views(id,name,filters) VALUES($1,$2,$3) RETURNING "+savedViewColumns, id, input.Name, encoded))
	} else {
		var revision int64
		if err := tx.QueryRow(ctx, "SELECT revision FROM ingest.saved_views WHERE id=$1 FOR UPDATE", id).Scan(&revision); err != nil {
			return item, databaseError("lock saved view", err)
		}
		if revision != input.Revision {
			return item, model.ErrConflict
		}
		item, err = scanSavedView(tx.QueryRow(ctx, "UPDATE ingest.saved_views SET name=$2,filters=$3,revision=revision+1,updated_at=NOW() WHERE id=$1 RETURNING "+savedViewColumns, id, input.Name, encoded))
	}
	if err != nil {
		return item, databaseError("save view", err)
	}
	return item, databaseError("commit saved view mutation", tx.Commit(ctx))
}

func (s *Store) DeleteSavedView(ctx context.Context, id string, revision int64) error {
	if revision < 1 {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin saved view deletion", err)
	}
	defer rollback(tx)
	var current int64
	if err := tx.QueryRow(ctx, "SELECT revision FROM ingest.saved_views WHERE id=$1 FOR UPDATE", id).Scan(&current); err != nil {
		return databaseError("lock saved view", err)
	}
	if current != revision {
		return model.ErrConflict
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.saved_views WHERE id=$1", id); err != nil {
		return databaseError("delete saved view", err)
	}
	return databaseError("commit saved view deletion", tx.Commit(ctx))
}
