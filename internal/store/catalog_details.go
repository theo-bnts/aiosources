package store

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

type catalogLocalKey struct{ provider, source string }

func decodeCatalogFields(raw []byte) (map[string]any, error) {
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&fields)
	return fields, err
}

func enrichCatalogItems(ctx context.Context, tx pgx.Tx, share model.Share, permissions model.CatalogEntities, items []model.CatalogItem, keys []catalogLocalKey) error {
	providers, sources := make([]string, len(keys)), make([]string, len(keys))
	indices := make(map[catalogLocalKey]int, len(keys))
	for i, key := range keys {
		providers[i], sources[i], indices[key] = key.provider, key.source, i
	}
	rows, err := tx.Query(ctx, `SELECT t.provider_id,t.source_id,t.fields,ingest.catalog_origin(t.origin,t.provider_id,t.source_id)
FROM ingest.torrents t JOIN unnest($1::text[],$2::text[]) k(provider_id,source_id) USING(provider_id,source_id)`, providers, sources)
	if err != nil {
		return databaseError("read shared torrent information", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key catalogLocalKey
		var raw, origin []byte
		if err = rows.Scan(&key.provider, &key.source, &raw, &origin); err != nil {
			return err
		}
		var currentOrigin model.CatalogOrigin
		if err = json.Unmarshal(origin, &currentOrigin); err != nil {
			return err
		}
		item := &items[indices[key]]
		if item.Deleted || item.Origin != currentOrigin {
			continue
		}
		base, err := projectCatalogFields(raw, share.Fields)
		if err != nil {
			return err
		}
		if model.CatalogFieldsFingerprint(base) != model.CatalogFieldsFingerprint(item.Fields) {
			continue // The immutable base changed; its next delta carries the facts.
		}
		fields, err := decodeCatalogFields(raw)
		if err != nil {
			return err
		}
		item.Details = model.ProjectCatalogDetails(fields, permissions)
	}
	return databaseError("read shared torrent information", rows.Err())
}

// Uses a bounded keyset over changed live rows; no raw source IDs or URLs are
// exposed in cursors, and no additional copy of a source payload is stored.
func catalogDetailsPage(ctx context.Context, tx pgx.Tx, share model.Share, state *catalogToken, result *model.CatalogEnvelope) (bool, error) {
	limit := min(state.Limit, 100)
	rows, err := tx.Query(ctx, `SELECT t.catalog_details_tx,t.occurrence_id,t.fields,ingest.catalog_origin(t.origin,t.provider_id,t.source_id)
FROM ingest.torrents t WHERE t.catalog_details_tx>$1 AND t.catalog_details_tx<=$2
AND (t.catalog_details_tx,t.occurrence_id)>($3,$4) AND ($5 OR t.provider_id=ANY($6::text[]))
ORDER BY t.catalog_details_tx,t.occurrence_id LIMIT $7`, state.DetailsLow, state.AuxUpper, state.AuxAfter, state.AuxIdentity, share.Scope == "all", share.SourceIDs, limit+1)
	if err != nil {
		return false, databaseError("read shared torrent changes", err)
	}
	defer rows.Close()
	indices := map[string]int{}
	count := 0
	for rows.Next() {
		if count == limit {
			return true, nil
		}
		count++
		var raw, origin []byte
		if err = rows.Scan(&state.AuxAfter, &state.AuxIdentity, &raw, &origin); err != nil {
			return false, err
		}
		var entry model.CatalogTorrentDetails
		if err = json.Unmarshal(origin, &entry.Origin); err != nil {
			return false, err
		}
		base, err := projectCatalogFields(raw, share.Fields)
		if err != nil {
			return false, err
		}
		fields, err := decodeCatalogFields(raw)
		if err != nil {
			return false, err
		}
		entry.ID, entry.BaseFingerprint, entry.Fields = model.CatalogItemID(entry.Origin), model.CatalogFieldsFingerprint(base), model.ProjectCatalogDetails(fields, state.Entities)
		if index, exists := indices[entry.ID]; exists {
			result.Supplement.Torrents[index] = entry
		} else {
			indices[entry.ID] = len(result.Supplement.Torrents)
			result.Supplement.Torrents = append(result.Supplement.Torrents, entry)
		}
	}
	return false, databaseError("read shared torrent changes", rows.Err())
}

func publishCatalogTorrentDetails(ctx context.Context, tx pgx.Tx, run model.Run) error {
	after := ""
	for {
		rows, err := tx.Query(ctx, `SELECT identity,data FROM ingest.run_catalog_entities WHERE run_id=$1 AND kind='torrent' AND identity>$2 ORDER BY identity LIMIT 100`, run.ID, after)
		if err != nil {
			return err
		}
		entries := []model.CatalogTorrentDetails{}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&after, &raw); err != nil {
				rows.Close()
				return err
			}
			var entry model.CatalogTorrentDetails
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err = decoder.Decode(&entry); err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(entries) == 0 {
			return err
		}
		if err = applyCatalogTorrentDetails(ctx, tx, run.ProviderID, entries); err != nil {
			return err
		}
	}
}

// Lock in natural-identity order and pipeline writes, as for other publication
// batches. A large detail delta must not make two database round trips per row.
func applyCatalogTorrentDetails(ctx context.Context, tx pgx.Tx, provider string, entries []model.CatalogTorrentDetails) error {
	ids := make([]string, len(entries))
	byID := make(map[string]model.CatalogTorrentDetails, len(entries))
	for i, entry := range entries {
		ids[i], byID[entry.ID] = entry.ID, entry
	}
	rows, err := tx.Query(ctx, `SELECT source_id,fields,origin FROM ingest.torrents
WHERE provider_id=$1 AND source_id=ANY($2::text[]) ORDER BY source_id FOR UPDATE`, provider, ids)
	if err != nil {
		return databaseError("read shared torrent versions", err)
	}
	defer rows.Close()
	batch := &pgx.Batch{}
	for rows.Next() {
		var id string
		var raw, provenance []byte
		if err = rows.Scan(&id, &raw, &provenance); err != nil {
			return err
		}
		entry := byID[id]
		var origin model.CatalogOrigin
		if err = json.Unmarshal(provenance, &origin); err != nil {
			return err
		}
		base, err := projectCatalogFields(raw, CatalogFields())
		if err != nil {
			return err
		}
		if origin != entry.Origin || model.CatalogFieldsFingerprint(base) != entry.BaseFingerprint {
			continue
		}
		fields, err := decodeCatalogFields(raw)
		if err != nil {
			return err
		}
		data, err := json.Marshal(model.ReplaceCatalogDetails(fields, entry.Fields))
		if err != nil {
			return err
		}
		before, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if !bytes.Equal(before, data) {
			batch.Queue(`UPDATE ingest.torrents SET fields=$3 WHERE provider_id=$1 AND source_id=$2 AND fields IS DISTINCT FROM $3::jsonb`, provider, id, data)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || batch.Len() == 0 {
		return err
	}
	results := tx.SendBatch(ctx, batch)
	for range batch.Len() {
		if _, err = results.Exec(); err != nil {
			_ = results.Close()
			return databaseError("publish shared torrent information", err)
		}
	}
	return databaseError("publish shared torrent information", results.Close())
}
