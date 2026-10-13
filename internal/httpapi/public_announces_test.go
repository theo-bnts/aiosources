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

func TestPublicAnnounceRoutesRequireSeparateConsentAndExposeOnlySourceNames(t *testing.T) {
	f, addon, _ := addonAPI(t)
	source := `{"version":1,"id":"example-source","name":"Example Library","adapter":"http_json","url":"https://source.example.invalid/private-endpoint","enabled":false,"auth":{"type":"bearer","secret_ref":"admin_source_key"}}`
	save, _ := json.Marshal(map[string]string{"json": source})
	if response := f.request("PUT", "/api/providers/example-source", string(save), true, ""); response.Code != 200 {
		t.Fatal("could not create private source fixture", response.Code, response.Body.String())
	}
	c, err := f.db.AIOConfig(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Trackers == nil {
		t.Fatal("default privacy configuration is missing")
	}
	tracker := c.Trackers["example-source"]
	tracker.Private, tracker.AnnounceSecret = true, "administrator_announce"
	c.Trackers["example-source"] = tracker
	if err := addon.Vault.Put(f.ctx, tracker.AnnounceSecret, "https://announce.example.invalid/administrator-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	input := `{"services":{"torbox":"visitor-service-key"},"announces":{"example-source":"https://announce.example.invalid/visitor-passkey/announce"}}`
	install := func(input string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/public/install", strings.NewReader(input)).WithContext(f.ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://addon.example")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	config := func(exposed bool) {
		t.Helper()
		response := f.request("GET", "/api/public/config", "", false, "")
		var info map[string]json.RawMessage
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &info) != nil {
			t.Fatal("public bootstrap failed", response.Code)
		}
		if _, exists := info["private_sources"]; exists != exposed {
			t.Fatal("private source list does not match administrator consent")
		}
		if exposed && string(info["private_sources"]) != `[{"id":"example-source","name":"Example Library"}]` {
			t.Fatal("bootstrap revealed source endpoints, secret references or more than names")
		}
	}
	config(false)
	if response := f.request("PUT", "/api/stremio/public-access", `{"revision":1,"enabled":true,"allow_private_downloads":true}`, false, ""); response.Code != 401 {
		t.Fatal("anonymous visitor changed source exposure")
	}
	if response := f.request("PUT", "/api/stremio/public-access", `{"revision":1,"enabled":true}`, true, ""); response.Code != 204 {
		t.Fatal("public access could not be enabled", response.Code)
	}
	config(false)
	if response := install(input); response.Code != 400 || providerCalls.Load() != 0 || strings.Contains(response.Body.String(), "visitor-passkey") {
		t.Fatal("unconsented announce contacted a provider or was reflected", response.Code)
	}
	if response := f.request("PUT", "/api/stremio/public-access", `{"revision":2,"enabled":true,"allow_private_downloads":true}`, true, ""); response.Code != 204 {
		t.Fatal("administrator could not enable optional source exposure", response.Code)
	}
	config(true)
	response := install(input)
	var created struct {
		URL string `json:"manifest_url"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &created) != nil || strings.Contains(created.URL, "visitor-passkey") {
		t.Fatal("public announce installation failed or exposed its values", response.Code)
	}
	manifestPath := strings.TrimPrefix(created.URL, "https://addon.example")
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 200 {
		t.Fatal("new public manifest is unusable", response.Code)
	}
	if response := f.request("PUT", "/api/stremio/public-access", `{"revision":3,"enabled":true,"allow_private_downloads":false}`, true, ""); response.Code != 204 {
		t.Fatal("optional permission cannot be withdrawn", response.Code)
	}
	config(false)
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 200 {
		t.Fatal("withdrawing optional permission revoked cached-only installation")
	}
	if response := f.request("PUT", "/api/stremio/public-access", `{"revision":4,"enabled":false,"allow_private_downloads":true}`, true, ""); response.Code != 204 {
		t.Fatal("public access cannot be disabled", response.Code)
	}
	config(false)
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 401 {
		t.Fatal("disabling public access retained visitor manifest access")
	}
}
