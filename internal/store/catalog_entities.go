package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

func catalogEntitiesJSON(value model.CatalogEntities) []byte {
	data, _ := json.Marshal(value)
	return data
}

func lockCatalogEntityWrites(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT ingest.lock_catalog_entity_writes()`)
	return databaseError("lock shared data publication", err)
}

func seedCatalogEntities(ctx context.Context, tx pgx.Tx, share model.Share) error {
	if share.Format != "catalog" || !share.Enabled || !share.CatalogEntities.Metadata {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,8)`); err != nil {
		return err
	}
	if share.CatalogEntities.Metadata {
		_, err := tx.Exec(ctx, `INSERT INTO ingest.catalog_entity_changes(kind,identity,changed_tx)
SELECT 'metadata',w.media_type||':'||w.tmdb_id,pg_current_xact_id()::text::bigint FROM ingest.aio_works w
WHERE w.catalog_import_source='' AND EXISTS(SELECT 1 FROM ingest.aio_matches m JOIN ingest.torrents t USING(provider_id,source_id)
WHERE m.work_id=w.id AND `+aioPublishedMatch+` AND ($1 OR t.provider_id=ANY($2::text[]))) ON CONFLICT DO NOTHING`, share.Scope == "all", share.SourceIDs)
		if err != nil {
			return databaseError("initialize shared metadata", err)
		}
	}

	return nil
}

// Metadata updates have their own delta, including when no torrent
// changed. Only committed transactions below snapshot xmin are acknowledged:
// an older still-running writer can never publish behind the checkpoint.
func catalogEntityPage(ctx context.Context, tx pgx.Tx, share model.Share, state *catalogToken, result *model.CatalogEnvelope) (bool, error) {
	limit := min(state.Limit, 100)
	rows, err := tx.Query(ctx, `SELECT c.kind,c.identity,c.changed_tx FROM ingest.catalog_entity_changes c
WHERE c.changed_tx>$1 AND c.changed_tx<=$2 AND (c.changed_tx,c.kind||':'||c.identity)>($3,$4)
AND (c.kind='metadata' AND $5 AND EXISTS(SELECT 1 FROM ingest.aio_works w
 WHERE w.media_type=split_part(c.identity,':',1) AND w.tmdb_id=split_part(c.identity,':',2) AND w.catalog_import_source='' AND EXISTS(
 SELECT 1 FROM ingest.aio_matches m JOIN ingest.torrents t USING(provider_id,source_id) WHERE m.work_id=w.id AND `+aioPublishedMatch+` AND ($6 OR t.provider_id=ANY($7::text[])))))
ORDER BY c.changed_tx,c.kind||':'||c.identity LIMIT $8`, state.AuxLow, state.AuxUpper, state.AuxAfter, state.AuxIdentity, state.Entities.Metadata, share.Scope == "all", share.SourceIDs, limit+1)
	if err != nil {
		return false, databaseError("read shared data changes", err)
	}
	type entry struct {
		kind, key string
		tx        int64
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.kind, &e.key, &e.tx); err != nil {
			rows.Close()
			return false, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	more := len(entries) > limit
	if more {
		entries = entries[:limit]
	}
	bytes := 0
	for _, e := range entries {
		if e.kind == "metadata" {
			var updated time.Time
			row := tx.QueryRow(ctx, `SELECT `+aioWorkColumns+`,w.updated_at FROM ingest.aio_works w WHERE w.media_type=split_part($1,':',1) AND w.tmdb_id=split_part($1,':',2) AND w.catalog_import_source=''`, e.key)
			w, err := scanAIOWork(catalogMetadataRow{Row: row, updated: &updated})
			if err != nil {
				if err == pgx.ErrNoRows {
					state.AuxAfter, state.AuxIdentity = e.tx, e.kind+":"+e.key
					continue
				}
				return false, err
			}
			m := model.CatalogWork(w, updated)
			if m.Valid(time.Now()) {
				data, _ := json.Marshal(m)
				if bytes+len(data) > 6<<20 {
					return true, nil
				}
				bytes += len(data)
				result.Supplement.Metadata = append(result.Supplement.Metadata, m)
			}
		}
		state.AuxAfter, state.AuxIdentity = e.tx, e.kind+":"+e.key
	}
	return more, nil
}

type catalogMetadataRow struct {
	pgx.Row
	updated *time.Time
}

func (r catalogMetadataRow) Scan(dest ...any) error { return r.Row.Scan(append(dest, r.updated)...) }
