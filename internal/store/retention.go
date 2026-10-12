package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Native updates replace supplied attribute values but retain absent keys.
const nativeFieldMerge = `(previous.fields || EXCLUDED.fields)
|| CASE WHEN jsonb_typeof(EXCLUDED.fields->'attributes')='object' THEN
jsonb_build_object('attributes',
    CASE WHEN jsonb_typeof(previous.fields->'attributes')='object' THEN previous.fields->'attributes' ELSE '{}'::jsonb END
    || (EXCLUDED.fields->'attributes')) ELSE '{}'::jsonb END`

// SavePage persists compact observations, derived rows and the checkpoint in one
// transaction on the connection holding the provider lock. Payloads stay in memory.
func (s *Store) SavePage(ctx context.Context, expected model.Run, page model.Page, fingerprint string) (model.Run, error) {
	lease, err := s.ownedLease(expected.ID)
	if err != nil {
		return model.Run{}, err
	}
	defer lease.mu.Unlock()
	tx, err := lease.conn.Begin(ctx)
	if err != nil {
		return model.Run{}, databaseError("begin page retention", err)
	}
	defer rollback(tx)
	run, err := scanRun(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM ingest.runs WHERE id=$1 FOR UPDATE", expected.ID))
	if err != nil {
		return model.Run{}, databaseError("read page checkpoint", err)
	}
	remote := remoteCatalog(run.Config)
	if run.Status != model.StatusRunning || run.Pages != expected.Pages || run.Records != expected.Records || run.Errors != expected.Errors || !bytes.Equal(run.Cursor, expected.Cursor) {
		return model.Run{}, model.ErrConflict
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('ingest.publication_run_id',$1,true)", run.ID); err != nil {
		return model.Run{}, databaseError("set page publication context", err)
	}
	primaryCount := 0
	rejectedPrimary := false
	page.Items = slices.Clone(page.Items)
	for index := range page.Items {
		record := &page.Items[index]
		record.Fields = model.WithoutSwarmCounts(record.Fields)
		if !record.Auxiliary {
			primaryCount++
		}
	}
	var pageFingerprint any
	stalled := false
	if page.Error == "" && primaryCount > 0 && fingerprint != "" {
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ingest.pages WHERE run_id=$1 AND fingerprint=$2)", run.ID, fingerprint).Scan(&stalled); err != nil {
			return model.Run{}, databaseError("check page progress", err)
		}
	}
	if page.Error == "" && !stalled && (run.Mode == model.ModeFull || remote) && page.RequireUniqueIDs {
		seen := make(map[string]bool, primaryCount)
		ids := make([]string, 0, primaryCount)
		for _, record := range page.Items {
			if record.Auxiliary || record.Ignored || record.SourceID == "" {
				continue
			}
			if strings.IndexByte(record.SourceID, 0) >= 0 || !utf8.ValidString(record.SourceID) {
				continue
			}
			if seen[record.SourceID] {
				stalled = true
				break
			}
			seen[record.SourceID] = true
			ids = append(ids, record.SourceID)
		}
		// A requested reset defines a new traversal generation. Its prior
		// staging is irrelevant, but duplicate IDs in the new page still fail.
		if !stalled && !page.ResetStaging && len(ids) > 0 {
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ingest.staged_torrents WHERE run_id=$1 AND source_id=ANY($2::text[]))", run.ID, ids).Scan(&stalled); err != nil {
				return model.Run{}, databaseError("check staged identities", err)
			}
		}
	}
	if stalled {
		page.Error = model.ErrStalled.Error()
		if page.Metadata == nil {
			page.Metadata = make(map[string]any)
		}
		page.Metadata["failure_code"] = "stalled"
	}
	if page.Error == "" && primaryCount > 0 && fingerprint != "" {
		pageFingerprint = fingerprint
	}
	if len(page.RefreshScopes) > 0 {
		if page.Metadata == nil {
			page.Metadata = make(map[string]any)
		}
		page.Metadata["coverage_refresh_scopes"] = page.RefreshScopes
		page.Metadata["coverage_refresh_applied"] = page.Error == ""
	}
	metadata, err := jsonObject(page.Metadata)
	if err != nil {
		return model.Run{}, errors.New("cannot encode response metadata")
	}
	cursor, err := jsonCursor(run.Cursor)
	if err != nil {
		return model.Run{}, err
	}
	if page.Error == "" {
		cursor, err = jsonCursor(page.Next)
		if err != nil {
			return model.Run{}, err
		}
	}
	pageIndex := run.Pages + 1
	var pageID int64
	resetStaging := page.Error == "" && (run.Mode == model.ModeFull || remote) && page.ResetStaging
	if err := tx.QueryRow(ctx, `INSERT INTO ingest.pages(run_id,provider_id,page_index,body,content_type,fingerprint,position,metadata,error,done,reset_staging,require_unique_ids,payload_retained)
VALUES($1,$2,$3,''::bytea,$4,$5,$6,$7,$8,$9,$10,$11,FALSE) RETURNING id`, run.ID, run.ProviderID, pageIndex, page.ContentType, pageFingerprint, page.Position, metadata, page.Error, page.Error == "" && page.Done, resetStaging, page.RequireUniqueIDs).Scan(&pageID); err != nil {
		return model.Run{}, databaseError("save response observation", err)
	}
	errorCount := 0
	if page.Error != "" {
		errorCount++
	}
	latestCoverage := make(map[string][]string)
	recordIDs := make([]string, 0, primaryCount)
	// Metadata updates wait until every observation is representable, so a bad
	// interpretation cannot partially enrich a page whose cursor must be retried.
	var metadataUpdates []metadataUpdate
	type stagedUpdate struct {
		sourceID string
		fields   []byte
		origin   []byte
		rawID    *int64
		deleted  bool
	}
	var stagedUpdates []stagedUpdate
	for _, record := range page.Items {
		if !record.Auxiliary && record.SourceID == "" && record.Error == "" {
			record.Error = "Record has no stable source identity"
		}
		fields, err := jsonObject(record.Fields)
		if err != nil {
			// Invalid interpretation remains an error observation, without
			// persisting either the original payload or invalid fields.
			fields = []byte("{}")
			if record.Error == "" {
				record.Error = "Record fields cannot be encoded"
			}
		}
		contentType := record.ContentType
		if contentType == "" {
			contentType = page.ContentType
		}
		var rawID *int64
		const insertRaw = `INSERT INTO ingest.raw_records(run_id,provider_id,source_id,page_id,page_index,raw,content_type,fields,error,ignored,auxiliary,payload_retained)
VALUES($1,$2,$3,$4,$5,''::bytea,$6,
CASE WHEN $11 OR jsonb_typeof($7::jsonb)<>'object' THEN $7::jsonb ELSE '{}'::jsonb END,
$8,$9,$10,FALSE) RETURNING id`
		// Evaluate the JSONB interpretation in every mode, even when its fields
		// are not retained here. PostgreSQL can reject otherwise valid wire JSON
		// (for example U+0000 or an overflowing exponent).
		itemTx, err := tx.Begin(ctx)
		if err != nil {
			return model.Run{}, databaseError("begin record retention", err)
		}
		retainObservation := run.StorageMode != model.StorageCompact || run.Mode == model.ModePreview || record.Error != "" || page.Error != ""
		if retainObservation {
			err = itemTx.QueryRow(ctx, insertRaw, run.ID, run.ProviderID, record.SourceID, pageID, pageIndex, contentType, fields, record.Error, record.Ignored, record.Auxiliary, run.Mode == model.ModePreview).Scan(&rawID)
		} else {
			// Validate the same interpretation without writing millions of empty
			// success observations. Durable identity/scope/filter ledgers below,
			// not diagnostic rows, establish coverage and recovery boundaries.
			var kind string
			var sourceLength, typeLength int
			err = itemTx.QueryRow(ctx, `SELECT jsonb_typeof($1::jsonb),length($2::text),length($3::text)`, fields, record.SourceID, contentType).Scan(&kind, &sourceLength, &typeLength)
		}
		if err != nil {
			rollback(itemTx)
			var dataError *pgconn.PgError
			if !errors.As(err, &dataError) || (dataError.Code != "22P02" && dataError.Code != "22P05" && dataError.Code != "22003" && dataError.Code != "22021") {
				return model.Run{}, databaseError("save record observation", err)
			}
			if strings.IndexByte(record.SourceID, 0) >= 0 || !utf8.ValidString(record.SourceID) {
				record.SourceID = ""
			}
			if strings.IndexByte(contentType, 0) >= 0 || !utf8.ValidString(contentType) {
				contentType = ""
			}
			record.Error = "Record interpretation cannot be represented in database fields"
			fields = []byte("{}")
			if err := tx.QueryRow(ctx, insertRaw, run.ID, run.ProviderID, record.SourceID, pageID, pageIndex, contentType, fields, record.Error, record.Ignored, record.Auxiliary, run.Mode == model.ModePreview).Scan(&rawID); err != nil {
				return model.Run{}, databaseError("retain uninterpretable record", err)
			}
		} else if err := itemTx.Commit(ctx); err != nil {
			return model.Run{}, databaseError("finish record retention", err)
		}
		if record.Error != "" && !record.Auxiliary {
			rejectedPrimary = true
			errorCount++
		}
		if page.Error != "" || record.Error != "" || record.Auxiliary || record.SourceID == "" {
			continue
		}
		if record.Ignored {
			// A bounded adapter can explicitly attest that a valid identity
			// now matches no selected scope. Ordinary ignored observations
			// (unspecified or nonempty coverage) remain non-authoritative.
			if record.CoverageScopes != nil && len(record.CoverageScopes) == 0 {
				latestCoverage[record.SourceID] = record.CoverageScopes
			}
			continue
		}
		recordIDs = append(recordIDs, record.SourceID)
		if record.CoverageScopes != nil {
			latestCoverage[record.SourceID] = record.CoverageScopes
		}
		if run.Mode == model.ModePreview {
			continue
		}
		record.Fields = model.StoredTorrentFields(record.Fields, run.StorageMode)
		if remote {
			fields, err = jsonObject(record.Fields)
			if err != nil {
				return model.Run{}, err
			}
			origin, err := json.Marshal(record.Origin)
			if err != nil || record.Origin == nil || record.SourceID != model.CatalogItemID(*record.Origin) {
				return model.Run{}, errors.New("invalid retained catalogue identity")
			}
			stagedUpdates = append(stagedUpdates, stagedUpdate{sourceID: record.SourceID, fields: fields, origin: origin, rawID: rawID, deleted: record.Deleted})
			continue
		}
		canonical, err := canonicalFields(record.Fields)
		if err != nil {
			return model.Run{}, err
		}
		switch run.Mode {
		case model.ModeIncremental:
			_, err = tx.Exec(ctx, `INSERT INTO ingest.torrents AS previous(provider_id,source_id,fields,raw_id)
VALUES($1,$2,$3,$4) ON CONFLICT(provider_id,source_id) DO UPDATE SET
fields=CASE WHEN $5 AND ingest.torrent_hash(previous.fields->'info_hash') IS DISTINCT FROM ingest.torrent_hash(EXCLUDED.fields->'info_hash')
THEN EXCLUDED.fields ELSE `+nativeFieldMerge+` END,raw_id=EXCLUDED.raw_id,last_seen_at=NOW(),historical=FALSE,origin=NULL`, run.ProviderID, record.SourceID, canonical, rawID, run.Config.SupportsMetadata())
		case model.ModeFull:
			stagedUpdates = append(stagedUpdates, stagedUpdate{sourceID: record.SourceID, fields: canonical, rawID: rawID})
		case model.ModeMetadata:
			metadataUpdates = append(metadataUpdates, metadataUpdate{sourceID: record.SourceID, fields: canonical, rawID: rawID})
		default:
			return model.Run{}, model.ErrInvalid
		}
		if err != nil {
			return model.Run{}, databaseError("retain canonical record", err)
		}
	}
	pages := 0
	if page.Error == "" && primaryCount > 0 {
		pages = 1
	}
	if (remote || run.Mode == model.ModeFull || run.Mode == model.ModeMetadata) && page.Error == "" && errorCount > 0 {
		// Keep error observations without certifying an incomplete page.
		cursor, err = jsonCursor(run.Cursor)
		if err != nil {
			return model.Run{}, err
		}
		page.Error = "Remote catalogue records cannot be retained faithfully"
		if run.Mode == model.ModeMetadata {
			page.Error = "Metadata records cannot be represented in database fields"
		} else if !remote {
			page.Error = "Full collection records cannot be retained faithfully"
		}
		page.Done = false
		pages = 0
		if _, err := tx.Exec(ctx, "UPDATE ingest.pages SET error=$2,done=FALSE,fingerprint=NULL,reset_staging=FALSE,metadata=jsonb_set(metadata,'{coverage_refresh_applied}','false'::jsonb,FALSE) WHERE id=$1", pageID, page.Error); err != nil {
			return model.Run{}, databaseError("mark incomplete catalogue page", err)
		}
	}
	if page.Error == "" {
		if resetStaging {
			if _, err := tx.Exec(ctx, "DELETE FROM ingest.staged_torrents WHERE run_id=$1", run.ID); err != nil {
				return model.Run{}, databaseError("reset collection staging", err)
			}
		}
		if err := refreshCoverage(ctx, tx, run.ID, page.RefreshScopes, resetStaging); err != nil {
			return model.Run{}, err
		}
		if len(page.RefreshScopes) > 0 {
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM ingest.run_record_identities WHERE run_id=$1", run.ID).Scan(&run.DistinctRecords); err != nil {
				return model.Run{}, databaseError("count refreshed identities", err)
			}
		}
		for _, update := range stagedUpdates {
			if remote {
				_, err = tx.Exec(ctx, `INSERT INTO ingest.staged_torrents(run_id,provider_id,source_id,fields,raw_id,origin,deleted)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(run_id,source_id) DO UPDATE SET
fields=EXCLUDED.fields,raw_id=EXCLUDED.raw_id,origin=EXCLUDED.origin,deleted=EXCLUDED.deleted,last_seen_at=NOW()`,
					run.ID, run.ProviderID, update.sourceID, update.fields, update.rawID, update.origin, update.deleted)
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO ingest.staged_torrents AS previous(run_id,provider_id,source_id,fields,raw_id)
VALUES($1,$2,$3,$4,$5) ON CONFLICT(run_id,source_id) DO UPDATE SET
fields=CASE WHEN ingest.torrent_hash(previous.fields->'info_hash') IS DISTINCT FROM ingest.torrent_hash(EXCLUDED.fields->'info_hash')
THEN EXCLUDED.fields ELSE `+nativeFieldMerge+` END,raw_id=EXCLUDED.raw_id,last_seen_at=NOW()`, run.ID, run.ProviderID, update.sourceID, update.fields, update.rawID)
			}
			if err != nil {
				return model.Run{}, databaseError("stage canonical record", err)
			}
		}
	}
	if page.Error == "" {
		for _, update := range metadataUpdates {
			if err := saveMetadata(ctx, tx, run, update); err != nil {
				return model.Run{}, err
			}
		}
	}
	if page.Error == "" {
		if resetStaging {
			if _, err := tx.Exec(ctx, "DELETE FROM ingest.run_record_identities WHERE run_id=$1", run.ID); err != nil {
				return model.Run{}, databaseError("reset collection identities", err)
			}
			run.DistinctRecords = 0
		}
		if len(recordIDs) > 0 {
			var added int64
			if err := tx.QueryRow(ctx, `WITH inserted AS (
    INSERT INTO ingest.run_record_identities(run_id,source_id)
    SELECT $1,source_id FROM unnest($2::text[]) AS observed(source_id)
    ON CONFLICT (run_id,source_id) DO NOTHING
    RETURNING 1
)
SELECT count(*) FROM inserted`, run.ID, recordIDs).Scan(&added); err != nil {
				return model.Run{}, databaseError("retain collection identities", err)
			}
			run.DistinctRecords += added
		}
		if err := saveCoverage(ctx, tx, run.ID, latestCoverage); err != nil {
			return model.Run{}, err
		}
		var outside []string
		for id, scopes := range latestCoverage {
			if len(scopes) == 0 {
				outside = append(outside, id)
			}
		}
		if len(outside) > 0 {
			if _, err := tx.Exec(ctx, "DELETE FROM ingest.staged_torrents WHERE run_id=$1 AND source_id=ANY($2::text[])", run.ID, outside); err != nil {
				return model.Run{}, databaseError("retire out-of-scope staging", err)
			}
			var retired int64
			if err := tx.QueryRow(ctx, `WITH removed AS (
DELETE FROM ingest.run_record_identities WHERE run_id=$1 AND source_id=ANY($2::text[]) RETURNING 1
)
SELECT count(*) FROM removed`, run.ID, outside).Scan(&retired); err != nil {
				return model.Run{}, databaseError("retire out-of-scope identities", err)
			}
			run.DistinctRecords -= retired
		}
	}
	// This ledger is append-only within a traversal generation. Coverage refresh
	// may retire derived rows, but must never make an old native ID look new.
	policy := run.EffectivePolicy()
	auxiliaryResponse, _ := page.Metadata["auxiliary_response"].(bool)
	progressResponse := page.Body != nil && !auxiliaryResponse && !(primaryCount == 0 && len(page.Items) > 0)
	if policy.NoProgressRequests > 0 && page.Error == "" {
		if resetStaging {
			if _, err := tx.Exec(ctx, "DELETE FROM ingest.run_progress_identities WHERE run_id=$1", run.ID); err != nil {
				return model.Run{}, databaseError("reset useful-progress generation", err)
			}
			run.RequestsWithoutNewIDs, run.ProgressWarningSent = 0, false
		}
		var added int64
		if len(recordIDs) > 0 {
			if err := tx.QueryRow(ctx, `WITH inserted AS (
INSERT INTO ingest.run_progress_identities(run_id,source_id)
SELECT $1,source_id FROM unnest($2::text[]) AS observed(source_id)
ON CONFLICT (run_id,source_id) DO NOTHING RETURNING 1)
SELECT count(*) FROM inserted`, run.ID, recordIDs).Scan(&added); err != nil {
				return model.Run{}, databaseError("retain useful-progress identities", err)
			}
		}
		if added > 0 {
			run.RequestsWithoutNewIDs, run.ProgressWarningSent = 0, false
		} else if progressResponse {
			run.RequestsWithoutNewIDs++
		}
		if progressResponse && run.RequestsWithoutNewIDs >= int64(policy.NoProgressRequests) {
			if !run.ProgressWarningSent {
				if err := lifecycleEvent(ctx, tx, run.ID, "no_progress", "Collection has reached its useful-progress threshold without a new source identity"); err != nil {
					return model.Run{}, databaseError("record useful-progress warning", err)
				}
				run.ProgressWarningSent = true
			}
			if policy.NoProgressAction == "pause" && !page.Done && !run.PauseRequested && !run.CancelRequested {
				run.PauseReason = model.PauseNoProgress
			}
		}
	}
	if !remote && run.Mode == model.ModeIncremental && primaryCount > 0 && page.Error == "" {
		if _, err := tx.Exec(ctx, "DELETE FROM ingest.remote_checkpoints WHERE provider_id=$1", run.ProviderID); err != nil {
			return model.Run{}, databaseError("invalidate replaced catalogue checkpoint", err)
		}
	}
	if page.Error == "" && page.KnownPageStreak != nil {
		if *page.KnownPageStreak < 0 {
			return model.Run{}, model.ErrInvalid
		}
		run.KnownPageStreak = *page.KnownPageStreak
	}
	// Errored attempts contribute to observation/error counters but cannot
	// change traversal state or its completion evidence.
	if page.Error == "" && page.Metadata["traversal_phase"] == "options_list" && len(recordIDs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO ingest.run_filter_records(page_id,source_id)
SELECT $1,source_id FROM unnest($2::text[]) AS accepted(source_id) ON CONFLICT DO NOTHING`, pageID, recordIDs); err != nil {
			return model.Run{}, databaseError("retain filter coverage evidence", err)
		}
	}
	run, err = scanRun(tx.QueryRow(ctx, `UPDATE ingest.runs SET pages=pages+$2,records=records+$3,errors=errors+$4,
cursor=$5,traversal_done=CASE WHEN $6 THEN $7 ELSE traversal_done END,distinct_records=$8,
requests_without_new_ids=$9,progress_warning_sent=$10,pause_reason=$11,known_page_streak=$12,rejected_primary=rejected_primary OR $13
WHERE id=$1 RETURNING `+runColumns, run.ID, pages, primaryCount, errorCount, cursor, page.Error == "", page.Done, run.DistinctRecords, run.RequestsWithoutNewIDs, run.ProgressWarningSent, run.PauseReason, run.KnownPageStreak, page.Error == "" && rejectedPrimary))
	if err != nil {
		return model.Run{}, databaseError("save collection checkpoint", err)
	}
	eventKind, eventMessage := "page_saved", "Response observations and checkpoint saved"
	if page.Error != "" {
		eventKind, eventMessage = "page_error", "Response error recorded; checkpoint unchanged"
	}
	eventValues := map[string]any{
		"page_id": pageID, "page": pageIndex, "records": primaryCount,
		"auxiliary_records": len(page.Items) - primaryCount, "errors": errorCount,
		"done": page.Error == "" && page.Done, "emitted": pages > 0,
		"payload_retained": false,
	}
	if page.Error == "" && run.Config.Traversal != nil && run.Config.Traversal.TotalMode == "at_least" {
		if scope, ok := page.Metadata["coverage_scope"].(string); ok {
			eventMessage = "Complete source category checked; collection target updated"
			eventValues["coverage_scope"] = scope
			for _, key := range []string{"coverage_advertised_total", "coverage_previous_target", "coverage_target"} {
				eventValues[key] = page.Metadata[key]
			}
		}
	}
	if page.Error != "" {
		code, _ := page.Metadata["failure_code"].(string)
		switch code {
		case "authentication", "certificate", "network", "timeout", "http", "parse", "stalled", "configuration":
		default:
			code = "unknown"
		}
		eventValues["failure_code"] = code
		// Event page is the observation index, not the source's logical page.
		// Keep it stable while carrying safe source pagination diagnostics.
		connectors.CopyFailureDiagnostics(eventValues, page.Metadata)
		eventValues["page"] = pageIndex
		if reason, ok := eventValues["failure_reason"].(string); ok {
			eventMessage = connectors.FailureReasonMessage(reason) + "; checkpoint unchanged"
		}
	}
	eventData, err := jsonObject(eventValues)
	if err != nil {
		return model.Run{}, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO ingest.events(run_id,kind,message,data) VALUES($1,$2,$3,$4)", run.ID, eventKind, eventMessage, eventData); err != nil {
		return model.Run{}, databaseError("record retained page event", err)
	}
	if err := notify(ctx, tx); err != nil {
		return model.Run{}, databaseError("notify retained page", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Run{}, databaseError("commit retained page", err)
	}
	if stalled {
		return run, model.ErrStalled
	}
	return run, nil
}

// Known requires acceptance by a collection completed before this run began,
// without skipped primary records. Recovered request errors do not disqualify
// success, but a partially published failure cannot create a boundary.
func (s *Store) Known(ctx context.Context, providerID string, ids []string, before time.Time) (map[string]bool, error) {
	known := make(map[string]bool)
	if len(ids) == 0 {
		return known, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT record.source_id FROM ingest.torrents AS record
WHERE record.provider_id=$1 AND record.source_id=ANY($2::text[]) AND record.first_seen_at<$3
AND record.collection_verified_at>=record.first_seen_at AND record.collection_verified_at<$3`, providerID, ids, before)
	if err != nil {
		return nil, databaseError("read known identities", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, databaseError("read known identity", err)
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read known identities", err)
	}
	return known, nil
}
