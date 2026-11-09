package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *Store) StorageRetention(ctx context.Context) (model.StorageRetentionSettings, error) {
	var result model.StorageRetentionSettings
	var encoded []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,days,request_limit,request_results FROM aiosources.storage_retention_settings WHERE singleton`).Scan(&result.Revision, &encoded, &result.RequestLimit, &result.RequestResults)
	if err == nil {
		err = json.Unmarshal(encoded, &result.Days)
	}
	if err == nil && !result.Valid() {
		return result, model.ErrInvalid
	}
	return result, databaseError("read retention overrides", err)
}
func (s *Store) UpdateStorageRetention(ctx context.Context, value model.StorageRetentionSettings) error {
	if !value.Valid() {
		return model.ErrInvalid
	}
	encoded, err := json.Marshal(value.Days)
	if err != nil {
		return model.ErrInvalid
	}
	result, err := s.pool.Exec(ctx, `UPDATE aiosources.storage_retention_settings SET days=$1,revision=revision+1,request_limit=$3,request_results=$4 WHERE singleton AND revision=$2`, encoded, value.Revision, value.RequestLimit, value.RequestResults)
	if err != nil {
		return databaseError("save retention overrides", err)
	}
	if result.RowsAffected() == 0 {
		return model.ErrConflict
	}
	return nil
}
func storageRetentionCutoffs(ctx context.Context, tx pgx.Tx) (map[string]time.Time, error) {
	var encoded []byte
	var now time.Time
	var fallback int
	if err := tx.QueryRow(ctx, `SELECT now(),(SELECT (values->>'days')::int FROM aiosources.runtime_settings WHERE section='history'),days FROM aiosources.storage_retention_settings WHERE singleton`).Scan(&now, &fallback, &encoded); err != nil {
		return nil, err
	}
	var days map[string]int
	if json.Unmarshal(encoded, &days) != nil || !(model.StorageRetentionSettings{Revision: 1, Days: days}).Valid() {
		return nil, model.ErrInvalid
	}
	result := map[string]time.Time{}
	for _, key := range model.StorageRetentionGroups {
		n := days[key]
		if n < 0 {
			n = fallback
		}
		result[key] = now.Add(-time.Duration(n) * 24 * time.Hour)
	}
	return result, nil
}
