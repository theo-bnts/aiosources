package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

// MetadataCandidates walks published native identities, not listing pages or
// hash groups. A committed keyset cursor remains useful even for skipped rows
// whose metadata stays absent. Empty arrays/objects and false are known values.
func (s *Store) MetadataCandidates(ctx context.Context, run model.Run, after string, limit int) ([]model.MetadataCandidate, error) {
	if run.Mode != model.ModeMetadata || !run.Config.SupportsMetadata() || run.ProviderID != run.Config.ID || run.CreatedAt.IsZero() || limit < 1 || limit > 100 {
		return nil, model.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT source_id,COALESCE(search_info_hash,'') FROM ingest.torrents AS record
WHERE provider_id=$1 AND origin IS NULL AND first_seen_at<=$2 AND source_id>$3
AND EXISTS (SELECT 1 FROM unnest($4::text[]) AS required(name)
    WHERE COALESCE(record.fields->required.name,'null'::jsonb)='null'::jsonb)
AND ($6='' OR EXISTS (
    SELECT 1 FROM ingest.run_record_identities AS accepted
    JOIN ingest.runs AS parent ON parent.id=accepted.run_id
    WHERE accepted.run_id=$6 AND accepted.source_id=record.source_id
    AND parent.provider_id=record.provider_id AND record.first_seen_at>=parent.created_at))
ORDER BY source_id LIMIT $5`, run.ProviderID, run.CreatedAt, after, run.Config.MetadataFields(), limit, run.MetadataParentRunID)
	if err != nil {
		return nil, databaseError("read metadata candidates", err)
	}
	defer rows.Close()
	candidates := make([]model.MetadataCandidate, 0, limit)
	for rows.Next() {
		var candidate model.MetadataCandidate
		if err := rows.Scan(&candidate.SourceID, &candidate.InfoHash); err != nil {
			return nil, databaseError("read metadata candidate", err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, databaseError("read metadata candidates", rows.Err())
}

// The caller has completed the Incremental or its initial Full under queue and row
// locks. Copy the stored snapshot, not current defaults, and commit the child
// and both lifecycle events atomically with the successful parent.
func queueMetadataAfterDiscovery(ctx context.Context, tx pgx.Tx, parent model.Run) error {
	id, err := newRunID()
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO ingest.runs
(id,provider_id,provider_name,mode,status,max_pages,revision,config,policy,trigger,metadata_parent_run_id)
SELECT $1,parent.provider_id,parent.provider_name,'metadata','queued',parent.max_pages,
parent.revision,parent.config,parent.policy,parent.trigger,parent.id
FROM ingest.runs AS parent WHERE parent.id=$2 AND EXISTS (
    SELECT 1 FROM ingest.run_record_identities AS accepted
    JOIN ingest.torrents AS record ON record.provider_id=parent.provider_id AND record.source_id=accepted.source_id
    WHERE accepted.run_id=parent.id AND record.origin IS NULL
    AND record.first_seen_at>=parent.created_at AND record.first_seen_at<=NOW()
    AND EXISTS (SELECT 1 FROM unnest($3::text[]) AS required(name)
        WHERE COALESCE(record.fields->required.name,'null'::jsonb)='null'::jsonb))
ON CONFLICT (metadata_parent_run_id) DO NOTHING RETURNING id`, id, parent.ID, parent.Config.MetadataFields()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return databaseError("queue discovery metadata", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO ingest.events(run_id,kind,message,data) VALUES
($1,'metadata_queued','Metadata queued for new torrents',jsonb_build_object('metadata_run_id',$2::text)),
($2,'queued','Metadata queued automatically for new torrents from successful discovery',jsonb_build_object('metadata_parent_run_id',$1::text))`, parent.ID, id)
	return databaseError("record discovery metadata events", err)
}

type metadataUpdate struct {
	sourceID string
	fields   []byte
	rawID    int64
}

// A detail response can only supplement the same published native identity and
// hash. It cannot create catalogue membership, replace known values, or change
// first_seen_at. Missing attribute keys are merged independently of other fields.
func saveMetadata(ctx context.Context, tx pgx.Tx, run model.Run, update metadataUpdate) error {
	_, err := tx.Exec(ctx, `UPDATE ingest.torrents AS previous SET
fields=previous.fields
    || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each($3::jsonb)
        WHERE COALESCE(previous.fields->key,'null'::jsonb)='null'::jsonb),'{}'::jsonb)
    || CASE WHEN jsonb_typeof(previous.fields->'attributes')='object' AND jsonb_typeof($3::jsonb->'attributes')='object'
       THEN jsonb_build_object('attributes',previous.fields->'attributes'
           || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each($3::jsonb->'attributes')
               WHERE COALESCE(previous.fields->'attributes'->key,'null'::jsonb)='null'::jsonb),'{}'::jsonb))
       ELSE '{}'::jsonb END,
raw_id=$4,last_seen_at=NOW(),historical=FALSE
WHERE provider_id=$1 AND source_id=$2 AND origin IS NULL AND first_seen_at<=$5
AND search_info_hash=ingest.torrent_hash($3::jsonb->'info_hash')`, run.ProviderID, update.sourceID, update.fields, update.rawID, run.CreatedAt)
	return databaseError("fill missing native metadata", err)
}
