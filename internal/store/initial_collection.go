package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Called under the provider queue lock, before inserting a new run. Existing
// continuations keep their mode, cursor, budget and immutable provider snapshot.
func initialCollectionMode(ctx context.Context, tx pgx.Tx, provider model.Provider, requested model.RunMode) (model.RunMode, bool, error) {
	if requested != model.ModeIncremental {
		return requested, false, nil
	}
	var initialized bool
	query := `SELECT EXISTS(SELECT 1 FROM ingest.collection_state WHERE provider_id=$1)`
	args := []any{provider.ID}
	if remoteCatalog(provider) {
		// Remote deltas require a checkpoint for this exact publisher endpoint.
		query = `SELECT EXISTS(SELECT 1 FROM ingest.remote_checkpoints
WHERE provider_id=$1 AND endpoint=$2 AND checkpoint<>'')`
		args = append(args, provider.URL)
	}
	if err := tx.QueryRow(ctx, query, args...).Scan(&initialized); err != nil {
		return "", false, databaseError("read initial collection state", err)
	}
	if initialized {
		return requested, false, nil
	}
	return model.ModeFull, true, nil
}

func initialFullEvent(ctx context.Context, tx pgx.Tx, run model.Run) error {
	if !run.InitialFull {
		return nil
	}
	return databaseError("record initial collection", lifecycleEvent(ctx, tx, run.ID, "initial_full",
		"Incremental request starts with Full because this source has no successful collection baseline"))
}

// Successful discovery stays initialized after its debug history expires. The
// earliest trustworthy acceptance of each live identity remains a safe boundary.
func retainCollectionBaseline(ctx context.Context, tx pgx.Tx, run model.Run) error {
	if !remoteCatalog(run.Config) {
		if _, err := tx.Exec(ctx, `INSERT INTO ingest.collection_state(provider_id,initialized_at)
VALUES($1,$2) ON CONFLICT DO NOTHING`, run.ProviderID, run.FinishedAt); err != nil {
			return databaseError("retain collection initialization", err)
		}
	}
	_, err := tx.Exec(ctx, `UPDATE ingest.torrents t SET collection_verified_at=r.finished_at
FROM ingest.runs r JOIN ingest.run_record_identities accepted ON accepted.run_id=r.id
WHERE r.id=$1 AND (r.mode='full' OR NOT r.rejected_primary)
AND t.provider_id=r.provider_id AND t.source_id=accepted.source_id AND t.first_seen_at<=r.finished_at
AND (t.collection_verified_at IS NULL OR t.collection_verified_at<t.first_seen_at)`, run.ID)
	return databaseError("retain accepted collection baseline", err)
}
