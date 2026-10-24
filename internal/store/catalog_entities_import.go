package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

func stageCatalogEntities(ctx context.Context, tx pgx.Tx, run model.Run, supplement model.CatalogSupplement, reset bool) error {
	if !supplement.Valid(run.Config.CatalogEntities, time.Now()) || (!run.Config.IsRemoteCatalog() && (len(supplement.Metadata)+len(supplement.Availability) > 0)) {
		return model.ErrInvalid
	}
	if reset {
		if _, err := tx.Exec(ctx, `DELETE FROM ingest.run_catalog_entities WHERE run_id=$1`, run.ID); err != nil {
			return err
		}
	}
	put := func(kind, key string, data any) error {
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO ingest.run_catalog_entities(run_id,kind,identity,data) VALUES($1,$2,$3,$4)
ON CONFLICT(run_id,kind,identity) DO UPDATE SET data=EXCLUDED.data
WHERE CASE WHEN EXCLUDED.kind='metadata' THEN (EXCLUDED.data->>'updated_at')::timestamptz>=(ingest.run_catalog_entities.data->>'updated_at')::timestamptz
ELSE (EXCLUDED.data->>'checked_at')::timestamptz>=(ingest.run_catalog_entities.data->>'checked_at')::timestamptz END`, run.ID, kind, key, body)
		return databaseError("stage shared data", err)
	}
	for _, m := range supplement.Metadata {
		if err := put("metadata", m.Type+":"+m.TMDB, m); err != nil {
			return err
		}
	}
	for _, a := range supplement.Availability {
		if err := put("availability", a.Service+":"+a.Hash, a); err != nil {
			return err
		}
	}
	return nil
}

func publishCatalogEntities(ctx context.Context, tx pgx.Tx, run model.Run, state model.CatalogCursor) error {
	var metadataCount, cacheCount int64
	if run.Config.CatalogEntities.Metadata {
		after := ""
		for {
			rows, err := tx.Query(ctx, `SELECT identity,data FROM ingest.run_catalog_entities WHERE run_id=$1 AND kind='metadata' AND identity>$2 ORDER BY identity LIMIT 32`, run.ID, after)
			if err != nil {
				return err
			}
			entries := []model.CatalogMetadata{}
			for rows.Next() {
				var data []byte
				if err = rows.Scan(&after, &data); err != nil {
					rows.Close()
					return err
				}
				var m model.CatalogMetadata
				if err = json.Unmarshal(data, &m); err != nil {
					rows.Close()
					return err
				}
				entries = append(entries, m)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				break
			}
			inputs := []AIOMetadataPublication{}
			accepted := []model.CatalogMetadata{}
			for _, m := range entries {
				var accept bool
				err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM ingest.aio_works WHERE media_type=$1 AND tmdb_id=$2)
OR EXISTS(SELECT 1 FROM ingest.aio_works WHERE media_type=$1 AND tmdb_id=$2 AND catalog_import_source=$3 AND catalog_import_updated_at<$4)`, m.Type, m.TMDB, run.ProviderID, m.UpdatedAt).Scan(&accept)
				if err != nil {
					return err
				}
				if !accept {
					continue
				}
				normalized := make([]string, len(m.Titles))
				for i, title := range m.Titles {
					normalized[i] = model.NormalizeTitle(title)
				}
				inputs = append(inputs, AIOMetadataPublication{Work: m.Work(), Normalized: normalized})
				accepted = append(accepted, m)
			}
			if err = saveAIOMetadataBatch(ctx, tx, inputs, run.ProviderID); err != nil {
				return err
			}
			for _, m := range accepted {
				if _, err = tx.Exec(ctx, `UPDATE ingest.aio_works SET catalog_import_updated_at=$3,updated_at=$3 WHERE media_type=$1 AND tmdb_id=$2 AND catalog_import_source=$4`, m.Type, m.TMDB, m.UpdatedAt, run.ProviderID); err != nil {
					return err
				}
			}
			metadataCount += int64(len(accepted))
		}
	}
	if run.Config.CatalogEntities.Availability {
		config, err := readAIOConfig(ctx, tx)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO ingest.aio_shared_availability AS previous(service,hash,cached,checked_at,files,catalog_import_source)
SELECT e.data->>'service',e.data->>'hash',TRUE,(e.data->>'checked_at')::timestamptz,e.data->'files',$2
FROM ingest.run_catalog_entities e CROSS JOIN ingest.aio_state s
WHERE e.run_id=$1 AND e.kind='availability' AND s.cache_generation=$3
AND (e.data->>'checked_at')::timestamptz>now()-make_interval(mins=>$4)
AND (e.data->>'checked_at')::timestamptz<=now()
AND EXISTS(SELECT 1 FROM ingest.torrents t WHERE t.provider_id=$2 AND t.search_info_hash=e.data->>'hash')
ON CONFLICT(service,hash) DO UPDATE SET cached=TRUE,checked_at=EXCLUDED.checked_at,files=EXCLUDED.files,catalog_import_source=EXCLUDED.catalog_import_source
WHERE previous.checked_at<EXCLUDED.checked_at AND (previous.catalog_import_source<>'' OR previous.checked_at<now()-make_interval(mins=>CASE WHEN previous.cached THEN $4 ELSE LEAST($4,15) END))`, run.ID, run.ProviderID, state.CacheGeneration, config.CacheMinutes)
		if err != nil {
			return databaseError("publish shared cache observations", err)
		}
		cacheCount = tag.RowsAffected()
	}
	if metadataCount+cacheCount > 0 {
		body, _ := json.Marshal(map[string]int64{"metadata": metadataCount, "cache_observations": cacheCount})
		if _, err := tx.Exec(ctx, `INSERT INTO ingest.events(run_id,kind,message,data) VALUES($1,'shared_data','Shared metadata and cache observations imported',$2)`, run.ID, body); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM ingest.run_catalog_entities WHERE run_id=$1`, run.ID)
	return databaseError("release shared data staging", err)
}
