package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSavedIncomingTrustAndQuotasApplyImmediatelyAcrossPublicProtocols(t *testing.T) {
	f, _, _ := addonAPI(t)
	settings, _ := f.db.NetworkSettings(f.ctx)
	settings.Incoming = &model.IncomingNetwork{Enabled: true, IPHeader: "CF-Connecting-IP", Format: "single", TrustedProxies: []string{"10.0.0.2"}}
	encoded, _ := json.Marshal(settings)
	if w := f.request("PUT", "/api/settings/network", string(encoded), true, ""); w.Code != 200 {
		t.Fatal("incoming settings could not be saved", w.Code)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Limits = model.PublicLimits{SetupPerMinute: 2, RequestsPerMinute: 2, Concurrent: 50}
	encoded, _ = json.Marshal(access)
	if w := f.request("PUT", "/api/public-access", string(encoded), true, ""); w.Code != 204 {
		t.Fatal("quota settings could not be saved", w.Code)
	}
	call := func(path, peer, visitor string, admin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil).WithContext(f.ctx)
		r.RemoteAddr = peer
		r.Header.Set("CF-Connecting-IP", visitor)
		if admin {
			r.AddCookie(f.cookie)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	if call("/api/settings/network/incoming", "10.0.0.2:1234", "203.0.113.1", false).Code != 401 {
		t.Fatal("visitor could read proxy diagnostics")
	}
	w := call("/api/settings/network/incoming", "10.0.0.2:1234", "203.0.113.1", true)
	var status struct {
		ConnectionIP string `json:"connection_ip"`
		VisitorIP    string `json:"visitor_ip"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.ConnectionIP != "10.0.0.2" || status.VisitorIP != "203.0.113.1" {
		t.Fatal("saved proxy policy was not applied")
	}
	stremio := "/public/stremio/invalid/manifest.json"
	torznab := "/public/torznab/api?apikey=invalid&t=caps"
	if call(stremio, "10.0.0.2:1234", "203.0.113.1", false).Code != 401 || call(torznab, "10.0.0.2:1234", "203.0.113.1", false).Code != 401 {
		t.Fatal("initial requests should reach credential validation")
	}
	if call(stremio, "10.0.0.2:1234", "203.0.113.1", false).Code != 429 {
		t.Fatal("Stremio and Torznab did not share their per-visitor budget")
	}
	if call(stremio, "10.0.0.2:1234", "203.0.113.2", false).Code != 401 {
		t.Fatal("visitors behind the same proxy share a quota")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.Limits.RequestsPerMinute = 3
	encoded, _ = json.Marshal(access)
	if f.request("PUT", "/api/public-access", string(encoded), true, "").Code != 204 {
		t.Fatal("could not raise live request limit")
	}
	if call(stremio, "10.0.0.2:1234", "203.0.113.1", false).Code != 401 || call(stremio, "10.0.0.2:1234", "203.0.113.1", false).Code != 429 {
		t.Fatal("saved quota did not apply immediately or reset consumed attempts")
	}
	settings, _ = f.db.NetworkSettings(f.ctx)
	settings.Incoming.Enabled = false
	encoded, _ = json.Marshal(settings)
	if f.request("PUT", "/api/settings/network", string(encoded), true, "").Code != 200 {
		t.Fatal("could not withdraw header trust")
	}
	w = call("/api/settings/network/incoming", "10.0.0.2:1234", "203.0.113.1", true)
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || status.VisitorIP != "10.0.0.2" {
		t.Fatal("withdrawn trust remained active")
	}
}

func TestIncomingTrustAlsoSeparatesAdministratorLoginBudgets(t *testing.T) {
	f := newSecurityFixture(t)
	s := &server{options: Options{Store: f.db}, auth: f.auth}
	s.initRequestPolicy()
	settings, _ := f.db.NetworkSettings(f.ctx)
	settings.Incoming = &model.IncomingNetwork{Enabled: true, IPHeader: "X-Real-IP", Format: "single", TrustedProxies: []string{"10.0.0.2"}}
	if err := f.db.UpdateNetworkSettings(f.ctx, settings); err != nil {
		t.Fatal(err)
	}
	for _, visitor := range []string{"203.0.113.1", "203.0.113.2"} {
		for i := range 11 {
			r := httptest.NewRequest(http.MethodPost, "/api/login", nil).WithContext(f.ctx)
			r.RemoteAddr = "10.0.0.2:1234"
			r.Header.Set("X-Real-IP", visitor)
			if got := f.auth.allowAttempt(r); got != (i < 10) {
				t.Fatal("login quota shared between visitors", visitor, i)
			}
		}
	}
}
