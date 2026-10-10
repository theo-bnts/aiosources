package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestCollectionSettingsRejectUntrustedInvalidAndStaleWrites(t *testing.T) {
	fixture := newSecurityFixture(t)
	if err := fixture.db.InitializeCollectionSettings(fixture.ctx, model.DefaultCollectionWorkers); err != nil {
		t.Fatal(err)
	}
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	fixture.handler, err = New(Options{Store: fixture.db, Providers: registry, Jobs: jobs.New(fixture.db, registry, nil, 1), Vault: fixture.vault,
		AdminPassword: "synthetic-test-password", PublicURL: "https://admin.example.test", Frontend: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("panel")}}})
	if err != nil {
		t.Fatal(err)
	}
	const path = "/api/settings/collections"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if response := fixture.request(method, path, nil, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s: %d", method, response.Code)
		}
	}
	cookie := fixture.login(t, "")
	readOverview := func(t *testing.T) model.CollectionOverview {
		t.Helper()
		response := fixture.request(http.MethodGet, path, nil, cookie)
		var overview model.CollectionOverview
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &overview) != nil {
			t.Fatalf("read settings: %d %s", response.Code, response.Body.String())
		}
		return overview
	}
	initial := readOverview(t)
	desired := initial.Settings
	desired.Workers = 32
	encoded, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	validBody := string(encoded)
	withoutCSRF := httptest.NewRequest(http.MethodPut, "https://admin.example.test"+path, strings.NewReader(validBody)).WithContext(fixture.ctx)
	withoutCSRF.AddCookie(cookie)
	withoutCSRF.Header.Set("Content-Type", "application/json")
	withoutCSRF.Header.Set("Origin", "https://admin.example.test")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, withoutCSRF)
	if response.Code != http.StatusForbidden || readOverview(t) != initial {
		t.Fatalf("request without CSRF changed settings: %d", response.Code)
	}
	for _, test := range []struct {
		name   string
		field  string
		value  any
		remove bool
	}{
		{"missing workers", "workers", nil, true},
		{"null workers", "workers", nil, false},
		{"missing revision", "revision", nil, true},
		{"zero workers", "workers", 0, false},
		{"excess workers", "workers", 33, false},
		{"zero revision", "revision", 0, false},
		{"fractional workers", "workers", 1.5, false},
		{"string revision", "revision", "1", false},
		{"unknown field", "extra", true, false},
		{"excess retries", "max_quota_retries", 11, false},
		{"negative quota wait", "max_quota_wait_seconds", -1, false},
		{"retired recovery opt-out", "auto_resume_interrupted", false, false},
		{"negative progress threshold", "no_progress_requests", -1, false},
		{"invalid progress action", "no_progress_action", "ignore", false},
		{"retired timeout default", "default_request_timeout_seconds", 90, false},
		{"retired preview default", "default_preview_pages", 5, false},
		{"retired page default", "default_max_pages", 5, false},
		{"retired duration default", "default_max_duration_seconds", 30, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}
			if test.remove {
				delete(body, test.field)
			} else {
				body[test.field] = test.value
			}
			response := fixture.request(http.MethodPut, path, body, cookie)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid settings accepted: %d %s", response.Code, response.Body.String())
			}
			if got := readOverview(t); got != initial {
				t.Fatalf("invalid input changed collection state: %+v", got)
			}
		})
	}
	if _, err := fixture.db.CreateRun(fixture.ctx, model.Run{ProviderID: "settings-api", Config: model.Provider{ID: "settings-api"}, Mode: model.ModeFull}); err != nil {
		t.Fatal(err)
	}
	response = fixture.request(http.MethodPut, path, json.RawMessage(validBody), cookie)
	var saved model.CollectionOverview
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &saved) != nil {
		t.Fatalf("save settings: %d %s", response.Code, response.Body.String())
	}
	if saved.Settings.Workers != 32 || saved.Settings.Revision <= initial.Settings.Revision || saved.Running != 0 || saved.Queued != 1 {
		t.Fatalf("save did not return updated settings and current work: %+v", saved)
	}
	if got := readOverview(t); got != saved {
		t.Fatalf("saved settings were not durable: %+v", got)
	}
	stale := initial.Settings
	stale.Workers = 1
	response = fixture.request(http.MethodPut, path, stale, cookie)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale settings accepted: %d %s", response.Code, response.Body.String())
	}
	if got := readOverview(t); got != saved {
		t.Fatalf("stale revision overwrote saved state: %+v", got)
	}
}
