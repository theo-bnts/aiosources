package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
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
	auth, err := newAuthentication("synthetic-test-password", "https://admin.example.test", db, secrets)
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

func (f *securityFixture) login(t *testing.T, code string) *http.Cookie {
	t.Helper()
	w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password", "code": code}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login status %d", w.Code)
	}
	return w.Result().Cookies()[0]
}

func testTOTP(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[offset:offset+4])&0x7fffffff)%1000000)
}

func TestTOTPProfileAndReplayBoundaries(t *testing.T) {
	secret := []byte("12345678901234567890")
	// RFC 6238 SHA-1 vectors, reduced to the configured six-digit profile.
	for _, value := range []struct {
		at   int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		step, ok := totpStep(secret, value.code, time.Unix(value.at, 0), -1)
		if !ok || step != value.at/30 {
			t.Fatal("RFC 6238 code rejected")
		}
		if _, ok = totpStep(secret, value.code, time.Unix(value.at, 0), step); ok {
			t.Fatal("consumed code accepted again")
		}
		if _, ok = totpStep(secret, value.code, time.Unix(value.at+90, 0), -1); ok {
			t.Fatal("code outside clock window accepted")
		}
	}
}

func TestMFAActivationRecoveryReplayAndDisable(t *testing.T) {
	f := newSecurityFixture(t)
	current := f.login(t, "")
	other := f.login(t, "")
	password := map[string]string{"password": "synthetic-test-password"}
	setup := f.request(http.MethodPost, "/api/security/mfa/enroll", password, current)
	var enrollment struct {
		Secret string `json:"secret"`
		URL    string `json:"otpauth_url"`
	}
	if setup.Code != http.StatusOK || json.Unmarshal(setup.Body.Bytes(), &enrollment) != nil {
		t.Fatalf("enrollment status %d", setup.Code)
	}
	account, err := url.Parse(enrollment.URL)
	if err != nil || account.Scheme != "otpauth" || account.Path != "/AIOSources:Administrator" || account.Query().Get("issuer") != "AIOSources" || account.Query().Get("secret") != enrollment.Secret {
		t.Fatal("authenticator enrollment does not describe the AIOSources account")
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	confirmCode := testTOTP(secret, time.Now().Unix()/30)
	confirm := f.request(http.MethodPost, "/api/security/mfa/confirm", map[string]string{"password": "synthetic-test-password", "code": confirmCode}, current)
	var issued struct {
		Codes []string `json:"recovery_codes"`
	}
	if confirm.Code != http.StatusOK || json.Unmarshal(confirm.Body.Bytes(), &issued) != nil || len(issued.Codes) != 10 {
		t.Fatalf("confirmation status %d", confirm.Code)
	}
	if w := f.request(http.MethodGet, "/api/security", nil, other); w.Code != http.StatusUnauthorized {
		t.Fatal("activation did not revoke prior sessions")
	}
	if w := f.request(http.MethodPost, "/api/login", password, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("password alone bypassed enabled MFA")
	}
	if w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password", "code": confirmCode}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("enrollment code was replayed to establish a session")
	}
	// Race two requests using one recovery code: only one may establish a session.
	var group sync.WaitGroup
	var successes atomic.Int32
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password", "code": issued.Codes[0]}, nil)
			if w.Code == http.StatusOK {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("one-time recovery code was not consumed atomically")
	}
	rotate := f.request(http.MethodPost, "/api/security/mfa/recovery", map[string]string{"password": "synthetic-test-password", "code": issued.Codes[1]}, current)
	var replacement struct {
		Codes []string `json:"recovery_codes"`
	}
	if rotate.Code != http.StatusOK || json.Unmarshal(rotate.Body.Bytes(), &replacement) != nil || len(replacement.Codes) != 10 {
		t.Fatalf("recovery rotation status %d", rotate.Code)
	}
	if w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password", "code": issued.Codes[2]}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("rotated recovery code remained usable")
	}
	if w := f.request(http.MethodPost, "/api/security/mfa/disable", password, current); w.Code != http.StatusForbidden {
		t.Fatal("disabled MFA without factor confirmation")
	}
	disable := f.request(http.MethodPost, "/api/security/mfa/disable", map[string]string{"password": "synthetic-test-password", "code": replacement.Codes[0]}, current)
	if disable.Code != http.StatusNoContent {
		t.Fatalf("disable status %d", disable.Code)
	}
	f.login(t, "")
	history, err := f.db.Logs(f.ctx, model.LogOptions{ListOptions: model.ListOptions{Query: "security.", Limit: 20}})
	if err != nil || len(history.Items) != 3 {
		t.Fatal("MFA state changes missing common log entries")
	}
	if w := f.request(http.MethodGet, "/api/security/audit", nil, current); w.Code != http.StatusNotFound {
		t.Fatal("retired audit API remains active")
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
	if _, err = conn.Exec(f.ctx, `ALTER TABLE ingest.activity_logs ADD CONSTRAINT reject_fixture_activity CHECK (false)`); err != nil {
		t.Fatal(err)
	}
	if _, err = newAuthentication("changed-test-password", "https://admin.example.test", f.db, f.vault); err == nil {
		t.Fatal("password changed without a durable log")
	}
	if w := f.request(http.MethodGet, "/api/security", nil, cookie); w.Code != http.StatusOK {
		t.Fatal("failed verifier transaction revoked session")
	}
	if _, err = conn.Exec(f.ctx, `ALTER TABLE ingest.activity_logs DROP CONSTRAINT reject_fixture_activity`); err != nil {
		t.Fatal(err)
	}
	if _, err = newAuthentication("changed-test-password", "https://admin.example.test", f.db, f.vault); err != nil {
		t.Fatal(err)
	}
	if w := f.request(http.MethodGet, "/api/security", nil, cookie); w.Code != http.StatusUnauthorized {
		t.Fatal("old password session survived rotation")
	}
	if w := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("old process created a new session after password rotation")
	}
}
