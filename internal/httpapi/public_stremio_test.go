package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicStremioRevocationIsIndependentOfSearchAndBrowserCredentials(t *testing.T) {
	f, addon, _ := addonAPI(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
	}))
	t.Cleanup(upstream.Close)
	addon.Providers.TBBase = upstream.URL
	access, err := f.db.PublicAccess(f.ctx)
	if err != nil || !access.StremioEnabled || access.StremioGeneration != 1 {
		t.Fatal("migration changed the existing Stremio policy", err)
	}
	access.Enabled, access.SearchEnabled, access.TorznabEnabled = true, true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	send := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body)).WithContext(f.ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://addon.example")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	body := `{"services":{"torbox":"synthetic-visitor-key"}}`
	response := send("/api/public/install", body)
	var original struct{ Credential, ManifestURL string }
	var payload struct {
		Credential  string `json:"credential"`
		ManifestURL string `json:"manifest_url"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatal("initial installation failed", response.Code)
	}
	original.Credential, original.ManifestURL = payload.Credential, payload.ManifestURL
	manifestPath := strings.TrimPrefix(original.ManifestURL, "https://addon.example")
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 200 {
		t.Fatal("initial manifest failed", response.Code)
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.StremioEnabled = false
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if response := send("/api/public/install", body); response.Code != 404 || calls.Load() != before {
		t.Fatal("disabled Stremio contacted the provider or created a link", response.Code)
	}
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 401 {
		t.Fatal("old manifest survived disabling", response.Code)
	}
	requestBody := fmt.Sprintf(`{"credential":%q}`, original.Credential)
	response = send("/api/public/credentials", requestBody)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.ManifestURL != "" || payload.Credential == "" {
		t.Fatal("Search cannot reuse credentials independently", response.Code)
	}
	searchProfile, err := addon.OpenPublicInstallation(f.ctx, "https://addon.example", payload.Credential)
	if err != nil {
		t.Fatal("browser credentials were revoked", err)
	}
	clear(searchProfile.Services)
	clear(searchProfile.Announces)
	// A caller cannot turn the Search credential endpoint into a Stremio bypass.
	if response := f.request("GET", "/public/stremio/"+payload.Credential+"/manifest.json", "", false, ""); response.Code != 401 {
		t.Fatal("Search credential bypassed disabled Stremio", response.Code)
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.StremioEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 401 {
		t.Fatal("revoked manifest resurrected after re-enabling", response.Code)
	}
	response = send("/api/public/install", requestBody)
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.ManifestURL == original.ManifestURL {
		t.Fatal("new Stremio installation failed", response.Code)
	}
	if response := f.request("GET", strings.TrimPrefix(payload.ManifestURL, "https://addon.example"), "", false, ""); response.Code != 200 {
		t.Fatal("new manifest rejected", response.Code)
	}
	latest, err := f.db.PublicAccess(f.ctx)
	if err != nil || !latest.SearchEnabled || !latest.TorznabEnabled || latest.Generation != searchProfile.Generation {
		t.Fatal("Stremio changes affected other public connections", err)
	}
}

func TestExistingPublicSettingsFormPreservesStremioUnlessExplicitlyChanged(t *testing.T) {
	f, _, _ := addonAPI(t)
	for _, stremio := range []bool{true, false} {
		access, err := f.db.PublicAccess(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		access.StremioEnabled = stremio
		if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
			t.Fatal(err)
		}
		access, _ = f.db.PublicAccess(f.ctx)
		response := f.request("PUT", "/api/public-access", fmt.Sprintf(`{"revision":%d,"enabled":true}`, access.Revision), true, "")
		latest, err := f.db.PublicAccess(f.ctx)
		if response.Code != 204 || err != nil || latest.StremioEnabled != stremio || latest.StremioGeneration != access.StremioGeneration {
			t.Fatal("an older open form changed the Stremio policy", response.Code, err)
		}
	}
}
