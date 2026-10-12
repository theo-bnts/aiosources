package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type heldAuthenticationBody struct {
	reader  *strings.Reader
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *heldAuthenticationBody) Read(p []byte) (int, error) {
	b.once.Do(func() { b.started <- struct{}{} })
	<-b.release
	return b.reader.Read(p)
}

func (*heldAuthenticationBody) Close() error { return nil }

func TestLoginRejectsExcessRequestsBeforeReadingTheirBodies(t *testing.T) {
	f := newSecurityFixture(t)
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	responses := make(chan int, 8)
	var releasing sync.Once
	defer func() {
		releasing.Do(func() { close(release) })
		for range 8 {
			if status := <-responses; status != http.StatusOK {
				t.Errorf("admitted login failed after its body was released: %d", status)
			}
		}
	}()
	for index := range 8 {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test/api/login", nil).WithContext(f.ctx)
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", index+10)
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.Body = &heldAuthenticationBody{reader: strings.NewReader(`{"password":"synthetic-test-password"}`), started: started, release: release}
		go func() {
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			responses <- w.Code
		}()
	}
	deadline, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	for range 8 {
		select {
		case <-started:
		case <-deadline.Done():
			t.Fatal("login requests did not reach their body-read boundary")
		}
	}
	excess := f.request(http.MethodPost, "/api/login", map[string]string{"password": "synthetic-test-password"}, nil)
	if excess.Code != http.StatusTooManyRequests || excess.Header().Get("Retry-After") == "" {
		t.Fatalf("additional unauthenticated work was accepted while eight bodies were held: %d", excess.Code)
	}
	releasing.Do(func() { close(release) })
}

func TestPasswordProofBodiesHaveASeparateSmallLimit(t *testing.T) {
	f := newSecurityFixture(t)
	cookie := f.login(t, "")
	for _, path := range []string{"/api/login", "/api/security/mfa/enroll"} {
		r := httptest.NewRequest(http.MethodPost, "https://admin.example.test"+path, strings.NewReader(strings.Repeat(" ", 8192)+`{"password":"synthetic-test-password"}`)).WithContext(f.ctx)
		r.RemoteAddr = "192.0.2.2:1234"
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrfForToken(cookie.Value))
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "synthetic-test-password") {
			t.Fatalf("oversized password proof was accepted or reflected at %s: %d", path, w.Code)
		}
	}
}

func TestDirectHTTPDeploymentIgnoresForwardedIdentityAndKeepsMutationProtection(t *testing.T) {
	fixture := newSecurityFixture(t)
	auth, err := newAuthentication("synthetic-test-password", "", fixture.db, fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	request := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://admin.example.test/api/login", strings.NewReader(`{"password":"synthetic-test-password"}`))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Forwarded", "for=198.51.100.1;proto=https;host=untrusted.example.test")
		r.Header.Set("X-Forwarded-Host", "untrusted.example.test")
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}
	foreign := httptest.NewRecorder()
	auth.loginHandler(foreign, request("https://untrusted.example.test"))
	if foreign.Code != http.StatusForbidden {
		t.Fatal("forwarded headers changed the trusted login origin")
	}
	login := httptest.NewRecorder()
	auth.loginHandler(login, request("http://admin.example.test"))
	if login.Code != http.StatusOK {
		t.Fatal("same-origin direct HTTP login failed")
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatal("direct HTTP session lost its applicable cookie protections")
	}
	mutations := 0
	handler := securityHeaders(auth.protect(func(w http.ResponseWriter, r *http.Request) {
		mutations++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, check := range []struct {
		name, origin, csrf string
		withCookie         bool
		status             int
	}{
		{"anonymous", "http://admin.example.test", "", false, http.StatusUnauthorized},
		{"missing CSRF", "http://admin.example.test", "", true, http.StatusForbidden},
		{"foreign origin", "https://untrusted.example.test", csrfForToken(cookie.Value), true, http.StatusForbidden},
		{"authorized", "http://admin.example.test", csrfForToken(cookie.Value), true, http.StatusNoContent},
	} {
		r := request(check.origin)
		r.Method = http.MethodPut
		if check.withCookie {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", check.csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != check.status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("%s lost direct-server protection: %d", check.name, w.Code)
		}
	}
	if mutations != 1 {
		t.Fatal("an unauthorized request mutated state")
	}
	for attempt := range 11 {
		r := request("http://admin.example.test")
		r.RemoteAddr = "192.0.2.2:1234"
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", attempt+1))
		r.Body = http.NoBody
		w := httptest.NewRecorder()
		auth.loginHandler(w, r)
		want := http.StatusBadRequest
		if attempt == 10 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("forwarded address bypassed connection-based throttling: attempt %d, status %d", attempt, w.Code)
		}
	}
}

func TestSessionRequiresCSRFAndSameOriginAndLogoutRevokesIt(t *testing.T) {
	fixture := newSecurityFixture(t)
	auth := fixture.auth
	login := httptest.NewRequest(http.MethodPost, "https://admin.example.test/api/login", strings.NewReader(`{"password":"synthetic-test-password"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "https://admin.example.test")
	response := httptest.NewRecorder()
	auth.loginHandler(response, login)
	if response.Code != http.StatusOK {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	var sessionData struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &sessionData); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if !sessionData.Authenticated || sessionData.CSRF == "" || len(cookies) != 1 {
		t.Fatal("login did not establish a usable session")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie lacks transport/script/cross-site isolation")
	}
	// Restarting authentication keeps the durable session usable without
	// keeping a plaintext token or an in-memory session map on the server.
	restarted, err := newAuthentication("synthetic-test-password", "https://admin.example.test", fixture.db, fixture.vault)
	if err != nil {
		t.Fatal(err)
	}
	auth = restarted
	mutations := 0
	mutate := auth.protect(func(w http.ResponseWriter, r *http.Request) { mutations++; w.WriteHeader(http.StatusNoContent) })
	request := func(token, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodPut, "https://admin.example.test/api/secrets/example", nil)
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", token)
		r.Header.Set("Origin", origin)
		return r
	}
	for _, r := range []*http.Request{request("", "https://admin.example.test"), request(sessionData.CSRF, "https://other.example.test")} {
		w := httptest.NewRecorder()
		mutate(w, r)
		if w.Code != http.StatusForbidden || mutations != 0 {
			t.Fatal("untrusted request changed protected state")
		}
	}
	accepted := httptest.NewRecorder()
	mutate(accepted, request(sessionData.CSRF, "https://admin.example.test"))
	if accepted.Code != http.StatusNoContent || mutations != 1 {
		t.Fatal("authenticated same-origin request did not succeed")
	}
	logout := httptest.NewRecorder()
	auth.protect(auth.logoutHandler)(logout, request(sessionData.CSRF, "https://admin.example.test"))
	if logout.Code != http.StatusNoContent {
		t.Fatal("logout failed")
	}
	replay := httptest.NewRecorder()
	mutate(replay, request(sessionData.CSRF, "https://admin.example.test"))
	if replay.Code != http.StatusUnauthorized || mutations != 1 {
		t.Fatal("revoked session could mutate state")
	}
}
