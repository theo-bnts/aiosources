package store

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

const collectionSettingsSchema = `
CREATE TABLE ingest.collection_settings (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
    workers INTEGER CHECK(workers BETWEEN 1 AND 32),
    revision BIGINT NOT NULL DEFAULT 1 CHECK(revision > 0)
);
-- NULL preserves the first serve invocation's --workers bootstrap value.
INSERT INTO ingest.collection_settings(singleton) VALUES(TRUE);
`

// InitializeCollectionSettings seeds only an unset value, never a saved setting.
func (s *Store) InitializeCollectionSettings(ctx context.Context, workers int) error {
	if workers < 1 || workers > model.MaxCollectionWorkers {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin collection settings initialization", err)
	}
	defer rollback(tx)
	result, err := tx.Exec(ctx, "UPDATE ingest.collection_settings SET workers=$1 WHERE singleton AND workers IS NULL", workers)
	if err != nil {
		return databaseError("initialize collection settings", err)
	}
	if result.RowsAffected() != 0 {
		if err := notify(ctx, tx); err != nil {
			return databaseError("notify collection settings initialization", err)
		}
	}
	return databaseError("commit collection settings initialization", tx.Commit(ctx))
}

const collectionSettingsColumns = `COALESCE(workers,5),max_quota_retries,max_quota_wait_seconds,no_progress_requests,no_progress_action,revision,auto_resume_interrupted`

func scanCollectionSettings(row scanner) (model.CollectionSettings, error) {
	var value model.CollectionSettings
	err := row.Scan(&value.Workers, &value.MaxQuotaRetries, &value.MaxQuotaWaitSeconds,
		&value.NoProgressRequests, &value.NoProgressAction, &value.Revision, &value.AutoResumeInterrupted)
	return value, databaseError("read collection settings", err)
}

func (s *Store) CollectionSettings(ctx context.Context) (model.CollectionSettings, error) {
	return scanCollectionSettings(s.pool.QueryRow(ctx, "SELECT "+collectionSettingsColumns+" FROM ingest.collection_settings WHERE singleton"))
}

func (s *Store) CollectionOverview(ctx context.Context) (model.CollectionOverview, error) {
	var result model.CollectionOverview
	var err error
	result.Settings, err = s.CollectionSettings(ctx)
	if err != nil {
		return result, err
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='running'),
count(*) FILTER (WHERE status='queued'),
count(*) FILTER (WHERE status='running' AND NOT remote_catalog),
count(*) FILTER (WHERE status='queued' AND NOT remote_catalog)
FROM ingest.runs WHERE status IN ('running','queued')`).Scan(&result.Running, &result.Queued, &result.SourceRunning, &result.SourceQueued)
	result.Capacity = min(cap(s.claimSlots), model.MaxCollectionWorkers)
	return result, databaseError("read collection capacity", err)
}

func (s *Store) UpdateCollectionSettings(ctx context.Context, input model.CollectionSettings) error {
	if !input.Valid() {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin collection settings update", err)
	}
	defer rollback(tx)
	result, err := tx.Exec(ctx, `UPDATE ingest.collection_settings SET workers=$1,max_quota_retries=$2,
max_quota_wait_seconds=$3,no_progress_requests=$4,no_progress_action=$5,auto_resume_interrupted=COALESCE($7,auto_resume_interrupted),
revision=revision+1 WHERE singleton AND revision=$6`,
		input.Workers, input.MaxQuotaRetries, input.MaxQuotaWaitSeconds,
		input.NoProgressRequests, input.NoProgressAction, input.Revision, input.AutoResumeInterrupted)
	if err != nil {
		return databaseError("update collection settings", err)
	}
	if result.RowsAffected() == 0 {
		return model.ErrConflict
	}
	if err := notify(ctx, tx); err != nil {
		return databaseError("notify collection settings update", err)
	}
	return databaseError("commit collection settings update", tx.Commit(ctx))
}
