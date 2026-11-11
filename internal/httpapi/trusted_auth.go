package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"golang.org/x/crypto/argon2"
)

const trustedSessionCookie = "aiosources_trusted_session"
const trustedPasswordPrefix = "$argon2id$v=19$m=65536,t=3,p=1$"

// Fixed PHC parameters prevent corrupted storage from requesting unbounded
// memory. Password work also shares the bounded administrator admission queue.
func hashTrustedPassword(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len(password) > 1024 {
		return "", model.ErrInvalid
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	defer clear(key)
	return trustedPasswordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func checkTrustedPassword(encoded, password string) bool {
	validLength := utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12 && len(password) <= 1024
	if len(password) > 1024 {
		password = "invalid password"
	}
	parts := strings.Split(strings.TrimPrefix(encoded, trustedPasswordPrefix), "$")
	if !strings.HasPrefix(encoded, trustedPasswordPrefix) || len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	defer clear(actual)
	return subtle.ConstantTimeCompare(actual, expected) == 1 && validLength
}

type trustedSession struct {
	User    model.TrustedUser
	ID      string
	CSRF    string
	Expires time.Time
}
type trustedSessionKey struct{}

func (a *authentication) lookupTrusted(r *http.Request) (trustedSession, error) {
	var cookie *http.Cookie
	for _, candidate := range r.Cookies() {
		if candidate.Name == trustedSessionCookie {
			if cookie != nil {
				return trustedSession{}, store.ErrAuthentication
			}
			cookie = candidate
		}
	}
	if cookie == nil || len(cookie.Value) != 43 {
		return trustedSession{}, store.ErrAuthentication
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	user, id, expires, err := a.db.TrustedSession(r.Context(), hash[:])
	return trustedSession{User: user, ID: id, CSRF: csrfForToken(cookie.Value), Expires: expires}, err
}

func (a *authentication) trustedCookie(w http.ResponseWriter, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: trustedSessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}

func (a *authentication) protectTrusted(feature string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := a.lookupTrusted(r)
		if errors.Is(err, store.ErrAuthentication) {
			writeError(w, 401, "Sign in with your trusted user account")
			return
		}
		if err != nil {
			writeError(w, 503, "Authentication storage is unavailable")
			return
		}
		if feature != "" && !value.User.Allows(feature) {
			writeError(w, 403, "This feature is not enabled for your account")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrf := r.Header.Get("X-CSRF-Token")
			if !a.allowedOrigin(r) || len(csrf) != len(value.CSRF) || subtle.ConstantTimeCompare([]byte(csrf), []byte(value.CSRF)) != 1 {
				writeError(w, 403, "Invalid form session. Reload the page.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), trustedSessionKey{}, value)))
	}
}

func (s *server) trustedLogin(w http.ResponseWriter, r *http.Request) {
	a := s.auth
	if !a.allowedOrigin(r) {
		writeError(w, 403, "Request origin is not allowed")
		return
	}
	if !a.admitPasswordRequest(w) {
		return
	}
	defer func() { <-a.passwordRequests }()
	if !a.allowAttempt(r) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "Authentication is temporarily unavailable or rate limited. Try again in one minute.")
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordProofBytes)
	if !decodeJSON(w, r, &input) {
		return
	}
	email, _ := store.NormalizeTrustedEmail(input.Email)
	accountHash := sha256.Sum256([]byte("aiosources/trusted-login/v1\x00" + email))
	allowed, admissionErr := a.db.AllowTrustedAccountAttempt(r.Context(), hex.EncodeToString(accountHash[:]))
	if admissionErr != nil || !allowed {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "Authentication is temporarily unavailable or rate limited. Try again in one minute.")
		return
	}
	user, hash, err := a.db.TrustedLogin(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrAuthentication) {
		writeError(w, 503, "Authentication storage is unavailable")
		return
	}
	// Unknown and disabled accounts perform the same password work. The dummy
	// hash is instance-independent and cannot authenticate an account.
	if hash == "" {
		hash = trustedPasswordPrefix + "AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	}
	select {
	case a.passwordChecks <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	valid := checkTrustedPassword(hash, input.Password)
	<-a.passwordChecks
	input.Password = ""
	if !valid || err != nil {
		writeError(w, 401, "Incorrect email or password")
		return
	}
	token, err := randomToken()
	if err != nil {
		respondError(w, err)
		return
	}
	tokenHash := sha256.Sum256([]byte(token))
	hours, err := a.db.RuntimeValue(r.Context(), "security", "session_hours")
	if err != nil {
		respondError(w, err)
		return
	}
	_, expires, err := a.db.CreateTrustedSession(r.Context(), model.TrustedGrant{UserID: user.ID, Generation: user.Generation}, tokenHash[:], time.Duration(hours)*time.Hour)
	if err != nil {
		writeError(w, 401, "Sign-in could not be completed. Try again.")
		return
	}
	if old, e := a.lookupTrusted(r); e == nil {
		_ = a.db.RevokeTrustedSession(r.Context(), old.ID)
	}
	a.trustedCookie(w, token, expires, max(1, int(time.Until(expires).Seconds())))
	a.cookie(w, "", time.Unix(1, 0), -1)
	a.retirePreviousSessionCookie(w, r)
	writeJSON(w, 200, map[string]any{"authenticated": true, "role": "trusted", "csrf_token": csrfForToken(token), "user": user})
}

func (s *server) trustedLogout(w http.ResponseWriter, r *http.Request) {
	value := r.Context().Value(trustedSessionKey{}).(trustedSession)
	if err := s.options.Store.RevokeTrustedSession(r.Context(), value.ID); err != nil {
		respondError(w, err)
		return
	}
	s.auth.trustedCookie(w, "", time.Unix(1, 0), -1)
	w.WriteHeader(204)
}
