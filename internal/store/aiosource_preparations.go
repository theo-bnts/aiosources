package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
)

// These are administrator-owned transfer locators, never visitor credentials,
// tracker URLs or file links. They allow playback to reuse pending transfers.
func (s *Store) AIOPreparation(ctx context.Context, service, account, hash string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT provider_id FROM ingest.aio_preparations WHERE service=$1 AND account=$2 AND hash=$3 AND expires_at>now()`, service, account, hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, databaseError("read prepared account torrent", err)
}

func (s *Store) AIOPreparations(ctx context.Context, service, account string, hashes []string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT hash,provider_id FROM ingest.aio_preparations WHERE service=$1 AND account=$2 AND hash=ANY($3::text[]) AND expires_at>now()`, service, account, hashes)
	if err != nil {
		return nil, databaseError("read account preparations", err)
	}
	defer rows.Close()
	items := map[string]string{}
	for rows.Next() {
		var hash, id string
		if err := rows.Scan(&hash, &id); err != nil {
			return nil, err
		}
		items[hash] = id
	}
	return items, databaseError("read account preparations", rows.Err())
}

func (s *Store) SaveAIOPreparation(ctx context.Context, service, account, hash, id string) error {
	if !model.IsDebridService(service) || account == "" || !sharedHashRE.MatchString(hash) || id == "" || len(id) > 128 {
		return model.ErrInvalid
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO ingest.aio_preparations(service,account,hash,provider_id,expires_at) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(service,account,hash) DO UPDATE SET provider_id=EXCLUDED.provider_id,expires_at=EXCLUDED.expires_at WHERE ingest.aio_preparations.provider_id IS DISTINCT FROM EXCLUDED.provider_id OR ingest.aio_preparations.expires_at<=now()`, service, account, hash, id, time.Now().Add(7*24*time.Hour))
	return databaseError("save prepared account torrent", err)
}

func (s *Store) ForgetAIOPreparation(ctx context.Context, service, account, hash, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ingest.aio_preparations WHERE service=$1 AND account=$2 AND hash=$3 AND provider_id=$4`, service, account, hash, id)
	return databaseError("forget unavailable account torrent", err)
}
