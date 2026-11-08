package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Only optional query accelerators belong here. Identity, uniqueness, foreign
// key, recovery and publication indexes cannot be disabled by configuration.
var optionalStorageIndexes = []struct{ name, definition string }{
	{"torrents_fulltext", `ON ingest.torrents USING GIN(search_document)`},
	{"torrents_fragments", `ON ingest.torrents USING GIN(ingest.torrent_search_text(provider_id,source_id,fields) public.gin_trgm_ops)`},
	{"torrents_categories", `ON ingest.torrents USING GIN(search_categories)`},
	{"torrents_size", `ON ingest.torrents((ingest.torrent_integer_key(search_size) COLLATE "C")) WHERE search_size IS NOT NULL`},
	{"torrents_published_at", `ON ingest.torrents(search_published_at,provider_id,source_id) WHERE search_published_at IS NOT NULL`},
	{"raw_records_fragments", `ON ingest.raw_records USING GIN(ingest.raw_search_text(source_id,fields,error) public.gin_trgm_ops)`},
}

func validStorageIndexes(input model.StorageIndexSettings) bool {
	if input.Revision < 1 || len(input.Values) != len(optionalStorageIndexes) {
		return false
	}
	for _, index := range optionalStorageIndexes {
		if _, ok := input.Values[index.name]; !ok {
			return false
		}
	}
	return true
}

func (s *Store) StorageIndexes(ctx context.Context) (model.StorageIndexSettings, error) {
	var result model.StorageIndexSettings
	var encoded, failures []byte
	if err := s.pool.QueryRow(ctx, `SELECT revision,values,errors FROM ingest.storage_index_settings WHERE singleton`).Scan(&result.Revision, &encoded, &failures); err != nil {
		return result, databaseError("read optional index settings", err)
	}
	if json.Unmarshal(encoded, &result.Values) != nil || !validStorageIndexes(result) {
		return result, model.ErrInvalid
	}
	problems := map[string]string{}
	if json.Unmarshal(failures, &problems) != nil {
		return result, model.ErrInvalid
	}
	result.Status = []model.StorageIndexStatus{}
	names := make([]string, 0, len(optionalStorageIndexes))
	for _, index := range optionalStorageIndexes {
		names = append(names, index.name)
	}
	rows, err := s.pool.Query(ctx, `SELECT configured.name,c.oid IS NOT NULL,i.indisvalid IS TRUE,
 CASE WHEN c.oid IS NULL THEN 0 ELSE pg_relation_size(c.oid) END
 FROM unnest($1::text[]) WITH ORDINALITY AS configured(name,position)
 LEFT JOIN pg_class c ON c.relname=configured.name AND c.relnamespace='ingest'::regnamespace
 LEFT JOIN pg_index i ON i.indexrelid=c.oid ORDER BY configured.position`, names)
	if err != nil {
		return result, databaseError("measure optional indexes", err)
	}
	defer rows.Close()
	for rows.Next() {
		var index model.StorageIndexStatus
		var present, valid bool
		if err := rows.Scan(&index.Name, &present, &valid, &index.Bytes); err != nil {
			return result, databaseError("read optional index measurement", err)
		}
		index.Enabled = result.Values[index.Name]
		index.Error = problems[index.Name]
		index.Ready = (index.Enabled && valid) || (!index.Enabled && !present)
		result.Status = append(result.Status, index)
	}
	return result, databaseError("measure optional indexes", rows.Err())
}

func (s *Store) UpdateStorageIndexes(ctx context.Context, input model.StorageIndexSettings) error {
	if !validStorageIndexes(input) {
		return model.ErrInvalid
	}
	encoded, err := json.Marshal(input.Values)
	if err != nil {
		return model.ErrInvalid
	}
	result, err := s.pool.Exec(ctx, `UPDATE ingest.storage_index_settings SET values=$1,errors='{}'::jsonb,revision=revision+1 WHERE singleton AND revision=$2`, encoded, input.Revision)
	if err != nil {
		return databaseError("save optional index settings", err)
	}
	if result.RowsAffected() == 0 {
		return model.ErrConflict
	}
	return nil
}

// Concurrent DDL never runs in an API request or startup migration. PostgreSQL
// continues serving reads and imports while building/dropping an accelerator.
// The durable target is reconciled one index at a time across all instances.
func (s *Store) ReconcileStorageIndexes(ctx context.Context) (bool, error) {
	release, locked, err := s.AIOLock(ctx, "optional-storage-indexes")
	if err != nil || !locked {
		return false, err
	}
	defer release()
	settings, err := s.StorageIndexes(ctx)
	if err != nil {
		return false, err
	}
	for i, index := range settings.Status {
		if index.Ready {
			if index.Error != "" {
				s.storageIndexResult(ctx, index.Name, nil)
			}
			continue
		}
		config := s.pool.Config().ConnConfig.Copy()
		config.RuntimeParams["application_name"] = "aiosources-storage-indexes"
		conn, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			return false, err
		}
		defer closeConnection(conn)
		var admitted bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock_shared(1768843109,5)`).Scan(&admitted); err != nil || !admitted {
			return false, err
		}
		defer func() {
			unlock, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_, _ = conn.Exec(unlock, `SELECT pg_advisory_unlock_shared(1768843109,5)`)
		}()
		if _, err = conn.Exec(ctx, `SET lock_timeout='2s'`); err != nil {
			return false, err
		}
		// An interrupted concurrent build may leave an invalid index. Dropping
		// it first makes the next build recoverable rather than silently skipped.
		name := pgx.Identifier{"ingest", index.Name}.Sanitize()
		if _, err = conn.Exec(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+name); err != nil {
			s.storageIndexResult(ctx, index.Name, err)
			return false, databaseError("remove optional index", err)
		}
		if index.Enabled {
			_, err = conn.Exec(ctx, `CREATE INDEX CONCURRENTLY `+pgx.Identifier{index.Name}.Sanitize()+` `+optionalStorageIndexes[i].definition)
			if err != nil {
				s.storageIndexResult(ctx, index.Name, err)
				return false, databaseError("build optional index", err)
			}
		}
		s.storageIndexResult(ctx, index.Name, nil)
		return true, nil
	}
	return false, nil
}

func (s *Store) storageIndexResult(ctx context.Context, name string, cause error) {
	if ctx.Err() != nil {
		return
	}
	var problem *pgconn.PgError
	if errors.As(cause, &problem) && (problem.Code == "55P03" || problem.Code == "57014") {
		return
	}
	message := ""
	if cause != nil {
		message = "This index could not be changed. Check database permissions and free disk space; the change will be retried."
	}
	_, _ = s.pool.Exec(ctx, `UPDATE ingest.storage_index_settings SET errors=CASE WHEN $2='' THEN errors-$1 ELSE jsonb_set(errors,ARRAY[$1],to_jsonb($2::text)) END WHERE singleton AND COALESCE(errors->>$1,'')<>$2`, name, message)
}
