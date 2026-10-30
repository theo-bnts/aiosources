package store

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Capture relationships before deleting metadata. Availability belongs to a
// hash, not a source occurrence or a metadata provider, so every copy of a
// selected hash must be invalidated without clearing unrelated observations.
func (s *Store) resetInformation(ctx context.Context, scope string) error {
	types, services, resources := []string{"book"}, []string{"openlibrary"}, []string{"book", "ebook", "audiobook", "openlibrary"}
	if scope == "videos" || scope == "metadata" {
		videoResources := []string{"movie", "tv", "series", "anime", "imdb", "tmdb", "tvdb", "mal", "kitsu", "anilist"}
		if scope == "videos" {
			types, services, resources = []string{"movie", "tv"}, []string{"tmdb"}, videoResources
		} else {
			types, services, resources = append(types, "movie", "tv"), append(services, "tmdb"), append(resources, videoResources...)
		}
	} else if scope != "books" {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin information reset", err)
	}
	defer rollback(tx)
	// Retention takes this fence before catalog publication and metadata locks.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,4);
SELECT metadata_generation FROM ingest.aio_state FOR UPDATE;
SELECT ingest.lock_catalog_entity_writes();
CREATE TEMP TABLE reset_information_records(provider_id TEXT,source_id TEXT,occurrence_id TEXT,hash TEXT,PRIMARY KEY(provider_id,source_id)) ON COMMIT DROP;
CREATE TEMP TABLE reset_information_candidates(provider_id TEXT,source_id TEXT,PRIMARY KEY(provider_id,source_id)) ON COMMIT DROP;
CREATE TEMP TABLE reset_information_hashes(hash TEXT PRIMARY KEY) ON COMMIT DROP;
CREATE TEMP TABLE reset_information_operations(id TEXT PRIMARY KEY) ON COMMIT DROP;`); err != nil {
		return databaseError("lock information reset", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reset_information_records
SELECT t.provider_id,t.source_id,t.occurrence_id,t.search_info_hash
FROM ingest.torrents t LEFT JOIN ingest.aio_matches m USING(provider_id,source_id)
LEFT JOIN ingest.aio_works w ON w.id=m.work_id LEFT JOIN ingest.aio_match_reviews r USING(provider_id,source_id)
WHERE w.media_type=ANY($1::text[]) OR (r.active AND r.media_type=ANY($1::text[]));
`, types); err != nil {
		return databaseError("select related identifications", err)
	}
	// Include undecided automatic results and explicit exclusions, even if no
	// work remains attached. Reuse the identification classifier; do not infer a
	// book from an unrelated numeric source category or copy source payloads.
	if _, err = tx.Exec(ctx, `INSERT INTO reset_information_candidates
SELECT t.provider_id,t.source_id FROM (
 SELECT hash FROM ingest.aio_availability UNION SELECT hash FROM ingest.aio_shared_availability
 UNION SELECT hash FROM ingest.aio_preparations
) cached JOIN ingest.torrents t ON t.search_info_hash=cached.hash
UNION SELECT provider_id,source_id FROM ingest.aio_match_reviews WHERE media_type=''
UNION SELECT provider_id,source_id FROM ingest.aio_matches WHERE work_id IS NULL
ON CONFLICT DO NOTHING`); err != nil {
		return databaseError("select related cache observations", err)
	}
	if err = selectResetInformationRecords(ctx, tx, types); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reset_information_hashes
SELECT hash FROM reset_information_records WHERE hash IS NOT NULL
UNION SELECT unnest(hashes) FROM ingest.aio_resources WHERE split_part(resource,':',1)=ANY($1::text[])
ON CONFLICT DO NOTHING`, resources); err != nil {
		return databaseError("select related cache hashes", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO reset_information_operations
SELECT r.id FROM ingest.operation_runs r WHERE (r.kind='metadata' AND r.service=ANY($1::text[]))
OR (r.kind IN ('cache','download') AND split_part(r.resource,':',1)=ANY($2::text[]))
OR EXISTS(SELECT 1 FROM ingest.operation_downloads d WHERE d.run_id=r.id AND (
 EXISTS(SELECT 1 FROM ingest.aio_works w WHERE w.id=d.work_id AND w.media_type=ANY($3::text[]))
 OR d.input->>'occurrence_id' IN(SELECT occurrence_id FROM reset_information_records)))
OR ($4 AND r.kind='matching')`, services, resources, types, scope == "metadata"); err != nil {
		return databaseError("select related operation history", err)
	}
	// A live preparation owns its operation's advisory lock. Refuse the entire
	// reset before deleting anything, rather than lose its continuation or events.
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.lock_key FROM ingest.operation_runs r
JOIN reset_information_operations selected ON selected.id=r.id
WHERE r.kind IN ('cache','download') ORDER BY r.lock_key`)
	if err != nil {
		return databaseError("read related operation locks", err)
	}
	keys := []int64{}
	for rows.Next() {
		var key int64
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, key := range keys {
		var locked bool
		if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&locked); err != nil {
			return databaseError("lock related operation", err)
		}
		if !locked {
			return model.ErrBusy
		}
	}
	queries := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM ingest.activity_logs WHERE service=ANY($1::text[]) OR run_id IN(SELECT id FROM reset_information_operations)`, []any{services}},
		{`DELETE FROM ingest.operation_events WHERE run_id IN(SELECT id FROM reset_information_operations)`, nil},
		{`DELETE FROM ingest.operation_runs WHERE id IN(SELECT id FROM reset_information_operations)`, nil},
		{`DELETE FROM ingest.aio_availability WHERE hash IN(SELECT hash FROM reset_information_hashes)`, nil},
		{`DELETE FROM ingest.aio_shared_availability WHERE hash IN(SELECT hash FROM reset_information_hashes)`, nil},
		{`DELETE FROM ingest.aio_resources r WHERE split_part(resource,':',1)=ANY($1::text[]) OR EXISTS(SELECT 1 FROM reset_information_hashes h WHERE h.hash=ANY(r.hashes))`, []any{resources}},
		{`DELETE FROM ingest.stremio_requests WHERE $1 AND kind<>'manifest' AND media_type IN ('movie','series','anime')`, []any{scope != "books"}},
		{`DELETE FROM ingest.aio_match_reviews r WHERE r.media_type=ANY($1::text[]) OR (r.media_type='' AND (r.provider_id,r.source_id) IN(SELECT provider_id,source_id FROM reset_information_records))`, []any{types}},
		{`DELETE FROM ingest.aio_matches WHERE (provider_id,source_id) IN(SELECT provider_id,source_id FROM reset_information_records)`, nil},
		{`INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM reset_information_records ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1`, nil},
		{`DELETE FROM ingest.aio_works WHERE media_type=ANY($1::text[])`, []any{types}},
	}
	// Provider object ownership is retained: resetting local observations must
	// never orphan temporary objects or delete files from a debrid account.
	for _, query := range queries {
		if _, err = tx.Exec(ctx, query.sql, query.args...); err != nil {
			return databaseError("remove related information data", err)
		}
	}
	if scope != "videos" {
		if _, err = tx.Exec(ctx, `TRUNCATE ingest.ol_authors,ingest.ol_editions,ingest.ol_redirects;
UPDATE ingest.ol_import_state SET snapshot='',etag='',position=0,imported=0,works=0,editions=0,authors=0,skipped=0,last_sync=NULL,checked_at=NULL,retry_at=NULL,error='',complete=FALSE WHERE singleton;`+resetBookChangesSQL); err != nil {
			return databaseError("reset book information", err)
		}
	}
	if scope != "books" {
		if _, err = tx.Exec(ctx, `TRUNCATE ingest.aio_metadata_jobs,ingest.aio_season_requests,ingest.aio_inventory;
DELETE FROM ingest.catalog_entity_changes WHERE kind='metadata';
UPDATE ingest.aio_state SET last_sync=NULL,changes_through=NULL,inventory_date='',
sync_retry_at=CASE WHEN provider_retry_at>now() THEN provider_retry_at END,
sync_failures=0,sync_error='',recovery_started_at=NULL,applied_through=NULL;`); err != nil {
			return databaseError("reset video information", err)
		}
	}
	if scope == "metadata" {
		if _, err = tx.Exec(ctx, `DELETE FROM ingest.aio_matches;DELETE FROM ingest.aio_match_reviews;
INSERT INTO ingest.aio_match_queue(provider_id,source_id) SELECT provider_id,source_id FROM ingest.torrents
ON CONFLICT(provider_id,source_id) DO UPDATE SET version=ingest.aio_match_queue.version+1;
UPDATE ingest.aio_state SET last_match=NULL;`); err != nil {
			return databaseError("reset identification", err)
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_state SET metadata_generation=metadata_generation+1,cache_generation=cache_generation+1;`); err != nil {
		return err
	}
	if err = notify(ctx, tx); err != nil {
		return err
	}
	return databaseError("commit information reset", tx.Commit(ctx))
}

func selectResetInformationRecords(ctx context.Context, tx pgx.Tx, types []string) error {
	provider, source := "", ""
	for {
		rows, err := tx.Query(ctx, `SELECT `+aioRecordColumns+`,COALESCE(CASE WHEN r.active THEN NULLIF(r.media_type,'') END,w.media_type,''),m.provider_id IS NOT NULL
FROM reset_information_candidates c JOIN ingest.torrents t USING(provider_id,source_id)
LEFT JOIN ingest.aio_match_reviews r USING(provider_id,source_id) LEFT JOIN ingest.aio_matches m USING(provider_id,source_id)
LEFT JOIN ingest.aio_works w ON w.id=m.work_id
WHERE (t.provider_id,t.source_id)>($1,$2) ORDER BY t.provider_id,t.source_id LIMIT 1000`, provider, source)
		if err != nil {
			return databaseError("read related cache records", err)
		}
		providers, sources := []string{}, []string{}
		count := 0
		for rows.Next() {
			var kind string
			var decision bool
			record, err := scanAIORecord(aioStreamRow{scanner: rows, ids: []any{&kind, &decision}})
			if err != nil {
				rows.Close()
				return err
			}
			count++
			provider, source = record.Provider, record.SourceID
			if kind == "" {
				if books.ParseRecord(record).Kind != "" {
					kind = "book"
				} else if decision || record.IMDb != "" || record.TMDB != "" || record.TVDB != "" || record.MAL != "" {
					kind = "movie"
				}
			}
			if slices.Contains(types, kind) {
				providers, sources = append(providers, record.Provider), append(sources, record.SourceID)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(providers) > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO reset_information_records
SELECT t.provider_id,t.source_id,t.occurrence_id,t.search_info_hash FROM ingest.torrents t
WHERE (provider_id,source_id) IN(SELECT * FROM unnest($1::text[],$2::text[])) ON CONFLICT DO NOTHING`, providers, sources); err != nil {
				return databaseError("select related cache records", err)
			}
		}
		if count < 1000 {
			return nil
		}
	}
}
