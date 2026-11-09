package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviousSessionCookieMigratesWithoutChangingCredentialsOrIsolation(t *testing.T) {
	f := newSecurityFixture(t)
	current := f.login(t, "")
	previous := *current
	previous.Name = previousSessionCookie
	response := f.request(http.MethodGet, "/api/session", nil, &previous)
	var body struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || !body.Authenticated || body.CSRF != csrfForToken(previous.Value) {
		t.Fatal("an existing session did not survive the naming upgrade")
	}
	var adopted *http.Cookie
	var retired bool
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie {
			adopted = cookie
		}
		if cookie.Name == previousSessionCookie && cookie.MaxAge < 0 {
			retired = true
		}
	}
	if adopted == nil || adopted.Value != previous.Value || !adopted.HttpOnly || !adopted.Secure || adopted.SameSite != http.SameSiteStrictMode || adopted.Path != "/" || !retired {
		t.Fatal("the cookie upgrade changed identity or weakened browser isolation")
	}
	next := f.request(http.MethodGet, "/api/session", nil, adopted)
	if next.Code != http.StatusOK || json.Unmarshal(next.Body.Bytes(), &body) != nil || !body.Authenticated || len(next.Result().Cookies()) != 0 {
		t.Fatal("the adopted cookie was not stable")
	}
}

func TestInvalidCurrentSessionCannotFallBackToAnotherPreviousIdentity(t *testing.T) {
	f := newSecurityFixture(t)
	previous := *f.login(t, "")
	previous.Name = previousSessionCookie
	r := httptest.NewRequest(http.MethodGet, "https://admin.example.test/api/session", nil).WithContext(f.ctx)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: strings.Repeat("x", 43)})
	r.AddCookie(&previous)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	var body struct {
		Authenticated bool `json:"authenticated"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Authenticated || len(w.Result().Cookies()) != 0 {
		t.Fatal("an invalid current cookie switched to a previous identity")
	}
}
