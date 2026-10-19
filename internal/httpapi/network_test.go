package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestNetworkAPIRequiresAdministrationAndProtectsConfiguration(t *testing.T) {
	f := newSecurityFixture(t)
	s := &server{options: Options{Store: f.db, Vault: f.vault}, auth: f.auth}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", f.auth.loginHandler)
	s.registerNetworkRoutes(mux)
	f.handler = securityHeaders(mux)
	if response := f.request(http.MethodGet, "/api/settings/network", nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatal("Network profiles were exposed without administrator authentication")
	}
	cookie := f.login(t, "")
	initial := f.request(http.MethodGet, "/api/settings/network", nil, cookie)
	var settings model.NetworkSettings
	if initial.Code != http.StatusOK || json.Unmarshal(initial.Body.Bytes(), &settings) != nil {
		t.Fatal("Could not read network settings")
	}
	settings.Proxies = []model.NetworkProxy{{ID: "warp", Name: "WARP", URL: "socks5://127.0.0.1:1", Enabled: false}}
	if response := f.request(http.MethodPut, "/api/settings/network", settings, cookie); response.Code != http.StatusOK {
		t.Fatal("Could not create a disabled proxy")
	}
	if response := f.request(http.MethodPut, "/api/settings/network", settings, cookie); response.Code != http.StatusConflict {
		t.Fatal("A stale network write was accepted")
	}
	if response := f.request(http.MethodPost, "/api/settings/network/test", map[string]string{"id": "warp", "url": "http://arbitrary.example/"}, cookie); response.Code != http.StatusBadRequest {
		t.Fatal("The test endpoint accepted an arbitrary destination")
	}
	if response := f.request(http.MethodPost, "/api/settings/network/test", map[string]string{"id": "missing"}, cookie); response.Code != http.StatusNotFound {
		t.Fatal("The test endpoint did not require a saved profile")
	}
	if response := f.request(http.MethodPost, "/api/settings/network/test", map[string]string{"id": "warp"}, cookie); response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "127.0.0.1") {
		t.Fatal("A failed proxy test exposed its address or tried another connection")
	}
	current, err := f.db.NetworkSettings(f.ctx)
	if err != nil || current.Proxies[0].Enabled || current.Default != model.NetworkSystem {
		t.Fatal("Testing a disabled proxy changed live routing")
	}
	body, _ := json.Marshal(current)
	req := httptest.NewRequest(http.MethodPut, "https://admin.example.test/api/settings/network", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://admin.example.test")
	req.Header.Set("X-CSRF-Token", "invalid")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatal("Network routing could be changed without CSRF protection")
	}
}
