package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
)

type securityFixture struct {
	ctx         context.Context
	databaseURL string
	db          *store.Store
	vault       *vault.Vault
	auth        *authentication
	handler     http.Handler
	requests    atomic.Int64
}

func newSecurityFixture(t *testing.T) *securityFixture {
	t.Helper()
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	auth, err := newAuthentication("synthetic-test-password", "https://admin.example.test", db)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{options: Options{Store: db, Vault: secrets}, auth: auth}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", auth.loginHandler)
	mux.HandleFunc("GET /api/session", auth.sessionHandler)
	s.registerSecurityRoutes(mux)
	return &securityFixture{ctx: ctx, databaseURL: endpoint, db: db, vault: secrets, auth: auth, handler: securityHeaders(mux)}
}

func (f *securityFixture) request(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "https://admin.example.test"+path, strings.NewReader(string(encoded))).WithContext(f.ctx)
	r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", f.requests.Add(1))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://admin.example.test")
	if cookie != nil {
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrfForToken(cookie.Value))
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func (f *securityFixture) login(t *testing.T, _ string) *http.Cookie {
	t.Helper()
	w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login status %d", w.Code)
	}
	return w.Result().Cookies()[0]
}

func TestAdministratorPasswordOnlyAndRetiredFactorRoutes(t *testing.T) {
	f := newSecurityFixture(t)
	current := f.login(t, "")
	if w := f.request(http.MethodGet, "/api/security/sessions", nil, current); w.Code != http.StatusOK {
		t.Fatal("password sign-in failed", w.Code)
	}
	for _, path := range []string{"/api/security/mfa/enroll", "/api/security/mfa/confirm", "/api/security/mfa/cancel", "/api/security/mfa/disable", "/api/security/mfa/recovery"} {
		if w := f.request(http.MethodPost, path, map[string]string{"password": "synthetic-test-password"}, current); w.Code != http.StatusNotFound {
			t.Fatal("retired factor route active", path, w.Code)
		}
	}
	if w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "incorrect-password"}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("incorrect password accepted")
	}
}

func TestPasswordChangeInvalidatesOldProcessesAndRollsBackOnLogFailure(t *testing.T) {
	f := newSecurityFixture(t)
	cookie := f.login(t, "")
	conn, err := pgx.Connect(f.ctx, f.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	// A durable activity failure must roll back verifier/session changes together.
	if _, err = conn.Exec(f.ctx, `ALTER TABLE aiosources.activity_logs ADD CONSTRAINT reject_fixture_activity CHECK (false)`); err != nil {
		t.Fatal(err)
	}
	if _, err = newAuthentication("changed-test-password", "https://admin.example.test", f.db); err == nil {
		t.Fatal("password changed without a durable log")
	}
	if w := f.request(http.MethodGet, "/api/security/sessions", nil, cookie); w.Code != http.StatusOK {
		t.Fatal("failed verifier transaction revoked session")
	}
	if _, err = conn.Exec(f.ctx, `ALTER TABLE aiosources.activity_logs DROP CONSTRAINT reject_fixture_activity`); err != nil {
		t.Fatal(err)
	}
	if _, err = newAuthentication("changed-test-password", "https://admin.example.test", f.db); err != nil {
		t.Fatal(err)
	}
	if w := f.request(http.MethodGet, "/api/security/sessions", nil, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("old password session survived rotation")
	}
	if w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("old process created a new session after password rotation")
	}
}
