package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/theo-bnts/aiosources/internal/model"
)

const trustedUserColumns = "id,email,enabled,permissions,revision,generation,created_at,updated_at"

var ErrTrustedEmailConflict = errors.New("trusted account email already exists")

func scanTrustedUser(row scanner) (model.TrustedUser, error) {
	var value model.TrustedUser
	var permissions []byte
	err := row.Scan(&value.ID, &value.Email, &value.Enabled, &permissions, &value.Revision, &value.Generation, &value.CreatedAt, &value.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(permissions, &value.TrustedPermissions)
	}
	return value, err
}

func NormalizeTrustedEmail(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || address.Name != "" || len(value) > 254 || !strings.Contains(value, "@") {
		return "", model.ErrInvalid
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return "", model.ErrInvalid
		}
	}
	return value, nil
}

// Account throttling complements the existing per-client/global admission,
// without charging the same login twice against the global CPU protection.
func (s *Store) AllowTrustedAccountAttempt(ctx context.Context, accountHash string) (bool, error) {
	limit, err := s.RuntimeValue(ctx, "security", "login_per_minute")
	if err != nil {
		return false, err
	}
	var attempts int
	err = s.pool.QueryRow(ctx, `INSERT INTO aiosources.security_attempts(bucket,window_start,attempts) VALUES($1,now(),1) ON CONFLICT(bucket) DO UPDATE SET window_start=CASE WHEN security_attempts.window_start<=now()-interval '1 minute' THEN now() ELSE security_attempts.window_start END,attempts=CASE WHEN security_attempts.window_start<=now()-interval '1 minute' THEN 1 ELSE LEAST(security_attempts.attempts+1,1000000) END RETURNING attempts`, "trusted-account-"+accountHash).Scan(&attempts)
	return attempts <= limit, databaseError("limit trusted account authentication", err)
}

func (s *Store) TrustedUsers(ctx context.Context) ([]model.TrustedUser, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+trustedUserColumns+" FROM aiosources.trusted_users ORDER BY email,id")
	if err != nil {
		return nil, databaseError("list trusted users", err)
	}
	defer rows.Close()
	items := []model.TrustedUser{}
	for rows.Next() {
		value, err := scanTrustedUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, databaseError("list trusted users", rows.Err())
}

func (s *Store) TrustedUser(ctx context.Context, grant model.TrustedGrant) (model.TrustedUser, error) {
	value, err := scanTrustedUser(s.pool.QueryRow(ctx, "SELECT "+trustedUserColumns+" FROM aiosources.trusted_users WHERE id=$1 AND generation=$2 AND enabled", grant.UserID, grant.Generation))
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrAuthentication
	}
	return value, databaseError("authorize trusted user", err)
}

func (s *Store) TrustedLogin(ctx context.Context, email string) (model.TrustedUser, string, error) {
	var id, passwordHash string
	var generation int64
	err := s.pool.QueryRow(ctx, `SELECT id,generation,password_hash FROM aiosources.trusted_users WHERE email=$1 AND enabled`, email).Scan(&id, &generation, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.TrustedUser{}, "", ErrAuthentication
	}
	if err != nil {
		return model.TrustedUser{}, "", databaseError("read trusted login", err)
	}
	value, err := s.TrustedUser(ctx, model.TrustedGrant{UserID: id, Generation: generation})
	return value, passwordHash, err
}

func (s *Store) SaveTrustedUser(ctx context.Context, value model.TrustedUser, passwordHash string) (model.TrustedUser, error) {
	email, err := NormalizeTrustedEmail(value.Email)
	if err != nil || value.Revision < 0 {
		return model.TrustedUser{}, model.ErrInvalid
	}
	value.Email = email
	permissions, _ := json.Marshal(value.TrustedPermissions)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.TrustedUser{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return model.TrustedUser{}, databaseError("lock trusted catalog policy", err)
	}
	if value.ID == "" {
		if value.Revision != 0 || passwordHash == "" {
			return model.TrustedUser{}, model.ErrInvalid
		}
		value.ID, err = newRunID()
		if err != nil {
			return model.TrustedUser{}, err
		}
		value, err = scanTrustedUser(tx.QueryRow(ctx, `INSERT INTO aiosources.trusted_users(id,email,password_hash,enabled,permissions) VALUES($1,$2,$3,$4,$5) RETURNING `+trustedUserColumns, value.ID, value.Email, passwordHash, value.Enabled, permissions))
	} else {
		value, err = scanTrustedUser(tx.QueryRow(ctx, `UPDATE aiosources.trusted_users SET email=$2,password_hash=CASE WHEN $3='' THEN password_hash ELSE $3 END,enabled=$4,permissions=$5,revision=revision+1,generation=generation+1,updated_at=now() WHERE id=$1 AND revision=$6 RETURNING `+trustedUserColumns, value.ID, value.Email, passwordHash, value.Enabled, permissions, value.Revision))
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return model.TrustedUser{}, ErrTrustedEmailConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return model.TrustedUser{}, model.ErrConflict
	}
	if err != nil {
		return model.TrustedUser{}, databaseError("save trusted user", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.trusted_sessions WHERE user_id=$1`, value.ID); err != nil {
		return model.TrustedUser{}, err
	}
	if err = recordAccessActivityTx(ctx, tx, "security.trusted_user_changed", value.ID); err != nil {
		return model.TrustedUser{}, err
	}
	return value, databaseError("commit trusted user", tx.Commit(ctx))
}

func (s *Store) DeleteTrustedUser(ctx context.Context, id string, revision int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1768843109,3); SELECT pg_advisory_xact_lock(1768843109,9)`); err != nil {
		return databaseError("lock trusted catalog policy", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM aiosources.trusted_users WHERE id=$1 AND revision=$2`, id, revision)
	if err != nil {
		return databaseError("delete trusted user", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	if err = recordAccessActivityTx(ctx, tx, "security.trusted_user_deleted", id); err != nil {
		return err
	}
	return databaseError("commit trusted user deletion", tx.Commit(ctx))
}

func (s *Store) CreateTrustedSession(ctx context.Context, grant model.TrustedGrant, tokenHash []byte, lifetime time.Duration) (string, time.Time, error) {
	if len(tokenHash) != 32 || lifetime <= 0 || lifetime > 365*24*time.Hour {
		return "", time.Time{}, model.ErrInvalid
	}
	id, err := newRunID()
	if err != nil {
		return "", time.Time{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	defer rollback(tx)
	// Hold the account while creating the session so a concurrent password
	// replacement cannot create a session under an obsolete generation.
	var valid bool
	err = tx.QueryRow(ctx, `SELECT enabled FROM aiosources.trusted_users WHERE id=$1 AND generation=$2 FOR SHARE`, grant.UserID, grant.Generation).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !valid) {
		return "", time.Time{}, ErrAuthentication
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.trusted_sessions WHERE expires_at<=now() OR (user_id=$1 AND id IN (SELECT id FROM aiosources.trusted_sessions WHERE user_id=$1 ORDER BY created_at DESC OFFSET 19))`, grant.UserID); err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(lifetime)
	_, err = tx.Exec(ctx, `INSERT INTO aiosources.trusted_sessions(id,token_hash,user_id,generation,expires_at) VALUES($1,$2,$3,$4,$5)`, id, tokenHash, grant.UserID, grant.Generation, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	return id, expires, databaseError("commit trusted session", tx.Commit(ctx))
}

func (s *Store) TrustedSession(ctx context.Context, tokenHash []byte) (model.TrustedUser, string, time.Time, error) {
	var id, userID string
	var generation int64
	var expires time.Time
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,generation,expires_at FROM aiosources.trusted_sessions WHERE token_hash=$1 AND expires_at>now()`, tokenHash).Scan(&id, &userID, &generation, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.TrustedUser{}, "", expires, ErrAuthentication
	}
	if err != nil {
		return model.TrustedUser{}, "", expires, databaseError("read trusted session", err)
	}
	user, err := s.TrustedUser(ctx, model.TrustedGrant{UserID: userID, Generation: generation})
	return user, id, expires, err
}

func (s *Store) RevokeTrustedSession(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM aiosources.trusted_sessions WHERE id=$1`, id)
	return databaseError("revoke trusted session", err)
}
