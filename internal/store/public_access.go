package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

var sharedHashRE = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (s *Store) PublicAccess(ctx context.Context) (model.PublicAccess, error) {
	var out model.PublicAccess
	err := s.pool.QueryRow(ctx, `SELECT revision,enabled,generation,torznab_enabled,torznab_generation,search_enabled,stremio_enabled,stremio_generation,catalog_enabled,catalog_generation,use_instance_debrid,use_instance_announces FROM aiosources.aio_public_access WHERE singleton`).Scan(&out.Revision, &out.Enabled, &out.Generation, &out.TorznabEnabled, &out.TorznabGeneration, &out.SearchEnabled, &out.StremioEnabled, &out.StremioGeneration, &out.CatalogEnabled, &out.CatalogGeneration, &out.UseInstanceDebrid, &out.UseInstanceAnnounces)
	return out, databaseError("read public access", err)
}
func (s *Store) SetPublicAccess(ctx context.Context, value model.PublicAccess) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin public access change", err)
	}
	defer rollback(tx)
	// Match named catalog policy changes: publication and metadata fences precede
	// the policy row, including when enabling or revoking public access.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return databaseError("lock public catalog publication policy", err)
	}
	var previous, previousTorznab, previousSearch, previousStremio, previousCatalog, previousDebrid, previousAnnounces bool
	err = tx.QueryRow(ctx, `SELECT enabled,torznab_enabled,search_enabled,stremio_enabled,catalog_enabled,use_instance_debrid,use_instance_announces FROM aiosources.aio_public_access WHERE singleton AND revision=$1 FOR UPDATE`, value.Revision).Scan(&previous, &previousTorznab, &previousSearch, &previousStremio, &previousCatalog, &previousDebrid, &previousAnnounces)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrConflict
	}
	if err != nil {
		return databaseError("lock public access", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE aiosources.aio_public_access SET revision=revision+1,enabled=$1,
 generation=generation+CASE WHEN enabled<>$1 OR use_instance_debrid<>$7 OR use_instance_announces<>$8 THEN 1 ELSE 0 END,
 torznab_enabled=$3,torznab_generation=torznab_generation+CASE WHEN torznab_enabled<>$3 THEN 1 ELSE 0 END,
 search_enabled=$4,stremio_enabled=$5,stremio_generation=stremio_generation+CASE WHEN stremio_enabled<>$5 THEN 1 ELSE 0 END,
 catalog_enabled=$6,catalog_generation=catalog_generation+CASE WHEN catalog_enabled<>$6 THEN 1 ELSE 0 END,
 use_instance_debrid=$7,use_instance_announces=$8 WHERE singleton AND revision=$2`, value.Enabled, value.Revision, value.TorznabEnabled, value.SearchEnabled, value.StremioEnabled, value.CatalogEnabled, value.UseInstanceDebrid, value.UseInstanceAnnounces)
	if err != nil {
		return databaseError("save public access", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if previous != value.Enabled {
		kind := "security.public_access_disabled"
		if value.Enabled {
			kind = "security.public_access_enabled"
		}
		if err = recordAccessActivityTx(ctx, tx, kind, "public"); err != nil {
			return err
		}
	}
	if previousTorznab != value.TorznabEnabled {
		kind := "security.public_torznab_disabled"
		if value.TorznabEnabled {
			kind = "security.public_torznab_enabled"
		}
		if err = recordAccessActivityTx(ctx, tx, kind, "public"); err != nil {
			return err
		}
	}
	if previousSearch != value.SearchEnabled {
		kind := "security.public_search_disabled"
		if value.SearchEnabled {
			kind = "security.public_search_enabled"
		}
		if err = recordAccessActivityTx(ctx, tx, kind, "public"); err != nil {
			return err
		}
	}
	if previousStremio != value.StremioEnabled {
		kind := "security.public_stremio_disabled"
		if value.StremioEnabled {
			kind = "security.public_stremio_enabled"
		}
		if err = recordAccessActivityTx(ctx, tx, kind, "public"); err != nil {
			return err
		}
	}
	if previousCatalog != value.CatalogEnabled {
		kind := "security.public_catalog_disabled"
		if value.CatalogEnabled {
			kind = "security.public_catalog_enabled"
		}
		if err = recordAccessActivityTx(ctx, tx, kind, "public"); err != nil {
			return err
		}
	}
	if previousDebrid != value.UseInstanceDebrid || previousAnnounces != value.UseInstanceAnnounces {
		if err = recordAccessActivityTx(ctx, tx, "security.public_credentials_changed", "public"); err != nil {
			return err
		}
	}
	return databaseError("commit public access change", tx.Commit(ctx))
}
func (s *Store) SharedAvailability(ctx context.Context, service string, hashes []string, after time.Time) ([]model.AIOAvailability, error) {
	rows, err := s.pool.Query(ctx, `SELECT hash,cached,checked_at,files FROM aiosources.aio_shared_availability WHERE service=$1 AND hash=ANY($2) AND checked_at>$3 AND checked_at<=now() AND (cached OR checked_at>now()-interval '15 minutes')`, service, hashes, after)
	if err != nil {
		return nil, databaseError("read shared availability", err)
	}
	defer rows.Close()
	out := []model.AIOAvailability{}
	for rows.Next() {
		var a model.AIOAvailability
		var data []byte
		if err = rows.Scan(&a.Hash, &a.Cached, &a.CheckedAt, &data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &a.Files); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, databaseError("read shared availability", rows.Err())
}
func (s *Store) SaveSharedAvailability(ctx context.Context, service string, a model.AIOAvailability) error {
	if !model.IsDebridService(service) || !sharedHashRE.MatchString(a.Hash) || a.CheckedAt.IsZero() {
		return model.ErrInvalid
	}
	files := append([]model.AIOFile{}, a.Files...)
	for i := range files {
		files[i].ID = 0
		files[i].Link = ""
		if files[i].Size < 0 || len(files[i].Path) > 4096 {
			return model.ErrInvalid
		}
	}
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `WITH generation AS MATERIALIZED(SELECT 1 FROM aiosources.aio_state WHERE cache_generation=$6 FOR SHARE)
INSERT INTO aiosources.aio_shared_availability(service,hash,cached,checked_at,files) SELECT $1,$2,$3,$4,$5 FROM generation ON CONFLICT(service,hash) DO UPDATE SET cached=EXCLUDED.cached,checked_at=EXCLUDED.checked_at,files=EXCLUDED.files,catalog_import_source='' WHERE aiosources.aio_shared_availability.catalog_import_source<>'' OR aiosources.aio_shared_availability.checked_at<=EXCLUDED.checked_at`, service, strings.ToLower(a.Hash), a.Cached, a.CheckedAt, data, a.Generation)
	return databaseError("save shared availability", err)
}
func (s *Store) ForgetSharedAvailability(ctx context.Context, service, hash string, observed time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM aiosources.aio_shared_availability WHERE service=$1 AND hash=$2 AND checked_at<=$3`, service, hash, observed)
	return databaseError("invalidate shared availability", err)
}
