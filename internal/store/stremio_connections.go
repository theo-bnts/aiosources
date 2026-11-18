package store

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// StremioConnection returns settings only after verifying the independent key.
// Reading it creates no admission or visitor record and never retains the key.
func (s *Store) StremioConnection(ctx context.Context, id, key string) (model.Share, error) {
	var zero model.Share
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(key)
	if err != nil || len(decoded) != 32 || len(key) != 43 || len(id) != 36 {
		return zero, ErrCatalogAuthentication
	}
	var hash []byte
	var unexpired bool
	value, err := scanShare(s.pool.QueryRow(ctx, "SELECT "+shareColumns+`,password_hash,expires_at IS NULL OR expires_at>clock_timestamp() FROM aiosources.shares WHERE id=$1 AND format='stremio'`, id), &hash, &unexpired)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrCatalogAuthentication
	}
	if err != nil {
		return zero, databaseError("verify Stremio access", err)
	}
	candidate := sharingPasswordHash(id, key)
	if !value.Enabled || !unexpired || subtle.ConstantTimeCompare(hash, candidate[:]) != 1 {
		return zero, ErrCatalogAuthentication
	}
	return value, nil
}

func (s *Store) BeginStremioRequest(ctx context.Context, id, key string) (*CatalogDownload, error) {
	return s.beginShareRequest(ctx, id, key, "stremio")
}
