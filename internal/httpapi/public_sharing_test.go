package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicSharingRoutesRequireTheirOwnCredentialAndPreservePrivateData(t *testing.T) {
	f, _, conn := addonAPI(t)
	for _, route := range []string{"/api/public-catalog", "/api/providers", "/api/secrets"} {
		if response := f.request("GET", route, "", false, ""); response.Code != 401 {
			t.Fatal("public administration route", route, response.Code)
		}
	}
	install := func(origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/public/catalogs/install", strings.NewReader(body)).WithContext(f.ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	if response := install("https://addon.example", `{}`); response.Code != 404 {
		t.Fatal("disabled catalog setup", response.Code)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled, access.CatalogEnabled = true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/public/config", "", false, "")
	if strings.Contains(response.Body.String(), `"catalog_enabled":true`) || strings.Contains(response.Body.String(), `"catalog_fields"`) {
		t.Fatal("catalog exposed without source permission")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.ExposeSources = true
	access.CatalogEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('alpha','https://private.invalid/?apikey=private-native-secret','{"title":"Example Alpha","size":10,"info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","download_url":"https://private.invalid/admin-download","magnet":"magnet:?tr=https://admin.invalid/passkey/announce"}'),('beta','two','{"title":"Example Beta","size":20}')`); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://untrusted.example"} {
		if response := install(origin, `{}`); response.Code != 403 {
			t.Fatal("setup origin bypassed", response.Code)
		}
	}
	for _, body := range []string{`{"announces":{"alpha":"https://secret.invalid/announce"}}`, `{"fields":["download_url"]}`, `{"services":{"torbox":"never-store"}}`} {
		if response := install("https://addon.example", body); response.Code < 400 {
			t.Fatal("unsafe catalog setup accepted")
		}
	}
	response = install("https://addon.example", `{"fields":["title","size"]}`)
	var connection struct {
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &connection) != nil || connection.Password == "" {
		t.Fatal("catalog setup failed", response.Code)
	}
	endpoint, err := url.Parse(connection.URL)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.RawQuery != "" || endpoint.Scheme != "https" || !strings.HasPrefix(endpoint.Path, "/public/catalogs/") {
		t.Fatal("generated connection is incompatible with remote catalogs")
	}
	if response = f.request("GET", endpoint.Path, "", true, ""); response.Code != 401 {
		t.Fatal("admin session replaced export credential", response.Code)
	}
	if response = f.request("GET", endpoint.Path, "", false, "wrong"); response.Code != 401 {
		t.Fatal("invalid password exported catalog", response.Code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if response = f.request(method, endpoint.Path, "", false, connection.Password); response.Code != 405 {
			t.Fatal("catalog was not read-only", method, response.Code)
		}
	}
	response = f.request("GET", endpoint.Path+"?limit=1", "", false, connection.Password)
	var first, second model.CatalogEnvelope
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &first) != nil || len(first.Items) != 1 || first.NextCursor == "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog first page", response.Code)
	}
	response = f.request("GET", endpoint.Path+"?cursor="+url.QueryEscape(first.NextCursor), "", false, connection.Password)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &second) != nil || len(second.Items) != 1 || second.Checkpoint == "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("catalog continuation", response.Code)
	}
	exported, _ := json.Marshal([]model.CatalogEnvelope{first, second})
	for _, private := range []string{"private-native-secret", "admin-download", "passkey", "magnet:", "info_hash"} {
		if strings.Contains(string(exported), private) {
			t.Fatal("catalog disclosed unselected or private data")
		}
	}
	if response = f.request("HEAD", endpoint.Path, "", false, connection.Password); response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("catalog HEAD did not remain read-only and bodyless", response.Code)
	}
	for _, query := range []string{"?fields=download_url", "?cursor=invalid", "?limit=0", "?limit=1&limit=2"} {
		if response = f.request("GET", endpoint.Path+query, "", false, connection.Password); response.Code != 400 {
			t.Fatal("invalid traversal accepted", query, response.Code)
		}
	}
	for _, route := range []string{"/api/public-catalog", "/api/shares", "/api/secrets"} {
		if response = f.request("GET", route, "", false, connection.Password); response.Code != 401 {
			t.Fatal("catalog password opened administration")
		}
	}
	for _, table := range []string{"shares", "share_requests", "secrets"} {
		var count int
		if err := conn.QueryRow(f.ctx, "SELECT count(*) FROM aiosources."+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("visitor state was saved", table, err)
		}
	}
	var logs string
	if err := conn.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(l))::text,'[]') FROM aiosources.activity_logs l`).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, "catalog.public_exported") || !strings.Contains(logs, "Public catalog page exported") || strings.Contains(logs, connection.Password) || strings.Contains(logs, endpoint.Path) || strings.Contains(logs, "private-native-secret") {
		t.Fatal("safe catalog diagnostics missing or leaking configuration")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.ExposeSources = false
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if response = f.request("GET", endpoint.Path, "", false, connection.Password); response.Code != 401 {
		t.Fatal("source exposure revocation did not block issued links", response.Code)
	}
}

func TestPublicSharingAdminPolicyUsesRevisionAndCSRF(t *testing.T) {
	f, _, _ := addonAPI(t)
	response := f.request("GET", "/api/public-catalog", "", true, "")
	var data struct {
		Policy model.PublicCatalogPolicy `json:"policy"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &data) != nil {
		t.Fatal("policy read", response.Code)
	}
	data.Policy.Fields = []string{"title"}
	data.Policy.CatalogEntities = model.CatalogEntities{Languages: true}
	body, _ := json.Marshal(data.Policy)
	r := httptest.NewRequest("PUT", "/api/public-catalog", strings.NewReader(string(body))).WithContext(f.ctx)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("policy update bypassed CSRF", w.Code)
	}
	if response = f.request("PUT", "/api/public-catalog", string(body), true, ""); response.Code != 204 {
		t.Fatal("policy update", response.Code)
	}
	if response = f.request("PUT", "/api/public-catalog", string(body), true, ""); response.Code != 409 {
		t.Fatal("stale policy overwrote newer choices", response.Code)
	}
	data.Policy.Revision++
	data.Policy.Sources = []string{"not-configured"}
	body, _ = json.Marshal(data.Policy)
	if response = f.request("PUT", "/api/public-catalog", string(body), true, ""); response.Code != 422 {
		t.Fatal("unknown selected source accepted", response.Code)
	}
}
