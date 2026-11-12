package httpapi

import (
	"encoding/json"
	"github.com/theo-bnts/aiosources/internal/aiosource"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func trustedRequest(f sharingAPIFixture, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(f.ctx)
	r.RemoteAddr = "192.0.2.55:1234"
	r.Header.Set("Origin", "https://addon.example")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrfForToken(cookie.Value))
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func TestTrustedPasswordHashUsesArgon2idAndRejectsInvalidProofs(t *testing.T) {
	hash, err := hashTrustedPassword("synthetic trusted password")
	if err != nil || !strings.HasPrefix(hash, trustedPasswordPrefix) || !checkTrustedPassword(hash, "synthetic trusted password") {
		t.Fatal("password hash is not usable")
	}
	second, _ := hashTrustedPassword("synthetic trusted password")
	if hash == second || checkTrustedPassword(hash, "incorrect password") || checkTrustedPassword(hash, "short") || checkTrustedPassword(hash, strings.Repeat("x", 1025)) {
		t.Fatal("password proof or salt validation failed")
	}
	for _, value := range []string{"", "$argon2id$v=19$m=999999999,t=99,p=255$bad$bad", hash + "$suffix"} {
		if checkTrustedPassword(value, "synthetic trusted password") {
			t.Fatal("accepted malformed password hash")
		}
	}
	if _, err = hashTrustedPassword("short"); err == nil {
		t.Fatal("accepted short password")
	}
}

func TestTrustedUsersIsolationConnectionsAndRevocation(t *testing.T) {
	f, _, conn := addonAPI(t)
	body := `{"email":"Reader@Example.Invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true,"stremio_enabled":true,"torznab_enabled":true,"catalog_enabled":true}`
	created := f.request("POST", "/api/trusted-users", body, true, "")
	var user model.TrustedUser
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &user) != nil || user.Email != "reader@example.invalid" || strings.Contains(created.Body.String(), "password") {
		t.Fatal("account creation failed", created.Code, created.Body)
	}
	var stored string
	if err := conn.QueryRow(f.ctx, `SELECT password_hash FROM aiosources.trusted_users WHERE id=$1`, user.ID).Scan(&stored); err != nil || !strings.HasPrefix(stored, trustedPasswordPrefix) {
		t.Fatal("password is not hashed", err)
	}
	wrong := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"reader@example.invalid","password":"incorrect password"}`)
	unknown := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"unknown@example.invalid","password":"incorrect password"}`)
	if wrong.Code != 401 || unknown.Code != 401 || wrong.Body.String() != unknown.Body.String() {
		t.Fatal("login enumerates accounts")
	}
	login := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"READER@example.invalid","password":"synthetic trusted password"}`)
	if login.Code != 200 {
		t.Fatal("trusted sign-in failed", login.Code, login.Body)
	}
	var cookie *http.Cookie
	for _, value := range login.Result().Cookies() {
		if value.Name == trustedSessionCookie {
			cookie = value
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("trusted session cookie is not protected")
	}
	for _, path := range []string{"/api/providers", "/api/secrets", "/api/aiosource/config", "/api/events", "/api/shares", "/api/trusted-users", "/api/security"} {
		if got := trustedRequest(f, cookie, "GET", path, ""); got.Code != 401 {
			t.Fatal("trusted session reached administration", path, got.Code)
		}
	}
	info := trustedRequest(f, cookie, "GET", "/api/trusted/config", "")
	if info.Code != 200 || strings.Contains(info.Body.String(), "secret") || strings.Contains(info.Body.String(), "password") {
		t.Fatal("trusted config exposed private settings", info.Code, info.Body)
	}
	r := httptest.NewRequest("POST", "/api/trusted/connections/stremio", strings.NewReader(`{}`)).WithContext(f.ctx)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://foreign.example")
	csrf := httptest.NewRecorder()
	f.handler.ServeHTTP(csrf, r)
	if csrf.Code != 403 {
		t.Fatal("trusted mutation bypassed CSRF")
	}
	stremio := trustedRequest(f, cookie, "POST", "/api/trusted/connections/stremio", `{"preferences":{}}`)
	tz := trustedRequest(f, cookie, "POST", "/api/trusted/connections/torznab", `{"options":{}}`)
	catalog := trustedRequest(f, cookie, "POST", "/api/trusted/connections/catalog", `{"catalog":{}}`)
	var st, torznab, shared map[string]any
	if stremio.Code != 200 || tz.Code != 200 || catalog.Code != 200 {
		t.Fatal("trusted connection creation failed", stremio.Code, tz.Code, catalog.Code, stremio.Body, tz.Body, catalog.Body)
	}
	_ = json.Unmarshal(stremio.Body.Bytes(), &st)
	_ = json.Unmarshal(tz.Body.Bytes(), &torznab)
	_ = json.Unmarshal(catalog.Body.Bytes(), &shared)
	manifest, _ := url.Parse(st["manifest_url"].(string))
	if got := f.request("GET", manifest.Path, "", false, ""); got.Code != 200 {
		t.Fatal("trusted manifest depends on public mode", got.Code, got.Body)
	}
	feed := torznab["url"].(string) + "?t=caps&apikey=" + url.QueryEscape(torznab["api_key"].(string))
	if got := f.request("GET", feed, "", false, ""); got.Code != 200 {
		t.Fatal("trusted Torznab is not usable", got.Code, got.Body)
	}
	if got := f.request("GET", shared["url"].(string), "", false, shared["password"].(string)); got.Code != 200 {
		t.Fatal("trusted catalog is not usable", got.Code, got.Body)
	}
	// No trusted token may be replayed as an anonymous public credential.
	if got := f.request("GET", "/public/torznab/api?t=caps&apikey="+url.QueryEscape(torznab["api_key"].(string)), "", false, ""); got.Code == 200 {
		t.Fatal("trusted credential crossed the public boundary")
	}
	user.Enabled = false
	encoded, _ := json.Marshal(user)
	updated := f.request("PUT", "/api/trusted-users/"+user.ID, string(encoded), true, "")
	if updated.Code != 200 {
		t.Fatal("account revocation failed", updated.Code, updated.Body)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/config", ""); got.Code != 401 {
		t.Fatal("revoked browser session still usable", got.Code)
	}
	for _, path := range []string{manifest.Path, feed} {
		if got := f.request("GET", path, "", false, ""); got.Code != 401 {
			t.Fatal("revoked connection still usable", got.Code)
		}
	}
	if got := f.request("GET", shared["url"].(string), "", false, shared["password"].(string)); got.Code != 401 {
		t.Fatal("revoked shared catalog still usable", got.Code)
	}
	var changed model.TrustedUser
	_ = json.Unmarshal(updated.Body.Bytes(), &changed)
	changed.Enabled = true
	encoded, _ = json.Marshal(changed)
	if got := f.request("PUT", "/api/trusted-users/"+user.ID, string(encoded), true, ""); got.Code != 200 {
		t.Fatal("could not enable account again")
	}
	if got := f.request("GET", manifest.Path, "", false, ""); got.Code != 401 {
		t.Fatal("re-enabling restored a revoked link")
	}
}

func TestTrustedUserPermissionsAndPasswordReplacement(t *testing.T) {
	f, _, _ := addonAPI(t)
	created := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true,"stremio_enabled":false,"torznab_enabled":false,"catalog_enabled":false}`, true, "")
	var user model.TrustedUser
	_ = json.Unmarshal(created.Body.Bytes(), &user)
	if created.Code != 201 || user.CatalogEnabled {
		t.Fatal("explicit feature permissions were not preserved")
	}
	login := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"reader@example.invalid","password":"synthetic trusted password"}`)
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == trustedSessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no trusted session")
	}
	for _, feature := range []string{"stremio", "torznab", "catalog"} {
		if got := trustedRequest(f, cookie, "POST", "/api/trusted/connections/"+feature, `{}`); got.Code != 403 {
			t.Fatal("account bypassed permission", feature, got.Code)
		}
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/catalog/search?q=Example", ""); got.Code != 200 {
		t.Fatal("trusted Search not available", got.Code, got.Body)
	}
	if got := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"another synthetic password"}`, true, ""); got.Code != 409 {
		t.Fatal("duplicate email accepted", got.Code)
	}
	encoded, _ := json.Marshal(map[string]any{"email": user.Email, "enabled": true, "search_enabled": true, "revision": user.Revision, "password": "replacement synthetic password"})
	if got := f.request("PUT", "/api/trusted-users/"+user.ID, string(encoded), true, ""); got.Code != 200 {
		t.Fatal("password replacement failed", got.Code, got.Body)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/config", ""); got.Code != 401 {
		t.Fatal("old session survived password replacement")
	}
	if got := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"reader@example.invalid","password":"synthetic trusted password"}`); got.Code != 401 {
		t.Fatal("old password survived replacement", got.Code)
	}
	if got := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"reader@example.invalid","password":"replacement synthetic password"}`); got.Code != 200 {
		t.Fatal("new password not accepted", got.Code, got.Body)
	}
}

func TestTrustedSearchTimeoutDoesNotInvalidateSession(t *testing.T) {
	f, addon, _ := addonAPI(t)
	created := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"synthetic trusted password","enabled":true}`, true, "")
	if created.Code != 201 {
		t.Fatal("create account", created.Code)
	}
	login := trustedRequest(f, nil, "POST", "/api/trusted/login", `{"email":"reader@example.invalid","password":"synthetic trusted password"}`)
	var cookie *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == trustedSessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no trusted session")
	}
	auth, err := newAuthentication("synthetic-admin-password", "https://addon.example", f.db)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{auth: auth, options: Options{Store: f.db, AIOSource: addon}, streams: make(chan struct{}, 1)}
	handler := s.trustedCatalogHandler(20*time.Millisecond, func(_ http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) (any, error) {
		<-r.Context().Done()
		return map[string]string{"title": "must-not-be-returned"}, r.Context().Err()
	})
	request := httptest.NewRequest("GET", "https://addon.example/api/trusted/catalog/search?q=Example", nil).WithContext(f.ctx)
	request.AddCookie(cookie)
	out := httptest.NewRecorder()
	handler(out, request)
	if out.Code != 503 || strings.Contains(out.Body.String(), "must-not-be-returned") {
		t.Fatal("timeout incorrectly treated as expired login", out.Code)
	}
	if got := trustedRequest(f, cookie, "GET", "/api/trusted/config", ""); got.Code != 200 {
		t.Fatal("valid session was lost", got.Code)
	}
}

func TestNewTrustedAccountsDefaultToAllFeaturesWithoutEnablingAccount(t *testing.T) {
	f, _, _ := addonAPI(t)
	created := f.request("POST", "/api/trusted-users", `{"email":"reader@example.invalid","password":"synthetic trusted password"}`, true, "")
	var user model.TrustedUser
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &user) != nil || user.Enabled || !user.SearchEnabled || !user.TorznabEnabled || !user.StremioEnabled || !user.CatalogEnabled || !user.UseInstanceDebrid || !user.UseInstanceAnnounces {
		t.Fatal("inconsistent account defaults", created.Code, user)
	}
}
