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

// SecurityState contains only ciphertext, one-way hashes and nonsecret state.
// SecurityUpdate holds the singleton row lock through verification and commit,
// so factor replay, session creation, revocation and activity logs are atomic.
type SecurityState struct {
	ActiveSecret     []byte
	PendingSecret    []byte
	PendingExpiresAt *time.Time
	PendingSessionID *string
	LastStep         int64
	RecoveryHashes   []string
}

type SecuritySessionCreate struct {
	ID               string
	TokenHash        []byte
	UserAgent        string
	ExpiresAt        time.Time
	ReplaceTokenHash []byte
}

type SecurityChange struct {
	NewSession      *SecuritySessionCreate
	RevokeOthers    bool
	ActivityKind    string
	ActivitySubject string
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
	if err = tx.QueryRow(ctx, `SELECT password_hash FROM ingest.admin_security WHERE singleton FOR UPDATE`).Scan(&stored); err != nil {
		return nil, databaseError("read authentication verifier", err)
	}
	if stored == nil || bcrypt.CompareHashAndPassword(stored, []byte(password)) != nil {
		if _, err = tx.Exec(ctx, `UPDATE ingest.admin_security SET password_hash=$1,pending_secret=NULL,pending_expires_at=NULL,pending_session_id=NULL WHERE singleton`, candidate); err != nil {
			return nil, databaseError("update authentication verifier", err)
		}
		if _, err = tx.Exec(ctx, `DELETE FROM ingest.admin_sessions`); err != nil {
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

func (s *Store) SecurityUpdate(ctx context.Context, currentID string, passwordHash []byte, apply func(*SecurityState) (SecurityChange, error)) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin security operation", err)
	}
	defer tx.Rollback(ctx)
	var state SecurityState
	var configuredHash []byte
	err = tx.QueryRow(ctx, `SELECT active_secret,pending_secret,pending_expires_at,pending_session_id,last_step,recovery_hashes,password_hash FROM ingest.admin_security WHERE singleton FOR UPDATE`).Scan(&state.ActiveSecret, &state.PendingSecret, &state.PendingExpiresAt, &state.PendingSessionID, &state.LastStep, &state.RecoveryHashes, &configuredHash)
	if err != nil {
		return databaseError("read security state", err)
	}
	if string(configuredHash) != string(passwordHash) {
		return ErrAuthentication
	}
	if currentID != "" {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.admin_sessions WHERE id=$1 AND expires_at>now())`, currentID).Scan(&valid); err != nil {
			return databaseError("verify current session", err)
		}
		if !valid {
			return ErrAuthentication
		}
	}
	change, err := apply(&state)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE ingest.admin_security SET active_secret=$1,pending_secret=$2,pending_expires_at=$3,pending_session_id=$4,last_step=$5,recovery_hashes=$6 WHERE singleton`, state.ActiveSecret, state.PendingSecret, state.PendingExpiresAt, state.PendingSessionID, state.LastStep, state.RecoveryHashes)
	if err != nil {
		return databaseError("save security state", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.admin_sessions WHERE expires_at<=now()`); err != nil {
		return databaseError("expire sessions", err)
	}
	if change.RevokeOthers {
		if currentID == "" {
			return model.ErrInvalid
		}
		if _, err = tx.Exec(ctx, `DELETE FROM ingest.admin_sessions WHERE id<>$1`, currentID); err != nil {
			return databaseError("revoke sessions", err)
		}
	}
	if next := change.NewSession; next != nil {
		if len(next.ReplaceTokenHash) == 32 {
			if _, err = tx.Exec(ctx, `DELETE FROM ingest.admin_sessions WHERE token_hash=$1`, next.ReplaceTokenHash); err != nil {
				return databaseError("replace session", err)
			}
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM ingest.admin_sessions`).Scan(&count); err != nil {
			return databaseError("count sessions", err)
		}
		if count >= 256 {
			return ErrSecurityLimit
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.admin_sessions(id,token_hash,user_agent,expires_at) VALUES($1,$2,$3,$4)`, next.ID, next.TokenHash, next.UserAgent, next.ExpiresAt); err != nil {
			return databaseError("create session", err)
		}
	}
	if change.ActivityKind != "" {
		if err = recordAccessActivityTx(ctx, tx, change.ActivityKind, change.ActivitySubject); err != nil {
			return err
		}
	}
	return databaseError("commit security operation", tx.Commit(ctx))
}

func (s *Store) AdminSession(ctx context.Context, tokenHash []byte, passwordHash []byte) (model.AdminSession, error) {
	var value model.AdminSession
	// Updating at minute granularity keeps an accurate bounded last-seen value
	// without writing a session row for every polling request.
	err := s.pool.QueryRow(ctx, `WITH valid AS (
 SELECT id FROM ingest.admin_sessions WHERE token_hash=$1 AND expires_at>now()
 AND EXISTS(SELECT 1 FROM ingest.admin_security WHERE singleton AND password_hash=$2)
), touched AS (
 UPDATE ingest.admin_sessions SET last_seen_at=now() WHERE id IN(SELECT id FROM valid) AND last_seen_at<now()-interval '1 minute'
) SELECT id,user_agent,created_at,last_seen_at,expires_at FROM ingest.admin_sessions WHERE id IN(SELECT id FROM valid)`, tokenHash, passwordHash).Scan(&value.ID, &value.UserAgent, &value.CreatedAt, &value.LastSeenAt, &value.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrAuthentication
	}
	return value, databaseError("read session", err)
}

func (s *Store) SecurityStatus(ctx context.Context, currentID string) (model.SecurityStatus, error) {
	var value model.SecurityStatus
	err := s.pool.QueryRow(ctx, `SELECT active_secret IS NOT NULL,cardinality(recovery_hashes),CASE WHEN pending_session_id=$1 AND pending_expires_at>now() THEN pending_expires_at END FROM ingest.admin_security WHERE singleton`, currentID).Scan(&value.MFAEnabled, &value.RecoveryCodesRemaining, &value.PendingExpiresAt)
	return value, databaseError("read security status", err)
}

func (s *Store) ListAdminSessions(ctx context.Context, currentID string) ([]model.AdminSession, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_agent,created_at,last_seen_at,expires_at,id=$1 FROM ingest.admin_sessions WHERE expires_at>now() ORDER BY (id=$1) DESC,last_seen_at DESC,created_at DESC,id DESC`, currentID)
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
	if _, err = tx.Exec(ctx, `SELECT singleton FROM ingest.admin_security WHERE singleton FOR UPDATE`); err != nil {
		return databaseError("lock security state", err)
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.admin_sessions WHERE id=$1 AND expires_at>now())`, currentID).Scan(&valid); err != nil {
		return databaseError("verify session", err)
	}
	if !valid {
		return ErrAuthentication
	}
	action := "security.session_revoked"
	if others {
		action = "security.sessions_revoked"
		targetID = currentID
		_, err = tx.Exec(ctx, `DELETE FROM ingest.admin_sessions WHERE id<>$1`, currentID)
	} else {
		var id string
		err = tx.QueryRow(ctx, `DELETE FROM ingest.admin_sessions WHERE id=$1 RETURNING id`, targetID).Scan(&id)
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, databaseError("begin authentication limit", err)
	}
	defer tx.Rollback(ctx)
	allowed := true
	for _, bucket := range []struct {
		key   string
		limit int
	}{{"administrator", 60}, {"client-" + clientHash, 10}} {
		var count int
		err = tx.QueryRow(ctx, `INSERT INTO ingest.security_attempts(bucket,window_start,attempts) VALUES($1,now(),1)
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
	if _, err = tx.Exec(ctx, `DELETE FROM ingest.security_attempts WHERE window_start<now()-interval '10 minutes'`); err != nil {
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
	case "security.mfa_enabled", "security.mfa_disabled", "security.recovery_rotated", "security.session_revoked", "security.sessions_revoked", "security.public_torznab_enabled", "security.public_torznab_disabled", "security.public_access_enabled", "security.public_access_disabled", "security.public_source_downloads_enabled", "security.public_source_downloads_disabled", "share.created", "share.permissions_changed", "share.rotated", "share.revoked", "stremio.created", "stremio.permissions_changed", "stremio.rotated", "stremio.revoked", "torznab.created", "torznab.permissions_changed", "torznab.rotated", "torznab.revoked":
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
