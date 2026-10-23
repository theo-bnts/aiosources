package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

const pendingMFAPurpose = "admin-mfa-pending-v1"
const activeMFAPurpose = "admin-mfa-active-v1"

func (s *server) registerSecurityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/security", s.auth.protect(s.securityStatus))
	mux.HandleFunc("GET /api/security/sessions", s.auth.protect(s.securitySessions))
	mux.HandleFunc("DELETE /api/security/sessions/{id}", s.auth.protect(s.securityRevokeSession))
	mux.HandleFunc("POST /api/security/sessions/revoke-others", s.auth.protect(s.securityRevokeOthers))
	mux.HandleFunc("POST /api/security/mfa/enroll", s.auth.protect(s.securityMFAEnroll))
	mux.HandleFunc("POST /api/security/mfa/confirm", s.auth.protect(s.securityMFAConfirm))
	mux.HandleFunc("POST /api/security/mfa/cancel", s.auth.protect(s.securityMFACancel))
	mux.HandleFunc("POST /api/security/mfa/disable", s.auth.protect(s.securityMFADisable))
	mux.HandleFunc("POST /api/security/mfa/recovery", s.auth.protect(s.securityMFARecovery))
}

func securityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrAuthentication):
		writeError(w, http.StatusForbidden, "Security verification failed. Check your password and use an unused authenticator or recovery code.")
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "Security settings changed or enrollment expired. Refresh and start again.")
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "Session not found")
	case errors.Is(err, model.ErrInvalid):
		writeError(w, http.StatusBadRequest, "Invalid security operation")
	default:
		writeError(w, http.StatusServiceUnavailable, "Security storage is unavailable. No change was confirmed.")
	}
}

func currentSession(r *http.Request) session { return r.Context().Value(sessionContextKey{}).(session) }

func (s *server) securityStatus(w http.ResponseWriter, r *http.Request) {
	value, err := s.options.Store.SecurityStatus(r.Context(), currentSession(r).id)
	if err != nil {
		securityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) securitySessions(w http.ResponseWriter, r *http.Request) {
	values, err := s.options.Store.ListAdminSessions(r.Context(), currentSession(r).id)
	if err != nil {
		securityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (s *server) securityRevokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 32 {
		writeError(w, http.StatusBadRequest, "Invalid session identifier")
		return
	}
	value := currentSession(r)
	if err := s.options.Store.RevokeAdminSessions(r.Context(), value.id, id, false); err != nil {
		securityError(w, err)
		return
	}
	if id == value.id {
		s.auth.cookie(w, "", time.Unix(1, 0), -1)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) securityRevokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := s.options.Store.RevokeAdminSessions(r.Context(), currentSession(r).id, "", true); err != nil {
		securityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type securityProof struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (s *server) securityProof(w http.ResponseWriter, r *http.Request) (securityProof, bool) {
	var input securityProof
	if !s.auth.admitPasswordRequest(w) {
		return input, false
	}
	defer func() { <-s.auth.passwordRequests }()
	if !s.auth.allowAttempt(r) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Security verification is temporarily unavailable or rate limited. Try again in one minute.")
		return input, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordProofBytes)
	if !decodeJSON(w, r, &input) {
		return input, false
	}
	if len(input.Code) > 64 || !s.auth.checkPassword(r, input.Password) {
		securityError(w, store.ErrAuthentication)
		return input, false
	}
	return input, true
}

func clearPending(state *store.SecurityState) {
	state.PendingSecret = nil
	state.PendingExpiresAt = nil
	state.PendingSessionID = nil
}

func (s *server) securityMFAEnroll(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.securityProof(w, r); !ok {
		return
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		securityError(w, err)
		return
	}
	defer clear(secret)
	encrypted, err := s.auth.vault.Seal(pendingMFAPurpose, secret)
	if err != nil {
		securityError(w, err)
		return
	}
	currentID := currentSession(r).id
	minutes, err := s.options.Store.RuntimeValue(r.Context(), "security", "enrollment_minutes")
	if err != nil {
		securityError(w, err)
		return
	}
	expires := time.Now().Add(time.Duration(minutes) * time.Minute)
	err = s.options.Store.SecurityUpdate(r.Context(), currentID, s.auth.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if len(state.ActiveSecret) > 0 {
			return store.SecurityChange{}, model.ErrConflict
		}
		state.PendingSecret = encrypted
		state.PendingExpiresAt = &expires
		state.PendingSessionID = &currentID
		return store.SecurityChange{}, nil
	})
	if err != nil {
		securityError(w, err)
		return
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	query := url.Values{"secret": {encoded}, "issuer": {"AIOSources"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	writeJSON(w, http.StatusOK, map[string]any{"secret": encoded, "otpauth_url": "otpauth://totp/AIOSources:Administrator?" + query.Encode(), "expires_at": expires})
}

func (s *server) securityMFACancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.securityProof(w, r); !ok {
		return
	}
	currentID := currentSession(r).id
	err := s.options.Store.SecurityUpdate(r.Context(), currentID, s.auth.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if state.PendingSessionID != nil && *state.PendingSessionID != currentID {
			return store.SecurityChange{}, model.ErrConflict
		}
		clearPending(state)
		return store.SecurityChange{}, nil
	})
	if err != nil {
		securityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) securityMFAConfirm(w http.ResponseWriter, r *http.Request) {
	input, ok := s.securityProof(w, r)
	if !ok {
		return
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		securityError(w, err)
		return
	}
	currentID := currentSession(r).id
	err = s.options.Store.SecurityUpdate(r.Context(), currentID, s.auth.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if len(state.ActiveSecret) > 0 || state.PendingExpiresAt == nil || !time.Now().Before(*state.PendingExpiresAt) || state.PendingSessionID == nil || *state.PendingSessionID != currentID {
			return store.SecurityChange{}, model.ErrConflict
		}
		secret, err := s.auth.vault.Open(pendingMFAPurpose, state.PendingSecret)
		if err != nil {
			return store.SecurityChange{}, err
		}
		defer clear(secret)
		step, ok := totpStep(secret, input.Code, time.Now(), -1)
		if !ok {
			return store.SecurityChange{}, store.ErrAuthentication
		}
		encrypted, err := s.auth.vault.Seal(activeMFAPurpose, secret)
		if err != nil {
			return store.SecurityChange{}, err
		}
		state.ActiveSecret = encrypted
		state.LastStep = step
		state.RecoveryHashes = hashes
		clearPending(state)
		return store.SecurityChange{RevokeOthers: true, ActivityKind: "security.mfa_enabled", ActivitySubject: currentID}, nil
	})
	if err != nil {
		securityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *server) securityMFADisable(w http.ResponseWriter, r *http.Request) {
	input, ok := s.securityProof(w, r)
	if !ok {
		return
	}
	currentID := currentSession(r).id
	err := s.options.Store.SecurityUpdate(r.Context(), currentID, s.auth.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if len(state.ActiveSecret) == 0 {
			return store.SecurityChange{}, model.ErrConflict
		}
		if err := s.auth.consumeFactor(state, input.Code); err != nil {
			return store.SecurityChange{}, err
		}
		state.ActiveSecret = nil
		state.LastStep = -1
		state.RecoveryHashes = []string{}
		clearPending(state)
		return store.SecurityChange{RevokeOthers: true, ActivityKind: "security.mfa_disabled", ActivitySubject: currentID}, nil
	})
	if err != nil {
		securityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) securityMFARecovery(w http.ResponseWriter, r *http.Request) {
	input, ok := s.securityProof(w, r)
	if !ok {
		return
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		securityError(w, err)
		return
	}
	currentID := currentSession(r).id
	err = s.options.Store.SecurityUpdate(r.Context(), currentID, s.auth.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if len(state.ActiveSecret) == 0 {
			return store.SecurityChange{}, model.ErrConflict
		}
		if err := s.auth.consumeFactor(state, input.Code); err != nil {
			return store.SecurityChange{}, err
		}
		state.RecoveryHashes = hashes
		clearPending(state)
		return store.SecurityChange{RevokeOthers: true, ActivityKind: "security.recovery_rotated", ActivitySubject: currentID}, nil
	})
	if err != nil {
		securityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func recoveryHash(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), "-", ""), " ", ""))
	hash := sha256.Sum256([]byte("scraper/admin-recovery/v1\x00" + code))
	return hex.EncodeToString(hash[:])
}

func newRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, nil, err
		}
		text := strings.ToUpper(hex.EncodeToString(raw[:]))
		codes[i] = text[:8] + "-" + text[8:16] + "-" + text[16:24] + "-" + text[24:]
		hashes[i] = recoveryHash(codes[i])
	}
	return codes, hashes, nil
}

func (a *authentication) consumeFactor(state *store.SecurityState, code string) error {
	if len(code) == 0 || len(code) > 64 {
		return store.ErrAuthentication
	}
	hash := recoveryHash(code)
	for i, stored := range state.RecoveryHashes {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(stored)) == 1 {
			state.RecoveryHashes = append(state.RecoveryHashes[:i], state.RecoveryHashes[i+1:]...)
			return nil
		}
	}
	secret, err := a.vault.Open(activeMFAPurpose, state.ActiveSecret)
	if err != nil {
		return err
	}
	defer clear(secret)
	step, ok := totpStep(secret, code, time.Now(), state.LastStep)
	if !ok {
		return store.ErrAuthentication
	}
	state.LastStep = step
	return nil
}

// totpStep implements RFC 6238's six-digit SHA-1 profile with one step of clock
// drift. The committed high-water mark prevents reuse even across replicas.
func totpStep(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	current := now.Unix() / 30
	for _, step := range []int64{current, current - 1, current + 1} {
		if step <= lastStep || step < 0 {
			continue
		}
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], uint64(step))
		mac := hmac.New(sha1.New, secret)
		_, _ = mac.Write(counter[:])
		digest := mac.Sum(nil)
		offset := digest[len(digest)-1] & 15
		number := (binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff) % 1000000
		expected := fmt.Sprintf("%06d", number)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
