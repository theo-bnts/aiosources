package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStorageSettingsAndResetRequireAdminCSRFValidInputAndCurrentRevision(t *testing.T) {
	fixture := newSecurityFixture(t)
	s := &server{options: Options{Store: fixture.db, Vault: fixture.vault}, auth: fixture.auth}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", fixture.auth.loginHandler)
	mux.HandleFunc("GET /api/session", fixture.auth.sessionHandler)
	s.registerStorageRoutes(mux)
	fixture.handler = securityHeaders(mux)
	const path = "/api/settings/storage"
	for _, method := range []string{"GET", "PUT"} {
		if response := fixture.request(method, path, nil, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous storage access: %d", response.Code)
		}
	}
	if response := fixture.request("POST", path+"/reset-sources", nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous reset: %d", response.Code)
	}
	cookie := fixture.login(t, "")
	initial, err := fixture.db.StorageOverview(fixture.ctx)
	if err != nil || initial.Settings.Mode != model.StorageCompact {
		t.Fatalf("default storage: %+v %v", initial, err)
	}
	for _, body := range []any{
		map[string]any{"mode": "unknown", "revision": initial.Settings.Revision},
		map[string]any{"mode": "full"},
		map[string]any{"mode": nil, "revision": initial.Settings.Revision},
		map[string]any{"mode": "compact", "revision": initial.Settings.Revision, "extra": true},
	} {
		if response := fixture.request("PUT", path, body, cookie); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid storage settings accepted: %d", response.Code)
		}
	}
	request := httptest.NewRequest("PUT", "https://admin.example.test"+path, strings.NewReader(`{"mode":"full","revision":1}`)).WithContext(fixture.ctx)
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example.test")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("storage write without CSRF: %d", response.Code)
	}
	desired := initial.Settings
	desired.Mode = model.StorageFull
	if response := fixture.request("PUT", path, desired, cookie); response.Code != http.StatusOK {
		t.Fatalf("storage save failed: %d %s", response.Code, response.Body.String())
	}
	if response := fixture.request("PUT", path, desired, cookie); response.Code != http.StatusConflict {
		t.Fatalf("stale storage write accepted: %d", response.Code)
	}
	current, _ := fixture.db.StorageOverview(fixture.ctx)
	if response := fixture.request("POST", path+"/reset-sources", map[string]any{"revision": current.Settings.Revision, "confirm": false}, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed reset: %d", response.Code)
	}
	if response := fixture.request("POST", path+"/reset-sources", map[string]any{"revision": initial.Settings.Revision, "confirm": true}, cookie); response.Code != http.StatusConflict {
		t.Fatalf("stale reset: %d", response.Code)
	}
	request = httptest.NewRequest("POST", "https://admin.example.test"+path+"/reset-sources", strings.NewReader(`{"confirm":true,"revision":2}`)).WithContext(fixture.ctx)
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example.test")
	response = httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reset without CSRF: %d", response.Code)
	}
	if response := fixture.request("POST", path+"/reset-sources", map[string]any{"revision": current.Settings.Revision, "confirm": true}, cookie); response.Code != http.StatusOK {
		t.Fatalf("safe reset failed: %d %s", response.Code, response.Body.String())
	}
}
