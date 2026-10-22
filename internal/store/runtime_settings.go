package store

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

const runtimePolicySchema = `
CREATE TABLE ingest.runtime_settings (
 section TEXT PRIMARY KEY CHECK(section IN ('debrid','outgoing','connections','history')),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 values JSONB NOT NULL CHECK(jsonb_typeof(values)='object')
);
INSERT INTO ingest.runtime_settings(section,values) VALUES
 ('debrid','{"cache_jobs":4,"download_jobs":4,"cleanup_jobs":4,"public_checks":1}'),
 ('outgoing','{"metadata_seconds":60,"debrid_seconds":60,"inventory_seconds":600}'),
 ('connections','{"stremio_seconds":70,"torznab_seconds":20,"catalog_seconds":20}'),
 ('history','{"days":7,"cleanup_seconds":30}');
`

const runtimeSettingsSchema = runtimePolicySchema + `ALTER TABLE ingest.collection_settings ADD COLUMN auto_resume_interrupted BOOLEAN NOT NULL DEFAULT TRUE;`

type runtimeSettingsCache struct {
	loading  chan struct{}
	epoch    uint64
	mu       sync.Mutex
	until    time.Time
	sections map[string]model.RuntimeSection
}

func (s *Store) RuntimeSection(ctx context.Context, section string) (model.RuntimeSection, error) {
	if model.RuntimeFields(section) == nil {
		return model.RuntimeSection{}, model.ErrInvalid
	}
	var value model.RuntimeSection
	var encoded []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,values FROM ingest.runtime_settings WHERE section=$1`, section).Scan(&value.Revision, &encoded)
	if err != nil {
		return value, databaseError("read runtime settings", err)
	}
	if err := json.Unmarshal(encoded, &value.Values); err != nil || !value.Valid(section) {
		return value, model.ErrInvalid
	}
	return value, nil
}

func (s *Store) UpdateRuntimeSection(ctx context.Context, section string, input model.RuntimeSection) error {
	if !input.Valid(section) {
		return model.ErrInvalid
	}
	encoded, err := json.Marshal(input.Values)
	if err != nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin runtime settings update", err)
	}
	defer rollback(tx)
	result, err := tx.Exec(ctx, `UPDATE ingest.runtime_settings SET values=$2,revision=revision+1 WHERE section=$1 AND revision=$3`, section, encoded, input.Revision)
	if err != nil {
		return databaseError("save runtime settings", err)
	}
	if result.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if err := notify(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return databaseError("commit runtime settings", err)
	}
	s.runtime.mu.Lock()
	s.runtime.until = time.Time{}
	s.runtime.epoch++
	s.runtime.mu.Unlock()
	return nil
}

// A shared one-second snapshot avoids a database read for every provider call.
// Other application replicas pick up saved policies within the same bound.
func (s *Store) RuntimeValue(ctx context.Context, section, key string) (int, error) {
	if _, ok := model.RuntimeFields(section)[key]; !ok {
		return 0, model.ErrInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		s.runtime.mu.Lock()
		if time.Now().Before(s.runtime.until) {
			value := s.runtime.sections[section].Values[key]
			s.runtime.mu.Unlock()
			return value, nil
		}
		if pending := s.runtime.loading; pending != nil {
			s.runtime.mu.Unlock()
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-pending:
			}
			continue
		}
		pending, epoch := make(chan struct{}), s.runtime.epoch
		s.runtime.loading = pending
		s.runtime.mu.Unlock()
		sections, err := s.loadRuntimeSettings(ctx)
		s.runtime.mu.Lock()
		if err == nil && epoch == s.runtime.epoch {
			s.runtime.sections, s.runtime.until = sections, time.Now().Add(time.Second)
		}
		s.runtime.loading = nil
		close(pending)
		s.runtime.mu.Unlock()
		if err != nil {
			return 0, err
		}
	}
}

func (s *Store) loadRuntimeSettings(ctx context.Context) (map[string]model.RuntimeSection, error) {
	rows, err := s.pool.Query(ctx, `SELECT section,revision,values FROM ingest.runtime_settings`)
	if err != nil {
		return nil, databaseError("load runtime policy", err)
	}
	defer rows.Close()
	sections := map[string]model.RuntimeSection{}
	for rows.Next() {
		var name string
		var value model.RuntimeSection
		var encoded []byte
		if err := rows.Scan(&name, &value.Revision, &encoded); err != nil {
			return nil, databaseError("read runtime policy", err)
		}
		if err := json.Unmarshal(encoded, &value.Values); err != nil || !value.Valid(name) {
			return nil, model.ErrInvalid
		}
		sections[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("load runtime policy", err)
	}
	if len(sections) != 4 {
		return nil, model.ErrInvalid
	}
	return sections, nil
}

func (s *Store) ConnectionTimeout(ctx context.Context, format string) (time.Duration, error) {
	seconds, err := s.RuntimeValue(ctx, "connections", format+"_seconds")
	return time.Duration(seconds) * time.Second, err
}
