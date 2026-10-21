package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestPublicDeploymentNeverExposesPrivateDataWithoutItsOwnCredential(t *testing.T) {
	fixture, addon, _ := addonAPI(t)
	for _, path := range []string{
		"/api/health", "/api/providers", "/api/providers/example", "/api/provider-schema", "/api/adapters", "/api/secrets", "/api/runs", "/api/runs/example", "/api/runs/example/events", "/api/events", "/api/raw", "/api/raw/1", "/api/raw/1/download", "/api/pages/1/download", "/api/torrents", "/api/torrents/1/history", "/api/source-torrent-stats", "/api/schedules", "/api/settings/collections", "/api/system-health", "/api/security", "/api/security/sessions", "/api/logs", "/api/shares", "/api/remotes", "/api/aiosource", "/api/aiosource/matches", "/api/aiosource/candidates", "/api/operations", "/api/stremio/statistics", "/api/stremio/requests",
	} {
		t.Run(path, func(t *testing.T) {
			response := fixture.request(http.MethodGet, path, "", false, "")
			if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("private route %s is accessible or cacheable: %d", path, response.Code)
			}
		})
	}
	for _, path := range []string{"/stremio/invalid/manifest.json", "/stremio/invalid/stream/movie/tt1234567.json", "/stremio/invalid/play/torbox/example/1/movie/tt1234567", "/api/catalogs/example"} {
		if response := fixture.request(http.MethodGet, path, "", false, ""); response.Code != http.StatusUnauthorized {
			t.Fatalf("missing installation or share credential was accepted at %s: %d", path, response.Code)
		}
	}
	if response := fixture.request(http.MethodGet, "/api/session", "", false, ""); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"authenticated":false`) || strings.Contains(response.Body.String(), "csrf_token") {
		t.Fatal("anonymous session response disclosed authentication state")
	}
	// The installation token is a separate, deliberately restricted credential.
	token, err := testPrivateInstallation(t, fixture.ctx, addon)
	if err != nil {
		t.Fatal(err)
	}
	if response := fixture.request(http.MethodGet, "/stremio/"+token+"/manifest.json", "", false, ""); response.Code != http.StatusOK {
		t.Fatal("valid installation credential no longer works without an admin session")
	}
	if response := fixture.request(http.MethodGet, "/api/providers", "", false, token); response.Code != http.StatusUnauthorized {
		t.Fatal("an installation credential granted administrator access")
	}
}
