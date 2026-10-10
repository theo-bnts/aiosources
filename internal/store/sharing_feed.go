package store

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

// CatalogPageRequest is read-only traversal state; neither fields nor providers
// are caller-selectable. Zero Limit means the initial default or a cursor's size.
type CatalogPageRequest struct {
	Limit      int
	Cursor     string
	Checkpoint string
	Head       bool
}

type catalogToken struct {
	Version    int    `json:"v"`
	InstanceID string `json:"instance"`
	ShareID    string `json:"share"`
	Revision   int64  `json:"revision"`
	Kind       string `json:"kind"`
	Mode       string `json:"mode,omitempty"`
	Low        int64  `json:"low,omitempty"`
	High       int64  `json:"high"`
	Limit      int    `json:"limit,omitempty"`
	After      int64  `json:"after,omitempty"`
}

func signCatalogToken(key []byte, value catalogToken) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("scraper.catalog.continuation.v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func parseCatalogToken(key []byte, raw string) (catalogToken, error) {
	var value catalogToken
	if len(raw) > 8192 {
		return value, ErrCatalogCursor
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return value, ErrCatalogCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return value, ErrCatalogCursor
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return value, ErrCatalogCursor
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("scraper.catalog.continuation.v1\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return value, ErrCatalogCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Version != 1 || value.Revision < 1 || value.High < 0 || value.Low < 0 || value.Low > value.High {
		return catalogToken{}, ErrCatalogCursor
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return catalogToken{}, ErrCatalogCursor
	}
	return value, nil
}

func catalogScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number:
		return true
	default:
		return false
	}
}

func projectCatalogFields(raw []byte, allowed []string) (map[string]any, error) {
	var source map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return nil, errors.New("could not decode published catalogue fields")
	}
	result := make(map[string]any, len(allowed))
	for _, name := range allowed {
		if !catalogField(name) {
			continue
		}
		value, found := source[name]
		if !found {
			continue
		}
		if catalogScalar(value) {
			result[name] = value
			continue
		}
		if values, ok := value.([]any); ok {
			safe := true
			for _, member := range values {
				if !catalogScalar(member) {
					safe = false
					break
				}
			}
			if safe {
				result[name] = value
			}
		}
	}
	return result, nil
}

// Latest local copies are resolved at the immutable high-water mark. Picking a
// live authorized copy before a tombstone prevents deleting an origin that is
// still present through another permitted provider. The delta includes each
// touched original identity exactly once, including an old identity on change.
const catalogPageSQL = `
WITH states AS (
    SELECT provider_id,source_id,origin,fields,deleted,sequence FROM ingest.catalog_journal
    WHERE sequence<=$1 AND ($2 OR provider_id=ANY($3::text[]))
    UNION ALL
    SELECT provider_id,source_id,origin,fields,deleted,sequence FROM ingest.catalog_baselines
    WHERE sequence<=$1 AND ($2 OR provider_id=ANY($3::text[]))
), copies AS (
    SELECT DISTINCT ON (provider_id,source_id,origin) origin,fields,deleted,sequence
    FROM states
    ORDER BY provider_id,source_id,origin,sequence DESC
), chosen AS (
    SELECT DISTINCT ON (origin) origin,fields,deleted,sequence
    FROM copies ORDER BY origin,deleted ASC,sequence DESC
)
SELECT origin,fields,deleted,sequence FROM chosen
WHERE (($4='full' AND NOT deleted) OR ($4='incremental' AND origin IN (
    SELECT origin FROM ingest.catalog_journal WHERE sequence>$5 AND sequence<=$1 AND ($2 OR provider_id=ANY($3::text[]))
)))
AND sequence > $6
ORDER BY sequence
LIMIT $7`

func (s *Store) CatalogPage(ctx context.Context, shareID, password string, request CatalogPageRequest) (model.CatalogEnvelope, error) {
	download, err := s.BeginCatalogDownload(ctx, shareID, password)
	if err != nil {
		return model.CatalogEnvelope{}, err
	}
	defer download.Close()
	return download.Page(ctx, request)
}

func (d *CatalogDownload) Page(ctx context.Context, request CatalogPageRequest) (model.CatalogEnvelope, error) {
	ctx, cancel := context.WithDeadline(ctx, d.deadline)
	defer cancel()
	result := model.CatalogEnvelope{Version: 1, Items: []model.CatalogItem{}}
	shareID := d.shareID
	if request.Limit < 0 || request.Limit > 1000 || (request.Cursor != "" && request.Checkpoint != "") {
		return result, ErrCatalogCursor
	}
	tx, err := d.store.pool.Begin(ctx)
	if err != nil {
		return result, databaseError("begin catalogue export", err)
	}
	defer rollback(tx)
	var share model.Share
	// Recheck current policy for every page, including authenticated old cursors.
	// Serialize permission changes with the export, never with network writes.
	err = tx.QueryRow(ctx, `SELECT s.enabled,s.scope,s.source_ids,s.fields,s.revision FROM ingest.shares s JOIN ingest.share_requests r ON r.share_id=s.id WHERE s.id=$1 AND s.enabled AND s.revision=$2 AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) AND r.id=$3 AND r.lease_until>clock_timestamp() FOR UPDATE OF s`, shareID, d.revision, d.id).Scan(&share.Enabled, &share.Scope, &share.SourceIDs, &share.Fields, &share.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrCatalogAuthentication
	}
	if err != nil {
		return result, databaseError("authorize catalogue export", err)
	}
	var signingKey []byte
	if err := tx.QueryRow(ctx, `SELECT instance_id,signing_key FROM ingest.catalog_identity WHERE singleton`).Scan(&result.InstanceID, &signingKey); err != nil {
		return result, databaseError("read catalogue identity", err)
	}
	var floor int64
	if err := tx.QueryRow(ctx, "SELECT floor FROM ingest.catalog_retention WHERE singleton FOR SHARE").Scan(&floor); err != nil {
		return result, databaseError("read retained catalog boundary", err)
	}
	state := catalogToken{Version: 1, InstanceID: result.InstanceID, ShareID: shareID, Revision: share.Revision, Kind: "page", Mode: "full", Limit: request.Limit}
	if state.Limit == 0 {
		state.Limit = 100
	}
	if request.Cursor != "" {
		state, err = parseCatalogToken(signingKey, request.Cursor)
		if err != nil || state.Kind != "page" || state.InstanceID != result.InstanceID || state.ShareID != shareID || state.Revision != share.Revision || (state.Mode != "full" && state.Mode != "incremental") || state.Limit < 1 || state.Limit > 1000 || (request.Limit != 0 && request.Limit != state.Limit) || (state.Mode == "full" && state.Low != 0) || state.After < 1 || state.After > state.High {
			return result, ErrCatalogCursor
		}
	} else {
		if request.Checkpoint != "" {
			checkpoint, parseErr := parseCatalogToken(signingKey, request.Checkpoint)
			if parseErr != nil || checkpoint.Kind != "checkpoint" || checkpoint.InstanceID != result.InstanceID || checkpoint.ShareID != shareID || checkpoint.Revision > share.Revision || checkpoint.Low != 0 || checkpoint.Mode != "" || checkpoint.Limit != 0 || checkpoint.After != 0 {
				return result, ErrCatalogCursor
			}
			// An authentic older scope is authoritative-full, never an incremental
			// continuation that might retain or reveal previously allowed sources.
			if checkpoint.Revision == share.Revision {
				state.Mode = "incremental"
				state.Low = checkpoint.High
			}
		}
		if err := tx.QueryRow(ctx, "SELECT GREATEST(COALESCE(MAX(sequence),0),$1::bigint) FROM ingest.catalog_journal", floor).Scan(&state.High); err != nil {
			return result, databaseError("capture catalogue publication boundary", err)
		}
		if state.Low > state.High {
			return result, ErrCatalogCursor
		}
	}
	if request.Cursor != "" && state.High < floor {
		return result, ErrCatalogExpired
	}
	if state.Mode == "incremental" && state.Low < floor {
		if request.Cursor != "" {
			return result, ErrCatalogExpired
		}
		state.Mode, state.Low = "full", 0
	}
	result.Mode = state.Mode
	rows, err := tx.Query(ctx, catalogPageSQL, state.High, share.Scope == "all", share.SourceIDs, state.Mode, state.Low, state.After, state.Limit+1)
	if err != nil {
		return result, databaseError("read catalogue page", err)
	}
	more := false
	for rows.Next() {
		if len(result.Items) == state.Limit {
			more = true
			break
		}
		var item model.CatalogItem
		var origin, fields []byte
		if err := rows.Scan(&origin, &fields, &item.Deleted, &state.After); err != nil {
			rows.Close()
			return result, databaseError("read catalogue item", err)
		}
		if err := json.Unmarshal(origin, &item.Origin); err != nil {
			rows.Close()
			return result, errors.New("could not decode catalogue provenance")
		}
		item.ID = model.CatalogItemID(item.Origin)
		if !item.Deleted {
			item.Fields, err = projectCatalogFields(fields, share.Fields)
			if err != nil {
				rows.Close()
				return result, err
			}
		}
		result.Items = append(result.Items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, databaseError("read catalogue page", err)
	}
	if more {
		result.NextCursor, err = signCatalogToken(signingKey, state)
	} else {
		result.Checkpoint, err = signCatalogToken(signingKey, catalogToken{Version: 1, InstanceID: result.InstanceID, ShareID: shareID, Revision: share.Revision, Kind: "checkpoint", High: state.High})
	}
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `UPDATE ingest.shares SET last_access_at=clock_timestamp(),last_sync_at=CASE WHEN $2 THEN clock_timestamp() ELSE last_sync_at END WHERE id=$1`, shareID, !more && !request.Head); err != nil {
		return result, databaseError("record catalogue access", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, databaseError("finish catalogue export", err)
	}
	if err := d.Validate(ctx); err != nil {
		return model.CatalogEnvelope{}, err
	}
	return result, nil
}
