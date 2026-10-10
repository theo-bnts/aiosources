package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
)

const sessionCookie = "ingest_session"
const sessionLifetime = 12 * time.Hour

type session struct {
	id      string
	csrf    string
	expires time.Time
}

type sessionContextKey struct{}

type loginWindow struct {
	attempts int
	until    time.Time
}

type authentication struct {
	mu             sync.Mutex
	passwordHash   []byte
	origin         string
	secure         bool
	db             *store.Store
	vault          *vault.Vault
	attempts       map[string]loginWindow
	passwordChecks chan struct{}
}

func newAuthentication(password, publicURL string, db *store.Store, secretVault *vault.Vault) (*authentication, error) {
	if len(password) < 12 || len(password) > 72 {
		return nil, errors.New("ADMIN_PASSWORD must contain 12 to 72 bytes")
	}
	if db == nil || secretVault == nil {
		return nil, errors.New("persistent authentication dependencies are required")
	}
	origin := ""
	secure := false
	if publicURL != "" {
		u, err := url.Parse(publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("PUBLIC_URL must be an HTTP(S) origin without credentials, query or path")
		}
		origin = u.Scheme + "://" + u.Host
		secure = u.Scheme == "https"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("could not initialize administrator authentication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hash, err = db.InitializeAdminPassword(ctx, password, hash)
	if err != nil {
		return nil, errors.New("could not initialize persistent administrator authentication")
	}
	return &authentication{passwordHash: hash, origin: origin, secure: secure, db: db, vault: secretVault, attempts: make(map[string]loginWindow), passwordChecks: make(chan struct{}, 2)}, nil
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func csrfForToken(token string) string {
	sum := sha256.Sum256([]byte("scraper/session-csrf/v1\x00" + token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (a *authentication) lookup(r *http.Request) (session, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 43 {
		return session{}, store.ErrAuthentication
	}
	key := sha256.Sum256([]byte(cookie.Value))
	value, err := a.db.AdminSession(r.Context(), key[:], a.passwordHash)
	if err != nil {
		return session{}, err
	}
	return session{id: value.ID, csrf: csrfForToken(cookie.Value), expires: value.ExpiresAt}, nil
}

// current deliberately performs a fresh durable check for long-lived SSE streams.
func (a *authentication) current(r *http.Request) (session, bool) {
	value, err := a.lookup(r)
	return value, err == nil
}

func (a *authentication) allowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	expected := a.origin
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	return origin == expected
}

func (a *authentication) cookie(w http.ResponseWriter, value string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}

func (a *authentication) sessionHandler(w http.ResponseWriter, r *http.Request) {
	value, err := a.lookup(r)
	if errors.Is(err, store.ErrAuthentication) {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Authentication storage is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf_token": value.csrf})
}

func (a *authentication) allowAttempt(r *http.Request) bool {
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		address = r.RemoteAddr
	}
	key := sha256.Sum256([]byte("scraper/login-client/v1\x00" + address))
	client := hex.EncodeToString(key[:])
	now := time.Now()
	a.mu.Lock()
	for key, window := range a.attempts {
		if !now.Before(window.until) {
			delete(a.attempts, key)
		}
	}
	window, exists := a.attempts[client]
	if !exists {
		if len(a.attempts) >= 4096 {
			a.mu.Unlock()
			return false
		}
		window.until = now.Add(time.Minute)
	}
	if window.attempts >= 10 {
		a.mu.Unlock()
		return false
	}
	window.attempts++
	a.attempts[client] = window
	a.mu.Unlock()
	allowed, err := a.db.AllowSecurityAttempt(r.Context(), client)
	return err == nil && allowed
}

func (a *authentication) checkPassword(r *http.Request, password string) bool {
	if len(password) > 72 || len(password) < 12 {
		return false
	}
	select {
	case a.passwordChecks <- struct{}{}:
		defer func() { <-a.passwordChecks }()
	case <-r.Context().Done():
		return false
	}
	return bcrypt.CompareHashAndPassword(a.passwordHash, []byte(password)) == nil
}

func (a *authentication) loginHandler(w http.ResponseWriter, r *http.Request) {
	if !a.allowedOrigin(r) {
		writeError(w, http.StatusForbidden, "Request origin is not allowed")
		return
	}
	if !a.allowAttempt(r) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Authentication is temporarily unavailable or rate limited. Try again in one minute.")
		return
	}
	var input struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !a.checkPassword(r, input.Password) || len(input.Code) > 64 {
		writeError(w, http.StatusUnauthorized, "Incorrect password or verification code")
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the session")
		return
	}
	var rawID [16]byte
	if _, err = rand.Read(rawID[:]); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the session")
		return
	}
	hash := sha256.Sum256([]byte(token))
	next := &store.SecuritySessionCreate{ID: hex.EncodeToString(rawID[:]), TokenHash: hash[:], UserAgent: safeUserAgent(r.UserAgent()), ExpiresAt: time.Now().Add(sessionLifetime)}
	if old, err := r.Cookie(sessionCookie); err == nil && len(old.Value) == 43 {
		oldHash := sha256.Sum256([]byte(old.Value))
		next.ReplaceTokenHash = oldHash[:]
	}
	err = a.db.SecurityUpdate(r.Context(), "", a.passwordHash, func(state *store.SecurityState) (store.SecurityChange, error) {
		if len(state.ActiveSecret) > 0 {
			if err := a.consumeFactor(state, input.Code); err != nil {
				return store.SecurityChange{}, err
			}
		}
		return store.SecurityChange{NewSession: next}, nil
	})
	if errors.Is(err, store.ErrAuthentication) {
		writeError(w, http.StatusUnauthorized, "Incorrect password or verification code")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Could not create the session. Check service availability and retry.")
		return
	}
	a.cookie(w, token, next.ExpiresAt, int(sessionLifetime.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf_token": csrfForToken(token)})
}

func safeUserAgent(raw string) string {
	// Never persist arbitrary user-agent text, which can contain names, URLs or
	// credentials. Deliberately retain only a coarse browser and OS family.
	browser := "Other browser"
	switch {
	case strings.Contains(raw, "Edg/"):
		browser = "Edge"
	case strings.Contains(raw, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(raw, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(raw, "Safari/"):
		browser = "Safari"
	}
	os := "Unknown OS"
	switch {
	case strings.Contains(raw, "Android"):
		os = "Android"
	case strings.Contains(raw, "iPhone") || strings.Contains(raw, "iPad"):
		os = "iOS"
	case strings.Contains(raw, "Windows"):
		os = "Windows"
	case strings.Contains(raw, "Macintosh"):
		os = "macOS"
	case strings.Contains(raw, "Linux"):
		os = "Linux"
	}
	return browser + " / " + os
}

func (a *authentication) logoutHandler(w http.ResponseWriter, r *http.Request) {
	value := r.Context().Value(sessionContextKey{}).(session)
	if err := a.db.RevokeAdminSessions(r.Context(), value.id, value.id, false); err != nil {
		securityError(w, err)
		return
	}
	a.cookie(w, "", time.Unix(1, 0), -1)
	w.WriteHeader(http.StatusNoContent)
}

func (a *authentication) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := a.lookup(r)
		if errors.Is(err, store.ErrAuthentication) {
			writeError(w, http.StatusUnauthorized, "Authentication required")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "Authentication storage is unavailable")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrf := r.Header.Get("X-CSRF-Token")
			if !a.allowedOrigin(r) || len(csrf) != len(value.csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(value.csrf)) != 1 {
				writeError(w, http.StatusForbidden, "Invalid form session. Reload the page.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, value)))
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
