package store

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/theo-bnts/aiosources/internal/model"
)

var ErrAuthentication = errors.New("authentication failed")
var ErrSecurityLimit = errors.New("security limit reached")

type SecuritySessionCreate struct {
	ID               string
	TokenHash        []byte
	UserAgent        string
	ExpiresAt        time.Time
	ReplaceTokenHash []byte
}

// InitializeAdminPassword keeps bcrypt's random salt stable across restarts.
// A real configured-password change invalidates all established sessions.
func (s *Store) InitializeAdminPassword(ctx context.Context, password string, candidate []byte) ([]byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, databaseError("initialize authentication", err)
	}
	defer tx.Rollback(ctx)
	var stored []byte
	if err = tx.QueryRow(ctx, `SELECT password_hash FROM aiosources.admin_security WHERE singleton FOR UPDATE`).Scan(&stored); err != nil {
		return nil, databaseError("read authentication verifier", err)
	}
	if stored == nil || bcrypt.CompareHashAndPassword(stored, []byte(password)) != nil {
		if _, err = tx.Exec(ctx, `UPDATE aiosources.admin_security SET password_hash=$1 WHERE singleton`, candidate); err != nil {
			return nil, databaseError("update authentication verifier", err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM aiosources.admin_sessions`); err != nil {
			return nil, databaseError("invalidate authentication sessions", err)
		}
		if stored != nil {
			if err = recordAccessActivityTx(ctx, tx, "security.sessions_revoked", "administrator"); err != nil {
				return nil, err
			}
		}
		stored = candidate
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, databaseError("initialize authentication", err)
	}
	return stored, nil
}

// CreateAdminSession serializes password changes and session creation. A stale
// application process cannot create a session after the password is replaced.
func (s *Store) CreateAdminSession(ctx context.Context, passwordHash []byte, next *SecuritySessionCreate) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin sign-in", err)
	}
	defer tx.Rollback(ctx)
	var configuredHash []byte
	if err = tx.QueryRow(ctx, `SELECT password_hash FROM aiosources.admin_security WHERE singleton FOR UPDATE`).Scan(&configuredHash); err != nil {
		return databaseError("read authentication verifier", err)
	}
	if string(configuredHash) != string(passwordHash) {
		return ErrAuthentication
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.admin_sessions WHERE expires_at<=now()`); err != nil {
		return databaseError("expire sessions", err)
	}
	if next != nil {
		if len(next.ReplaceTokenHash) == 32 {
			if _, err = tx.Exec(ctx, `DELETE FROM aiosources.admin_sessions WHERE token_hash=$1`, next.ReplaceTokenHash); err != nil {
				return databaseError("replace session", err)
			}
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM aiosources.admin_sessions`).Scan(&count); err != nil {
			return databaseError("count sessions", err)
		}
		var limit int
		if err := tx.QueryRow(ctx, `SELECT (values->>'session_limit')::int FROM aiosources.runtime_settings WHERE section='security'`).Scan(&limit); err != nil {
			return databaseError("read session limit", err)
		}
		if count >= limit {
			return ErrSecurityLimit
		}
		if _, err = tx.Exec(ctx, `INSERT INTO aiosources.admin_sessions(id,token_hash,user_agent,expires_at) VALUES($1,$2,$3,$4)`, next.ID, next.TokenHash, next.UserAgent, next.ExpiresAt); err != nil {
			return databaseError("create session", err)
		}
	}
	return databaseError("commit security operation", tx.Commit(ctx))
}

func (s *Store) AdminSession(ctx context.Context, tokenHash []byte, passwordHash []byte) (model.AdminSession, error) {
	var value model.AdminSession
	// Updating at minute granularity keeps an accurate bounded last-seen value
	// without writing a session row for every polling request.
	err := s.pool.QueryRow(ctx, `WITH valid AS (
 SELECT id FROM aiosources.admin_sessions WHERE token_hash=$1 AND expires_at>now()
 AND EXISTS(SELECT 1 FROM aiosources.admin_security WHERE singleton AND password_hash=$2)
), touched AS (
 UPDATE aiosources.admin_sessions SET last_seen_at=now() WHERE id IN(SELECT id FROM valid) AND last_seen_at<now()-interval '1 minute'
) SELECT id,user_agent,created_at,last_seen_at,expires_at FROM aiosources.admin_sessions WHERE id IN(SELECT id FROM valid)`, tokenHash, passwordHash).Scan(&value.ID, &value.UserAgent, &value.CreatedAt, &value.LastSeenAt, &value.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrAuthentication
	}
	return value, databaseError("read session", err)
}

func (s *Store) ListAdminSessions(ctx context.Context, currentID string) ([]model.AdminSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_agent,created_at,last_seen_at,expires_at,id=$1 FROM aiosources.admin_sessions WHERE expires_at>now() ORDER BY (id=$1) DESC,last_seen_at DESC,created_at DESC,id DESC`, currentID)
	if err != nil {
		return nil, databaseError("list sessions", err)
	}
	defer rows.Close()
	values := make([]model.AdminSession, 0)
	for rows.Next() {
		var value model.AdminSession
		if err = rows.Scan(&value.ID, &value.UserAgent, &value.CreatedAt, &value.LastSeenAt, &value.ExpiresAt, &value.Current); err != nil {
			return nil, databaseError("read sessions", err)
		}
		values = append(values, value)
	}
	return values, databaseError("list sessions", rows.Err())
}

func (s *Store) RevokeAdminSessions(ctx context.Context, currentID, targetID string, others bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin session revocation", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT singleton FROM aiosources.admin_security WHERE singleton FOR UPDATE`); err != nil {
		return databaseError("lock security state", err)
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.admin_sessions WHERE id=$1 AND expires_at>now())`, currentID).Scan(&valid); err != nil {
		return databaseError("verify session", err)
	}
	if !valid {
		return ErrAuthentication
	}
	action := "security.session_revoked"
	if others {
		action = "security.sessions_revoked"
		targetID = currentID
		_, err = tx.Exec(ctx, `DELETE FROM aiosources.admin_sessions WHERE id<>$1`, currentID)
	} else {
		var id string
		err = tx.QueryRow(ctx, `DELETE FROM aiosources.admin_sessions WHERE id=$1 RETURNING id`, targetID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrNotFound
		}
	}
	if err != nil {
		return databaseError("revoke sessions", err)
	}
	if err = recordAccessActivityTx(ctx, tx, action, targetID); err != nil {
		return err
	}
	return databaseError("commit session revocation", tx.Commit(ctx))
}

// AllowSecurityAttempt applies a durable per-client and global administrator
// limit across replicas. Client identifiers are domain-separated hashes, not raw addresses.
func (s *Store) AllowSecurityAttempt(ctx context.Context, clientHash string) (bool, error) {
	policy, err := s.RuntimeValues(ctx, "security")
	if err != nil {
		return false, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, databaseError("begin authentication limit", err)
	}
	defer tx.Rollback(ctx)
	allowed := true
	for _, bucket := range []struct {
		key   string
		limit int
	}{{"administrator", policy["login_global_per_minute"]}, {"client-" + clientHash, policy["login_per_minute"]}} {
		var count int
		err = tx.QueryRow(ctx, `INSERT INTO aiosources.security_attempts(bucket,window_start,attempts) VALUES($1,now(),1)
 ON CONFLICT(bucket) DO UPDATE SET window_start=CASE WHEN security_attempts.window_start<=now()-interval '1 minute' THEN now() ELSE security_attempts.window_start END,
 attempts=CASE WHEN security_attempts.window_start<=now()-interval '1 minute' THEN 1 ELSE LEAST(security_attempts.attempts+1,1000000) END RETURNING attempts`, bucket.key).Scan(&count)
		if err != nil {
			return false, databaseError("apply authentication limit", err)
		}
		if count > bucket.limit {
			allowed = false
			break
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.security_attempts WHERE window_start<now()-interval '10 minutes'`); err != nil {
		return false, databaseError("expire authentication limits", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, databaseError("commit authentication limit", err)
	}
	return allowed, nil
}

var securityTarget = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validAccessActivity(action, targetID string) bool {
	if !securityTarget.MatchString(targetID) {
		return false
	}
	switch action {
	case "security.trusted_user_changed", "security.trusted_user_deleted", "security.public_credentials_changed":
		return true
	case "security.public_catalog_enabled", "security.public_catalog_disabled", "security.public_catalog_permissions_changed", "security.public_stremio_enabled", "security.public_stremio_disabled", "security.public_search_enabled", "security.public_search_disabled", "security.session_revoked", "security.sessions_revoked", "security.public_torznab_enabled", "security.public_torznab_disabled", "security.public_access_enabled", "security.public_access_disabled", "share.created", "share.permissions_changed", "share.rotated", "share.revoked", "stremio.created", "stremio.permissions_changed", "stremio.rotated", "stremio.revoked", "torznab.created", "torznab.permissions_changed", "torznab.rotated", "torznab.revoked":
		return true
	default:
		return false
	}
}

func recordAccessActivityTx(ctx context.Context, tx pgx.Tx, kind, subjectID string) error {
	if !validAccessActivity(kind, subjectID) {
		return model.ErrInvalid
	}
	return recordActivityTx(ctx, tx, "info", kind, "", "", "", nil)
}
