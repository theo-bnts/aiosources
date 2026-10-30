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

const publicAccessSchema = `
CREATE TABLE ingest.aio_public_access(singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton), revision BIGINT NOT NULL DEFAULT 1, enabled BOOLEAN NOT NULL DEFAULT FALSE, generation BIGINT NOT NULL DEFAULT 1);
INSERT INTO ingest.aio_public_access(singleton) VALUES(TRUE);
CREATE TABLE ingest.aio_shared_availability(service TEXT NOT NULL CHECK(service IN ('alldebrid','torbox')), hash TEXT NOT NULL, cached BOOLEAN NOT NULL, checked_at TIMESTAMPTZ NOT NULL, files JSONB NOT NULL CHECK(jsonb_typeof(files)='array'), PRIMARY KEY(service,hash));
ALTER TABLE ingest.stremio_requests ADD COLUMN origin TEXT NOT NULL DEFAULT 'private' CHECK(origin IN ('private','public'));
ALTER TABLE ingest.stremio_requests ADD COLUMN kind TEXT NOT NULL DEFAULT 'stream' CHECK(kind IN ('stream','manifest','playback'));
ALTER TABLE ingest.stremio_requests ADD COLUMN service TEXT NOT NULL DEFAULT '' CHECK(service IN ('','alldebrid','torbox'));
ALTER TABLE ingest.stremio_requests ADD COLUMN calls JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(calls)='array');
ALTER TABLE ingest.stremio_requests DROP CONSTRAINT stremio_requests_outcome_check;
ALTER TABLE ingest.stremio_requests ADD CHECK(outcome IN ('results','empty','invalid','failed','cancelled','busy','pending'));
ALTER TABLE ingest.operation_events ADD COLUMN duration_ms BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ingest.operation_events ADD COLUMN http_status INTEGER NOT NULL DEFAULT 0;
ALTER TABLE ingest.operation_events ADD COLUMN error_code TEXT NOT NULL DEFAULT '';
`

func (s *Store) PublicAccess(ctx context.Context) (model.PublicAccess, error) {
	var out model.PublicAccess
	err := s.pool.QueryRow(ctx, `SELECT revision,enabled,expose_sources,generation,torznab_enabled,torznab_generation,search_enabled,stremio_enabled,stremio_generation FROM ingest.aio_public_access WHERE singleton`).Scan(&out.Revision, &out.Enabled, &out.ExposeSources, &out.Generation, &out.TorznabEnabled, &out.TorznabGeneration, &out.SearchEnabled, &out.StremioEnabled, &out.StremioGeneration)
	return out, databaseError("read public access", err)
}
func (s *Store) SetPublicAccess(ctx context.Context, value model.PublicAccess) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin public access change", err)
	}
	defer rollback(tx)
	var previous, previousDownloads, previousTorznab, previousSearch, previousStremio bool
	err = tx.QueryRow(ctx, `SELECT enabled,expose_sources,torznab_enabled,search_enabled,stremio_enabled FROM ingest.aio_public_access WHERE singleton AND revision=$1 FOR UPDATE`, value.Revision).Scan(&previous, &previousDownloads, &previousTorznab, &previousSearch, &previousStremio)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrConflict
	}
	if err != nil {
		return databaseError("lock public access", err)
	}
	tag, err := tx.Exec(ctx, `UPDATE ingest.aio_public_access SET revision=revision+1,enabled=$1,expose_sources=$3,generation=generation+CASE WHEN enabled<>$1 THEN 1 ELSE 0 END,torznab_enabled=$4,torznab_generation=torznab_generation+CASE WHEN torznab_enabled<>$4 THEN 1 ELSE 0 END,search_enabled=$5,stremio_enabled=$6,stremio_generation=stremio_generation+CASE WHEN stremio_enabled<>$6 THEN 1 ELSE 0 END WHERE singleton AND revision=$2`, value.Enabled, value.Revision, value.ExposeSources, value.TorznabEnabled, value.SearchEnabled, value.StremioEnabled)
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
	if previousDownloads != value.ExposeSources {
		kind := "security.public_source_downloads_disabled"
		if value.ExposeSources {
			kind = "security.public_source_downloads_enabled"
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
	return databaseError("commit public access change", tx.Commit(ctx))
}
func (s *Store) SharedAvailability(ctx context.Context, service string, hashes []string, after time.Time) ([]model.AIOAvailability, error) {
	rows, err := s.pool.Query(ctx, `SELECT hash,cached,checked_at,files FROM ingest.aio_shared_availability WHERE service=$1 AND hash=ANY($2) AND checked_at>$3 AND checked_at<=now() AND (cached OR checked_at>now()-interval '15 minutes')`, service, hashes, after)
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
	_, err = s.pool.Exec(ctx, `WITH generation AS MATERIALIZED(SELECT 1 FROM ingest.aio_state WHERE cache_generation=$6 FOR SHARE)
INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) SELECT $1,$2,$3,$4,$5 FROM generation ON CONFLICT(service,hash) DO UPDATE SET cached=EXCLUDED.cached,checked_at=EXCLUDED.checked_at,files=EXCLUDED.files,catalog_import_source='' WHERE ingest.aio_shared_availability.catalog_import_source<>'' OR ingest.aio_shared_availability.checked_at<=EXCLUDED.checked_at`, service, strings.ToLower(a.Hash), a.Cached, a.CheckedAt, data, a.Generation)
	return databaseError("save shared availability", err)
}
func (s *Store) ForgetSharedAvailability(ctx context.Context, service, hash string, observed time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ingest.aio_shared_availability WHERE service=$1 AND hash=$2 AND checked_at<=$3`, service, hash, observed)
	return databaseError("invalidate shared availability", err)
}
