package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

var ErrCatalogAuthentication = errors.New("catalogue authentication required")
var ErrCatalogCursor = errors.New("invalid catalogue continuation")
var ErrCatalogExpired = errors.New("catalogue snapshot expired")

const shareColumns = "id,name,enabled,scope,source_ids,fields,revision,created_at,updated_at,last_access_at,last_sync_at,expires_at,requests_per_minute,max_concurrent_downloads,format,media_types,stream_preferences,torznab_preferences,catalog_entities"

func shareFormat(format string) string {
	if format == "" {
		return "catalog"
	}
	return format
}

// CatalogFields returns a fresh allowlist so callers cannot widen export policy.
func CatalogFields() []string {
	return []string{"title", "size", "info_hash", "published_at", "category", "categories"}
}

func catalogField(name string) bool {
	switch name {
	case "title", "size", "info_hash", "published_at", "category", "categories":
		return true
	default:
		return false
	}
}

func ValidateShare(input model.ShareInput) error {
	format := shareFormat(input.Format)
	if input.CatalogEntities.Availability && !slices.Contains(input.Fields, "info_hash") {
		return model.ErrInvalid
	}
	if format != "catalog" && !input.CatalogEntities.Empty() {
		return model.ErrInvalid
	}
	if format != "catalog" && format != "torznab" && format != "stremio" {
		return model.ErrInvalid
	}
	if format != "torznab" && len(input.MediaTypes) != 0 {
		return model.ErrInvalid
	}
	if format == "stremio" {
		if input.StreamPreferences == nil || !model.ValidStreamPreferences(*input.StreamPreferences) || len(input.StreamPreferences.Sources) > 0 {
			return model.ErrInvalid
		}
	} else if input.StreamPreferences != nil {
		return model.ErrInvalid
	}
	if input.TorznabPreferences != nil && (format != "torznab" || !input.TorznabPreferences.Valid()) {
		return model.ErrInvalid
	}
	types := map[string]bool{}
	for _, typ := range input.MediaTypes {
		if (typ != "movie" && typ != "tv" && typ != "other" && typ != "book" && typ != "ebook" && typ != "audiobook") || types[typ] {
			return model.ErrInvalid
		}
		types[typ] = true
	}
	if format != "catalog" && !slices.Equal(input.Fields, CatalogFields()) {
		return model.ErrInvalid
	}
	minimum := 1
	if format != "catalog" {
		minimum = 0
	}
	if (input.RequestsPerMinute != nil && (*input.RequestsPerMinute < minimum || *input.RequestsPerMinute > 3600)) ||
		(input.MaxConcurrentDownloads != nil && (*input.MaxConcurrentDownloads < minimum || *input.MaxConcurrentDownloads > 16)) ||
		(input.ExpiresAt != nil && (input.ExpiresAt.Year() < 1 || input.ExpiresAt.Year() > 9999)) {
		return model.ErrInvalid
	}
	if len(input.Name) < 1 || len(input.Name) > 120 || strings.TrimSpace(input.Name) != input.Name || !utf8.ValidString(input.Name) || len(input.Fields) < 1 || len(input.Fields) > 8 || len(input.SourceIDs) > 1000 {
		return model.ErrInvalid
	}
	for _, r := range input.Name {
		if unicode.IsControl(r) {
			return model.ErrInvalid
		}
	}
	if (input.Scope != "selected" && input.Scope != "all") || (input.Scope == "selected" && len(input.SourceIDs) == 0) || (input.Scope == "all" && len(input.SourceIDs) != 0) {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(input.SourceIDs))
	for _, id := range input.SourceIDs {
		if !activityIdentity.MatchString(id) || strings.Contains(id, ".") || seen[id] {
			return model.ErrInvalid
		}
		seen[id] = true
	}
	clear(seen)
	for _, field := range input.Fields {
		if !catalogField(field) || seen[field] {
			return model.ErrInvalid
		}
		seen[field] = true
	}
	return nil
}

func scanShare(row scanner) (model.Share, error) {
	var item model.Share
	var preferences, torznabPreferences, entities []byte
	err := row.Scan(&item.ID, &item.Name, &item.Enabled, &item.Scope, &item.SourceIDs, &item.Fields, &item.Revision, &item.CreatedAt, &item.UpdatedAt, &item.LastAccessAt, &item.LastSyncAt, &item.ExpiresAt, &item.RequestsPerMinute, &item.MaxConcurrentDownloads, &item.Format, &item.MediaTypes, &preferences, &torznabPreferences, &entities)
	if err == nil && item.Format == "stremio" {
		var p model.StreamPreferences
		err = json.Unmarshal(preferences, &p)
		item.StreamPreferences = &p
	}
	if err == nil && item.Format == "torznab" {
		var p model.TorznabPreferences
		err = json.Unmarshal(torznabPreferences, &p)
		item.TorznabPreferences = &p
	}
	if err == nil {
		err = json.Unmarshal(entities, &item.CatalogEntities)
	}
	return item, err
}

func sharingPassword(id string) (string, []byte, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", nil, errors.New("could not generate sharing password")
	}
	password := base64.RawURLEncoding.EncodeToString(value[:])
	hash := sharingPasswordHash(id, password)
	return password, hash[:], nil
}

func sharingPasswordHash(id, password string) [32]byte {
	return sha256.Sum256([]byte("scraper.catalog.password.v1\x00" + id + "\x00" + password))
}

func (s *Store) CatalogInstanceID(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, "SELECT instance_id FROM ingest.catalog_identity WHERE singleton").Scan(&id)
	return id, databaseError("read catalogue installation identity", err)
}

func (s *Store) Shares(ctx context.Context) ([]model.Share, error) {
	return s.SharesByFormat(ctx, "catalog")
}

func (s *Store) SharesByFormat(ctx context.Context, format string) ([]model.Share, error) {
	if format != "catalog" && format != "torznab" && format != "stremio" {
		return nil, model.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, "SELECT "+shareColumns+" FROM ingest.shares WHERE format=$1 ORDER BY created_at,id", format)
	if err != nil {
		return nil, databaseError("list shares", err)
	}
	defer rows.Close()
	items := []model.Share{}
	for rows.Next() {
		item, err := scanShare(rows)
		if err != nil {
			return nil, databaseError("read share", err)
		}
		items = append(items, item)
	}
	return items, databaseError("list shares", rows.Err())
}

func (s *Store) Share(ctx context.Context, id string) (model.Share, error) {
	item, err := scanShare(s.pool.QueryRow(ctx, "SELECT "+shareColumns+" FROM ingest.shares WHERE id=$1", id))
	return item, databaseError("read share", err)
}

func (s *Store) CreateShare(ctx context.Context, input model.ShareInput) (model.ShareCredential, error) {
	var result model.ShareCredential
	if err := ValidateShare(input); err != nil {
		return result, err
	}
	if input.Revision != 0 {
		return result, model.ErrInvalid
	}
	id, err := newRunID()
	if err != nil {
		return result, err
	}
	password, hash, err := sharingPassword(id)
	if err != nil {
		return result, err
	}
	if input.SourceIDs == nil {
		input.SourceIDs = []string{}
	}
	if input.MediaTypes == nil {
		input.MediaTypes = []string{}
	}
	requests, concurrent := 120, 2
	if shareFormat(input.Format) != "catalog" {
		requests, concurrent = 0, 0
	}
	if input.RequestsPerMinute != nil {
		requests = *input.RequestsPerMinute
	}
	if input.MaxConcurrentDownloads != nil {
		concurrent = *input.MaxConcurrentDownloads
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, databaseError("begin share creation", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return model.ShareCredential{}, err
	}
	result.Share, err = scanShare(tx.QueryRow(ctx, `INSERT INTO ingest.shares(id,name,enabled,scope,source_ids,fields,password_hash,expires_at,requests_per_minute,max_concurrent_downloads,format,media_types,stream_preferences,torznab_preferences,catalog_entities) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,COALESCE($14::jsonb,'{}'::jsonb),$15::jsonb) RETURNING `+shareColumns, id, input.Name, input.Enabled, input.Scope, input.SourceIDs, input.Fields, hash, input.ExpiresAt, requests, concurrent, shareFormat(input.Format), input.MediaTypes, sharePreferencesJSON(input), torznabPreferencesJSON(input), catalogEntitiesJSON(input.CatalogEntities)))
	if err != nil {
		return model.ShareCredential{}, databaseError("create share", err)
	}
	if err := seedCatalogEntities(ctx, tx, result.Share); err != nil {
		return model.ShareCredential{}, err
	}
	if err := recordAccessActivityTx(ctx, tx, shareActivity(input.Format, "created"), id); err != nil {
		return model.ShareCredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ShareCredential{}, databaseError("commit share creation", err)
	}
	result.Password = password
	return result, nil
}

func lockShareRevision(ctx context.Context, tx pgx.Tx, id string, expected int64) error {
	if expected < 1 {
		return model.ErrInvalid
	}
	// Policy triggers and source resets use publication before recipient rows.
	// Acquiring it before the revision lock avoids a cycle with snapshot cleanup.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return databaseError("lock share publication policy", err)
	}
	var revision int64
	if err := tx.QueryRow(ctx, "SELECT revision FROM ingest.shares WHERE id=$1 FOR UPDATE", id).Scan(&revision); err != nil {
		return databaseError("lock share", err)
	}
	if revision != expected {
		return model.ErrConflict
	}
	return nil
}

func (s *Store) UpdateShare(ctx context.Context, id string, input model.ShareInput) (model.Share, error) {
	if input.RequestsPerMinute == nil || input.MaxConcurrentDownloads == nil {
		return model.Share{}, model.ErrInvalid
	}
	if err := ValidateShare(input); err != nil {
		return model.Share{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Share{}, databaseError("begin share update", err)
	}
	defer rollback(tx)
	if err := lockShareRevision(ctx, tx, id, input.Revision); err != nil {
		return model.Share{}, err
	}
	if input.SourceIDs == nil {
		input.SourceIDs = []string{}
	}
	if input.MediaTypes == nil {
		input.MediaTypes = []string{}
	}
	item, err := scanShare(tx.QueryRow(ctx, `UPDATE ingest.shares SET name=$2,enabled=$3,scope=$4,source_ids=$5,fields=$6,expires_at=$7,requests_per_minute=$8,max_concurrent_downloads=$9,revision=revision+1,updated_at=NOW(),media_types=$11,stream_preferences=$12,torznab_preferences=COALESCE($13::jsonb,torznab_preferences),catalog_entities=$14::jsonb WHERE id=$1 AND format=$10 RETURNING `+shareColumns, id, input.Name, input.Enabled, input.Scope, input.SourceIDs, input.Fields, input.ExpiresAt, *input.RequestsPerMinute, *input.MaxConcurrentDownloads, shareFormat(input.Format), input.MediaTypes, sharePreferencesJSON(input), torznabPreferencesJSON(input), catalogEntitiesJSON(input.CatalogEntities)))
	if err != nil {
		return item, databaseError("update share", err)
	}
	if err := seedCatalogEntities(ctx, tx, item); err != nil {
		return model.Share{}, err
	}
	if err := recordAccessActivityTx(ctx, tx, shareActivity(input.Format, "permissions_changed"), id); err != nil {
		return model.Share{}, err
	}
	return item, databaseError("commit share update", tx.Commit(ctx))
}

func (s *Store) RotateShare(ctx context.Context, id string, revision int64) (model.ShareCredential, error) {
	var result model.ShareCredential
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, databaseError("begin sharing password rotation", err)
	}
	defer rollback(tx)
	if err := lockShareRevision(ctx, tx, id, revision); err != nil {
		return result, err
	}
	password, hash, err := sharingPassword(id)
	if err != nil {
		return result, err
	}
	result.Share, err = scanShare(tx.QueryRow(ctx, `UPDATE ingest.shares SET password_hash=$2,revision=revision+1,updated_at=NOW() WHERE id=$1 RETURNING `+shareColumns, id, hash))
	if err != nil {
		return model.ShareCredential{}, databaseError("rotate sharing password", err)
	}
	if err := recordAccessActivityTx(ctx, tx, shareActivity(result.Share.Format, "rotated"), id); err != nil {
		return model.ShareCredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ShareCredential{}, databaseError("commit sharing password rotation", err)
	}
	result.Password = password
	return result, nil
}

func (s *Store) DeleteShare(ctx context.Context, id string, revision int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin share revocation", err)
	}
	defer rollback(tx)
	if err := lockShareRevision(ctx, tx, id, revision); err != nil {
		return err
	}
	var format string
	if err := tx.QueryRow(ctx, "SELECT format FROM ingest.shares WHERE id=$1", id).Scan(&format); err != nil {
		return databaseError("read access format", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.shares WHERE id=$1", id); err != nil {
		return databaseError("revoke share", err)
	}
	if err := recordAccessActivityTx(ctx, tx, shareActivity(format, "revoked"), id); err != nil {
		return err
	}
	return databaseError("commit share revocation", tx.Commit(ctx))
}

func shareActivity(format, action string) string {
	if format == "torznab" || format == "stremio" {
		return format + "." + action
	}
	return "share." + action
}

func sharePreferencesJSON(input model.ShareInput) []byte {
	if input.StreamPreferences == nil {
		return []byte(`{}`)
	}
	preferences := *input.StreamPreferences
	if preferences.Mode == "" {
		preferences.Mode = "all"
	}
	data, _ := json.Marshal(preferences)
	return data
}

func torznabPreferencesJSON(input model.ShareInput) []byte {
	if input.TorznabPreferences == nil {
		return nil
	}
	data, _ := json.Marshal(input.TorznabPreferences)
	return data
}
