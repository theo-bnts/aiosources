package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

const networkSchema = `
CREATE TABLE ingest.network_settings (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 default_route TEXT NOT NULL DEFAULT 'system',
 proxies JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(proxies)='array'),
 overrides JSONB NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(overrides)='object')
);
INSERT INTO ingest.network_settings(singleton) VALUES(TRUE);
ALTER TABLE ingest.activity_logs DROP CONSTRAINT activity_logs_service_check;
ALTER TABLE ingest.activity_logs ADD CHECK(service IN ('','tmdb','alldebrid','torbox','network'));

ALTER FUNCTION ingest.access_activity_message(TEXT) RENAME TO access_activity_message_before_network;
CREATE FUNCTION ingest.access_activity_message(kind TEXT) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(ingest.access_activity_message_before_network(kind), CASE kind
 WHEN 'network.settings_changed' THEN 'Network connections changed'
 WHEN 'network.test_completed' THEN 'Proxy connection test completed'
 WHEN 'network.test_failed' THEN 'Proxy connection test failed'
 ELSE NULL END);
$$;
`

func (s *Store) NetworkSettings(ctx context.Context) (model.NetworkSettings, error) {
	var result model.NetworkSettings
	var proxies, overrides []byte
	err := s.pool.QueryRow(ctx, `SELECT revision,default_route,proxies,overrides FROM ingest.network_settings WHERE singleton`).Scan(&result.Revision, &result.Default, &proxies, &overrides)
	if err != nil {
		return result, databaseError("read network settings", err)
	}
	if json.Unmarshal(proxies, &result.Proxies) != nil || json.Unmarshal(overrides, &result.Overrides) != nil || result.Validate() != nil {
		return model.NetworkSettings{}, errors.New("Stored network settings are invalid")
	}
	return result, nil
}

func (s *Store) UpdateNetworkSettings(ctx context.Context, input model.NetworkSettings) error {
	if input.Validate() != nil {
		return model.ErrInvalid
	}
	if input.Proxies == nil {
		input.Proxies = []model.NetworkProxy{}
	}
	if input.Overrides == nil {
		input.Overrides = map[string]string{}
	}
	proxies, err := json.Marshal(input.Proxies)
	if err != nil {
		return model.ErrInvalid
	}
	overrides, err := json.Marshal(input.Overrides)
	if err != nil {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin network settings update", err)
	}
	defer rollback(tx)
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM ingest.network_settings WHERE singleton FOR UPDATE`).Scan(&revision); err != nil {
		return databaseError("lock network settings", err)
	}
	if revision != input.Revision {
		return model.ErrConflict
	}
	var references []string
	for _, proxy := range input.Proxies {
		if proxy.UsernameRef != "" {
			references = append(references, proxy.UsernameRef, proxy.PasswordRef)
		}
	}
	slices.Sort(references)
	for _, reference := range slices.Compact(references) {
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM ingest.secrets WHERE name=$1 FOR KEY SHARE`, reference).Scan(&name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrInvalid
			}
			return databaseError("lock proxy credential", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE ingest.network_settings SET revision=revision+1,default_route=$1,proxies=$2,overrides=$3 WHERE singleton`, input.Default, proxies, overrides); err != nil {
		return databaseError("update network settings", err)
	}
	if err := recordActivityTx(ctx, tx, "info", "network.settings_changed", "", "", "", nil); err != nil {
		return err
	}
	if err := notify(ctx, tx); err != nil {
		return databaseError("notify network settings update", err)
	}
	return databaseError("commit network settings update", tx.Commit(ctx))
}
