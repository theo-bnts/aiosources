package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *Store) PutSecret(ctx context.Context, name string, ciphertext []byte) error {
	if name == "" || len(ciphertext) == 0 {
		return model.ErrInvalid
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO ingest.secrets(name,ciphertext) VALUES($1,$2)
ON CONFLICT(name) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,updated_at=NOW(),revision=nextval('ingest.secret_revisions')`, name, ciphertext)
	return databaseError("store encrypted secret", err)
}

// Revision zero creates a new reference only. Positive revisions replace exactly
// the value the operator selected; a concurrent rotation cannot be overwritten.
func (s *Store) PutSecretAtRevision(ctx context.Context, name string, ciphertext []byte, revision int64) error {
	if name == "" || len(ciphertext) == 0 || revision < 0 {
		return model.ErrInvalid
	}
	statement := `INSERT INTO ingest.secrets(name,ciphertext) VALUES($1,$2) ON CONFLICT DO NOTHING`
	args := []any{name, ciphertext}
	if revision > 0 {
		statement = `UPDATE ingest.secrets SET ciphertext=$2,updated_at=NOW(),revision=nextval('ingest.secret_revisions') WHERE name=$1 AND revision=$3`
		args = append(args, revision)
	}
	result, err := s.pool.Exec(ctx, statement, args...)
	if err != nil {
		return databaseError("store encrypted secret at revision", err)
	}
	if result.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}

func (s *Store) Secret(ctx context.Context, name string) ([]byte, error) {
	var ciphertext []byte
	err := s.pool.QueryRow(ctx, "SELECT ciphertext FROM ingest.secrets WHERE name=$1", name).Scan(&ciphertext)
	return ciphertext, databaseError("read encrypted secret", err)
}

func (s *Store) ListSecrets(ctx context.Context) ([]model.SecretInfo, error) {
	rows, err := s.pool.Query(ctx, "SELECT name,revision,updated_at FROM ingest.secrets ORDER BY name")
	if err != nil {
		return nil, databaseError("list secret references", err)
	}
	defer rows.Close()
	secrets := []model.SecretInfo{}
	for rows.Next() {
		var secret model.SecretInfo
		if err := rows.Scan(&secret.Name, &secret.Revision, &secret.UpdatedAt); err != nil {
			return nil, databaseError("read secret reference", err)
		}
		secrets = append(secrets, secret)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("list secret references", err)
	}
	return secrets, nil
}

// HasEncryptedMaterial includes MFA factors, which use the same vault key as
// named secrets even when no provider credentials are configured.
func (s *Store) HasEncryptedMaterial(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.secrets)
OR EXISTS(SELECT 1 FROM ingest.admin_security WHERE active_secret IS NOT NULL OR pending_secret IS NOT NULL)`).Scan(&exists)
	return exists, databaseError("inspect encrypted material", err)
}

func lockProviderSecrets(ctx context.Context, tx pgx.Tx, provider model.Provider) error {
	names, err := provider.SecretReferences()
	if err != nil {
		return err
	}
	for _, name := range names {
		var locked string
		err := tx.QueryRow(ctx, "SELECT name FROM ingest.secrets WHERE name=$1 FOR KEY SHARE", name).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrInvalid
		}
		if err != nil {
			return databaseError("lock collection secret", err)
		}
	}
	return nil
}

func (s *Store) DeleteSecret(ctx context.Context, name string) error {
	return s.deleteSecret(ctx, name, nil)
}

func (s *Store) DeleteSecretAtRevision(ctx context.Context, name string, revision int64) error {
	if revision < 1 {
		return model.ErrInvalid
	}
	return s.deleteSecret(ctx, name, &revision)
}

func (s *Store) deleteSecret(ctx context.Context, name string, expected *int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin encrypted secret deletion", err)
	}
	defer rollback(tx)
	var revision int64
	if err := tx.QueryRow(ctx, "SELECT revision FROM ingest.secrets WHERE name=$1 FOR UPDATE", name).Scan(&revision); err != nil {
		return databaseError("lock encrypted secret", err)
	}
	if expected != nil && revision != *expected {
		return model.ErrConflict
	}
	// Admissions hold KEY SHARE until their immutable snapshot commits. This
	// separate statement sees admissions that committed while the lock waited.
	var referenced bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM ingest.runs WHERE status IN ('queued','running','paused') AND (
$1 IN (config#>>'{auth,secret_ref}',config#>>'{auth,username_ref}',config#>>'{auth,password_ref}')
OR EXISTS (SELECT 1 FROM jsonb_each_text(CASE WHEN jsonb_typeof(config#>'{http,secret_headers}')='object'
THEN config#>'{http,secret_headers}' ELSE '{}'::jsonb END) AS headers WHERE headers.value=$1)))
OR $1='aiosource_installation'
OR EXISTS(SELECT 1 FROM ingest.settings WHERE key='aiosource.config.v2' AND (
$1 IN (value::jsonb->>'tmdb_secret',value::jsonb->>'alldebrid_secret',value::jsonb->>'torbox_secret')
OR EXISTS(SELECT 1 FROM jsonb_each(COALESCE(value::jsonb->'trackers','{}'::jsonb)) t WHERE $1 IN (t.value->>'announce_secret',t.value->>'passkey_secret'))))`, name).Scan(&referenced); err != nil {
		return databaseError("check encrypted secret dependencies", err)
	}
	if referenced {
		return model.ErrConflict
	}
	if _, err := tx.Exec(ctx, "DELETE FROM ingest.secrets WHERE name=$1", name); err != nil {
		return databaseError("delete encrypted secret", err)
	}
	return databaseError("commit encrypted secret deletion", tx.Commit(ctx))
}

func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, "SELECT value FROM ingest.settings WHERE key=$1", key).Scan(&value)
	return value, databaseError("read application setting", err)
}

func (s *Store) SetSettingOnce(ctx context.Context, key, value string) (string, error) {
	if key == "" {
		return "", model.ErrInvalid
	}
	var established string
	// The harmless conflict update returns the winning value even when another
	// transaction establishes it after this statement's initial snapshot.
	err := s.pool.QueryRow(ctx, `INSERT INTO ingest.settings(key,value) VALUES($1,$2)
ON CONFLICT(key) DO UPDATE SET value=ingest.settings.value RETURNING value`, key, value).Scan(&established)
	return established, databaseError("establish application setting", err)
}
