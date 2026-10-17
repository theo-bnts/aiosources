package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicConfigurationSharesTheBoundedAdmissionWithoutUsingRequestQuota(t *testing.T) {
	fixture := newSecurityFixture(t)
	s := &server{options: Options{Store: fixture.db}, auth: fixture.auth,
		public: &publicAdmission{windows: map[string]loginWindow{}, slots: make(chan struct{}, 1)}}
	mux := http.NewServeMux()
	s.registerPublicInstance(mux)
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/public/config", nil).WithContext(fixture.ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	s.public.slots <- struct{}{}
	response := request()
	<-s.public.slots
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("public bootstrap bypassed the bounded admission", response.Code)
	}
	if response := request(); response.Code != http.StatusOK || len(s.public.slots) != 0 || len(s.public.windows) != 0 {
		t.Fatal("ordinary homepage refresh consumes addon quota or leaked its admission", response.Code)
	}
}

func TestPublicAnnounceSetupAcceptsUsableLinksWithinTheBoundedRequestBudget(t *testing.T) {
	f, addon, _ := addonAPI(t)
	source := `{"version":1,"id":"example-source","name":"Example Library","adapter":"http_json","url":"https://source.example.invalid/api","enabled":false}`
	save, _ := json.Marshal(map[string]string{"json": source})
	if response := f.request("PUT", "/api/providers/example-source", string(save), true, ""); response.Code != http.StatusOK {
		t.Fatal("could not create private source fixture", response.Code)
	}
	config, err := f.db.AIOConfig(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.Trackers["example-source"] = model.AIOTracker{Private: true}
	if err := f.db.SetAIOConfig(f.ctx, config); err != nil {
		t.Fatal(err)
	}
	access, err := f.db.PublicAccess(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	access.Enabled, access.ExposeSources = true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	var providerCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	addon.Providers.TBBase = upstream.URL
	key := strings.Repeat("k", 512)
	announce := "https://announce.example.invalid/" + strings.Repeat("v", 4300) + "/announce"
	input, _ := json.Marshal(map[string]any{
		"services":  map[string]string{"torbox": key},
		"announces": map[string]string{"example-source": announce},
	})
	if len(input) <= 4096 {
		t.Fatal("fixture must exercise a valid setup larger than the former body limit")
	}
	install := func(input []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/public/install", strings.NewReader(string(input))).WithContext(f.ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://addon.example")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	response := install(input)
	var created struct {
		URL string `json:"manifest_url"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &created) != nil {
		t.Fatal("valid encrypted installation rejected by the setup body limit", response.Code)
	}
	configuration := strings.TrimSuffix(strings.TrimPrefix(created.URL, "https://addon.example/public/stremio/"), "/manifest.json")
	visitor, err := addon.OpenPublicInstallation(f.ctx, "https://addon.example", configuration)
	if err != nil || visitor.Services["torbox"] != key || visitor.Announces["example-source"] != announce {
		t.Fatal("setup credentials did not round-trip through the usable encrypted link", err)
	}
	if response := f.request("GET", strings.TrimPrefix(created.URL, "https://addon.example"), "", false, ""); response.Code != http.StatusOK {
		t.Fatal("generated manifest exceeds the usable route budget", response.Code)
	}
	// The larger form still has a hard body limit before any provider request.
	oversized := []byte(`{"services":{"torbox":"` + strings.Repeat("x", 9000) + `"}}`)
	response = install(oversized)
	if response.Code != http.StatusBadRequest || providerCalls.Load() != 1 || strings.Contains(response.Body.String(), strings.Repeat("x", 64)) {
		t.Fatal("oversized setup was processed or echoed instead of rejected safely", response.Code)
	}
	if _, err := addon.OpenPublicInstallation(f.ctx, "https://addon.example", strings.Repeat("x", 9000)); err != aiosource.ErrAuth {
		t.Fatal("oversized encrypted link accepted", err)
	}
}

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
	if response := f.request("PUT", "/api/public-access", `{"revision":1,"enabled":true,"expose_sources":true}`, false, ""); response.Code != 401 {
		t.Fatal("anonymous visitor changed source exposure")
	}
	if response := f.request("PUT", "/api/public-access", `{"revision":1,"enabled":true}`, true, ""); response.Code != 204 {
		t.Fatal("public access could not be enabled", response.Code)
	}
	config(false)
	if response := install(input); response.Code != 400 || providerCalls.Load() != 0 || strings.Contains(response.Body.String(), "visitor-passkey") {
		t.Fatal("unconsented announce contacted a provider or was reflected", response.Code)
	}
	if response := f.request("PUT", "/api/public-access", `{"revision":2,"enabled":true,"expose_sources":true}`, true, ""); response.Code != 204 {
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
	if response := f.request("PUT", "/api/public-access", `{"revision":3,"enabled":true,"expose_sources":false}`, true, ""); response.Code != 204 {
		t.Fatal("optional permission cannot be withdrawn", response.Code)
	}
	config(false)
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 200 {
		t.Fatal("withdrawing optional permission revoked cached-only installation")
	}
	if response := f.request("PUT", "/api/public-access", `{"revision":4,"enabled":false,"expose_sources":true}`, true, ""); response.Code != 204 {
		t.Fatal("public access cannot be disabled", response.Code)
	}
	config(false)
	if response := f.request("GET", manifestPath, "", false, ""); response.Code != 401 {
		t.Fatal("disabling public access retained visitor manifest access")
	}
}
