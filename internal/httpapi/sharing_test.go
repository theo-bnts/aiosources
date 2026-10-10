package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
)

type sharingAPIFixture struct {
	ctx     context.Context
	db      *store.Store
	handler http.Handler
	cookie  *http.Cookie
	csrf    string
}

func sharingAPI(t *testing.T, origin string) sharingAPIFixture {
	t.Helper()
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	registry, err := providers.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	_, err = registry.Save("fixture", `{"version":1,"id":"fixture","name":"Fixture source","adapter":"http_json","url":"https://source.invalid/api","enabled":false}`, "")
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.New(ctx, db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{Store: db, Providers: registry, Jobs: jobs.New(db, registry, secrets.Resolve, 1), Vault: secrets, AdminPassword: "synthetic-admin-password", PublicURL: origin, Frontend: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>Fixture</title>")}}})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"synthetic-admin-password"}`)).WithContext(ctx)
	login.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, login)
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &session) != nil || len(w.Result().Cookies()) != 1 {
		t.Fatalf("fixture login failed: %d %s", w.Code, w.Body.String())
	}
	return sharingAPIFixture{ctx: ctx, db: db, handler: handler, cookie: w.Result().Cookies()[0], csrf: session.CSRF}
}

func (f sharingAPIFixture) request(method, path, body string, admin bool, password string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(f.ctx)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if admin {
		r.AddCookie(f.cookie)
		r.Header.Set("X-CSRF-Token", f.csrf)
	}
	if password != "" {
		r.Header.Set("Authorization", "Bearer "+password)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func TestSharingAPIAdminCSRFAndCredentialIsolation(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	input := `{"name":"Friend","scope":"selected","source_ids":["fixture"],"fields":["title","size"]}`
	noCSRF := httptest.NewRequest(http.MethodPost, "/api/shares", strings.NewReader(input)).WithContext(fixture.ctx)
	noCSRF.Header.Set("Content-Type", "application/json")
	noCSRF.AddCookie(fixture.cookie)
	denied := httptest.NewRecorder()
	fixture.handler.ServeHTTP(denied, noCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("share creation bypassed CSRF: %d", denied.Code)
	}
	created := fixture.request(http.MethodPost, "/api/shares", input, true, "")
	var grant model.ShareCredential
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &grant) != nil || grant.Share.Enabled || grant.Share.URL != "https://owner.example.test/api/catalogs/"+grant.Share.ID || len(grant.Password) != 43 {
		t.Fatalf("disabled one-time credential creation failed: %d %s", created.Code, created.Body.String())
	}
	feed := "/api/catalogs/" + grant.Share.ID
	if response := fixture.request(http.MethodGet, feed, "", false, grant.Password); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled share exported records: %d", response.Code)
	}
	update := model.ShareInput{Name: grant.Share.Name, Enabled: true, Scope: "selected", SourceIDs: grant.Share.SourceIDs, Fields: grant.Share.Fields, Revision: grant.Share.Revision, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads}
	encoded, _ := json.Marshal(update)
	response := fixture.request(http.MethodPut, "/api/shares/"+grant.Share.ID, string(encoded), true, "")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &grant.Share) != nil {
		t.Fatalf("enable share failed: %d %s", response.Code, response.Body.String())
	}
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/shares", ""},
		{http.MethodPost, "/api/shares", input},
		{http.MethodGet, "/api/raw", ""},
		{http.MethodGet, "/api/raw/1/download", ""},
		{http.MethodGet, "/api/pages/1/download", ""},
		{http.MethodGet, "/api/secrets", ""},
		{http.MethodPut, "/api/secrets/secret", `{"value":"never-save"}`},
		{http.MethodGet, "/api/providers/fixture", ""},
		{http.MethodPost, "/api/runs", `{"provider_id":"fixture","mode":"full"}`},
		{http.MethodDelete, "/api/shares/" + grant.Share.ID, `{"revision":2}`},
	} {
		response := fixture.request(route.method, route.path, route.body, false, grant.Password)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("sharing credential reached administrator route %s %s: %d", route.method, route.path, response.Code)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		response := fixture.request(method, feed, `{"fields":{"title":"overwrite"}}`, false, grant.Password)
		if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("public mutation was not rejected: %s %d", method, response.Code)
		}
	}
	if response := fixture.request(http.MethodHead, feed, "", false, grant.Password); response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD response: %d %s", response.Code, response.Body.String())
	}
	shares, err := fixture.db.Shares(fixture.ctx)
	if err != nil || len(shares) != 1 || shares[0].LastSyncAt != nil {
		t.Fatalf("HEAD claimed a completed download: %+v %v", shares, err)
	}
	if response := fixture.request(http.MethodGet, feed, "", false, grant.Password); response.Code != http.StatusOK {
		t.Fatalf("enabled feed failed: %d %s", response.Code, response.Body.String())
	}
	listed := fixture.request(http.MethodGet, "/api/shares", "", true, "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), grant.Password) || strings.Contains(listed.Body.String(), "password_hash") {
		t.Fatalf("share listing disclosed credentials: %d %s", listed.Code, listed.Body.String())
	}
	var listing struct {
		Items     []model.Share   `json:"items"`
		Sources   []sharingSource `json:"sources"`
		Fields    []string        `json:"fields"`
		PublicURL string          `json:"public_url"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listing); err != nil || len(listing.Items) != 1 || listing.Items[0].LastSyncAt == nil || len(listing.Sources) != 1 || listing.Sources[0].ID != "fixture" || listing.PublicURL != "https://owner.example.test" {
		t.Fatalf("owner sharing metadata missing: %+v %v", listing, err)
	}
	rotationBody, _ := json.Marshal(map[string]int64{"revision": grant.Share.Revision})
	rotation := fixture.request(http.MethodPost, "/api/shares/"+grant.Share.ID+"/rotate", string(rotationBody), true, "")
	var rotated model.ShareCredential
	if rotation.Code != http.StatusOK || json.Unmarshal(rotation.Body.Bytes(), &rotated) != nil || rotated.Password == grant.Password {
		t.Fatalf("one-time rotation failed: %d %s", rotation.Code, rotation.Body.String())
	}
	if response := fixture.request(http.MethodGet, feed, "", false, grant.Password); response.Code != http.StatusUnauthorized {
		t.Fatalf("previous sharing password still worked: %d", response.Code)
	}
	if response := fixture.request(http.MethodGet, feed, "", false, rotated.Password); response.Code != http.StatusOK {
		t.Fatalf("new sharing password failed: %d", response.Code)
	}
}

func TestSharingAPIRejectsPermissionExpansionAndUnconfiguredHTTPS(t *testing.T) {
	fixture := sharingAPI(t, "")
	for _, body := range []string{
		`{"name":"Friend","enabled":true,"scope":"all","source_ids":[],"fields":["title"]}`,
		`{"name":"Friend","scope":"all","source_ids":[],"fields":["download_url"]}`,
		`{"name":"Friend","scope":"selected","source_ids":[],"fields":["title"]}`,
		`{"name":"Friend","scope":"selected","source_ids":["missing"],"fields":["title"]}`,
	} {
		if response := fixture.request(http.MethodPost, "/api/shares", body, true, ""); response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid sharing permission accepted: %d %s", response.Code, response.Body.String())
		}
	}
	if response := fixture.request(http.MethodPost, "/api/shares", `{"name":"Friend","scope":"all","fields":["title"],"password":"weak"}`, true, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("manual sharing password accepted: %d", response.Code)
	}
	created := fixture.request(http.MethodPost, "/api/shares", `{"name":"Friend","scope":"all","fields":["title"]}`, true, "")
	var grant model.ShareCredential
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &grant) != nil || grant.Share.URL != "" {
		t.Fatalf("disabled sharing setup required HTTPS: %d %s", created.Code, created.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "http://forged.example/api/catalogs/"+grant.Share.ID, nil).WithContext(fixture.ctx)
	request.Header.Set("Forwarded", "proto=https;host=trusted.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Authorization", "Bearer "+grant.Password)
	w := httptest.NewRecorder()
	fixture.handler.ServeHTTP(w, request)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("untrusted proxy headers enabled HTTP sharing: %d %s", w.Code, w.Body.String())
	}
}

func TestSharingAPIRejectsFieldProviderAndPaginationOverrides(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	created := fixture.request(http.MethodPost, "/api/shares", `{"name":"Friend","enabled":true,"scope":"all","fields":["title"]}`, true, "")
	var grant model.ShareCredential
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &grant) != nil {
		t.Fatalf("create share failed: %d %s", created.Code, created.Body.String())
	}
	for _, query := range []string{"fields=download_url", "provider=private", "password=weak", "limit=1001", "limit=0", "limit=1&limit=2", "cursor=&checkpoint=", "checkpoint=forged", "cursor=forged"} {
		response := fixture.request(http.MethodGet, "/api/catalogs/"+grant.Share.ID+"?"+query, "", false, grant.Password)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe query accepted: %s: %d %s", query, response.Code, response.Body.String())
		}
	}
}

func TestSharingAPICanDisableAfterSelectedProviderDisappears(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	created := fixture.request(http.MethodPost, "/api/shares", `{"name":"Friend","enabled":true,"scope":"selected","source_ids":["fixture"],"fields":["title"]}`, true, "")
	var grant model.ShareCredential
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &grant) != nil {
		t.Fatalf("create share: %d %s", created.Code, created.Body.String())
	}
	provider := fixture.request(http.MethodGet, "/api/providers/fixture", "", true, "")
	var definition model.ProviderDocument
	if provider.Code != http.StatusOK || json.Unmarshal(provider.Body.Bytes(), &definition) != nil {
		t.Fatalf("read provider: %d %s", provider.Code, provider.Body.String())
	}
	deletion, _ := json.Marshal(map[string]string{"revision": definition.Revision})
	if removed := fixture.request(http.MethodDelete, "/api/providers/fixture", string(deletion), true, ""); removed.Code != http.StatusNoContent {
		t.Fatalf("remove provider: %d %s", removed.Code, removed.Body.String())
	}
	input := model.ShareInput{Name: grant.Share.Name, Enabled: false, Scope: grant.Share.Scope, SourceIDs: grant.Share.SourceIDs, Fields: grant.Share.Fields, Revision: grant.Share.Revision, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads}
	body, _ := json.Marshal(input)
	disabled := fixture.request(http.MethodPut, "/api/shares/"+grant.Share.ID, string(body), true, "")
	var saved model.Share
	if disabled.Code != http.StatusOK || json.Unmarshal(disabled.Body.Bytes(), &saved) != nil || saved.Enabled {
		t.Fatalf("missing provider prevented disabling access: %d %s", disabled.Code, disabled.Body.String())
	}
	if response := fixture.request(http.MethodGet, "/api/catalogs/"+grant.Share.ID, "", false, grant.Password); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled grant remained accessible: %d", response.Code)
	}
	input.Enabled, input.Revision = true, saved.Revision
	body, _ = json.Marshal(input)
	if response := fixture.request(http.MethodPut, "/api/shares/"+grant.Share.ID, string(body), true, ""); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing provider could be re-enabled: %d", response.Code)
	}
}
