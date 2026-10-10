package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

func remoteCatalog(p model.Provider) bool {
	return p.Adapter == "http_json" && p.HTTP.Catalog
}

// RemoteCheckpoint is endpoint-bound: changing a source URL must begin a new
// snapshot, not apply another publisher's delta to the existing namespace.
func (s *Store) RemoteCheckpoint(ctx context.Context, providerID, endpoint string) (string, error) {
	var checkpoint string
	err := s.pool.QueryRow(ctx, "SELECT checkpoint FROM ingest.remote_checkpoints WHERE provider_id=$1 AND endpoint=$2", providerID, endpoint).Scan(&checkpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return checkpoint, databaseError("read remote catalogue checkpoint", err)
}

// Initial state is captured once, under the provider lease and the run row lock.
// It survives an interrupted first request as well as every subsequent resume.
func initializeRemoteCursor(ctx context.Context, tx pgx.Tx, run model.Run) error {
	if !remoteCatalog(run.Config) || len(run.Cursor) != 0 {
		return nil
	}
	state := model.CatalogCursor{Version: 1}
	err := tx.QueryRow(ctx, "SELECT endpoint,checkpoint FROM ingest.remote_checkpoints WHERE provider_id=$1", run.ProviderID).Scan(&state.BaseEndpoint, &state.BaseCheckpoint)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return databaseError("read catalogue publication baseline", err)
	}
	if run.Mode == model.ModeIncremental && state.BaseEndpoint == run.Config.URL {
		state.Checkpoint = state.BaseCheckpoint
	}
	cursor, err := json.Marshal(state)
	if err != nil {
		return errors.New("cannot encode initial catalogue checkpoint")
	}
	_, err = tx.Exec(ctx, "UPDATE ingest.runs SET cursor=$2 WHERE id=$1", run.ID, cursor)
	return databaseError("initialize catalogue continuation", err)
}

func remoteBaselineMatches(ctx context.Context, tx pgx.Tx, run model.Run) (bool, error) {
	var state model.CatalogCursor
	if json.Unmarshal(run.Cursor, &state) != nil || state.Version != 1 {
		return false, model.ErrConflict
	}
	var endpoint, checkpoint string
	err := tx.QueryRow(ctx, "SELECT endpoint,checkpoint FROM ingest.remote_checkpoints WHERE provider_id=$1 FOR UPDATE", run.ProviderID).Scan(&endpoint, &checkpoint)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, databaseError("check catalogue publication baseline", err)
	}
	return endpoint == state.BaseEndpoint && checkpoint == state.BaseCheckpoint, nil
}

type RemoteState struct {
	Endpoint     string
	Checkpoint   string
	LastSyncedAt time.Time
}

// RemoteStates reads durable remote state in one query for a definitions batch.
func (s *Store) RemoteStates(ctx context.Context, providerIDs []string) (map[string]RemoteState, error) {
	result := make(map[string]RemoteState, len(providerIDs))
	if len(providerIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT provider_id,endpoint,checkpoint,last_synced_at FROM ingest.remote_checkpoints WHERE provider_id=ANY($1::text[])", providerIDs)
	if err != nil {
		return nil, databaseError("list remote catalogue checkpoints", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var state RemoteState
		if err := rows.Scan(&id, &state.Endpoint, &state.Checkpoint, &state.LastSyncedAt); err != nil {
			return nil, databaseError("read remote catalogue state", err)
		}
		result[id] = state
	}
	return result, databaseError("list remote catalogue checkpoints", rows.Err())
}

func publishStaged(ctx context.Context, tx pgx.Tx, run model.Run) error {
	mode := run.Mode
	var state model.CatalogCursor
	remote := remoteCatalog(run.Config)
	if remote {
		if json.Unmarshal(run.Cursor, &state) != nil || state.Version != 1 || state.InstanceID == "" || !state.Done || state.Cursor != "" || state.Checkpoint == "" || (state.Mode != "full" && state.Mode != "incremental") {
			return model.ErrConflict
		}
		mode = model.RunMode(state.Mode)
	}
	if mode != model.ModeFull && !remote {
		return nil
	}
	fields := "EXCLUDED.fields"
	args := []any{run.ID}
	if !remote && run.Config.SupportsMetadata() {
		// Only mapped detail-only fields and missing derived attribute keys
		// survive a discovery-only Full. Listing values remain authoritative.
		preserved := []string{"attributes"}
		for key := range run.Config.Traversal.IDRecovery.Mapping.Fields {
			if _, listed := run.Config.Mapping.Fields[key]; !listed && key != "attributes" {
				preserved = append(preserved, key)
			}
		}
		fields = `CASE WHEN previous.origin IS NULL
AND ingest.torrent_hash(previous.fields->'info_hash')=ingest.torrent_hash(EXCLUDED.fields->'info_hash')
THEN COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(previous.fields) WHERE key=ANY($2::text[])),'{}'::jsonb)
    || EXCLUDED.fields
    || CASE WHEN jsonb_typeof(previous.fields->'attributes')='object' AND jsonb_typeof(EXCLUDED.fields->'attributes')='object'
       THEN jsonb_build_object('attributes',(previous.fields->'attributes') || (EXCLUDED.fields->'attributes'))
       ELSE '{}'::jsonb END
ELSE EXCLUDED.fields END`
		args = append(args, preserved)
	}
	// Exact upsert keeps observation history while avoiding delete/reinsert
	// journal noise for identities whose values and provenance are unchanged.
	if _, err := tx.Exec(ctx, `INSERT INTO ingest.torrents AS previous(provider_id,source_id,fields,raw_id,first_seen_at,last_seen_at,historical,origin)
SELECT provider_id,source_id,fields,raw_id,first_seen_at,last_seen_at,FALSE,origin
FROM ingest.staged_torrents WHERE run_id=$1 AND NOT deleted
ON CONFLICT(provider_id,source_id) DO UPDATE SET fields=`+fields+`,raw_id=EXCLUDED.raw_id,
first_seen_at=LEAST(previous.first_seen_at,EXCLUDED.first_seen_at),last_seen_at=EXCLUDED.last_seen_at,historical=FALSE,origin=EXCLUDED.origin`, args...); err != nil {
		return databaseError("publish staged catalogue", err)
	}
	if mode == model.ModeFull {
		if _, err := tx.Exec(ctx, `DELETE FROM ingest.torrents AS live WHERE provider_id=$1 AND NOT EXISTS
(SELECT 1 FROM ingest.staged_torrents AS staged WHERE staged.run_id=$2 AND staged.source_id=live.source_id AND NOT staged.deleted)`, run.ProviderID, run.ID); err != nil {
			return databaseError("remove missing catalogue records", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `DELETE FROM ingest.torrents AS live USING ingest.staged_torrents AS staged
WHERE staged.run_id=$1 AND staged.deleted AND live.provider_id=$2 AND live.source_id=staged.source_id`, run.ID, run.ProviderID); err != nil {
			return databaseError("publish catalogue deletions", err)
		}
	}
	if remote {
		if _, err := tx.Exec(ctx, `INSERT INTO ingest.remote_checkpoints(provider_id,endpoint,checkpoint)
VALUES($1,$2,$3) ON CONFLICT(provider_id) DO UPDATE SET endpoint=EXCLUDED.endpoint,checkpoint=EXCLUDED.checkpoint,last_synced_at=NOW()`, run.ProviderID, run.Config.URL, state.Checkpoint); err != nil {
			return databaseError("publish remote catalogue checkpoint", err)
		}
	}
	if !remote {
		if _, err := tx.Exec(ctx, "DELETE FROM ingest.remote_checkpoints WHERE provider_id=$1", run.ProviderID); err != nil {
			return databaseError("invalidate replaced catalogue checkpoint", err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.staged_torrents WHERE run_id=$1", run.ID); err != nil {
		return databaseError("release published staging", err)
	}
	return nil
}
