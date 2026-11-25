package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestInspectTorznabCapabilitiesEndpoint(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	const apiKey = "vault-torznab-api-key"
	if response := fixture.request(http.MethodPut, "/api/secrets/demo_torznab_key", `{"value":"`+apiKey+`","revision":0}`, true, ""); response.Code != http.StatusNoContent {
		t.Fatalf("create secret: status %d", response.Code)
	}

	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" || r.URL.Query().Get("t") != "caps" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("apikey") != apiKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<caps>
			<server title="Cinema Index"/>
			<limits default="50" max="150"/>
			<searching>
				<search available="yes" supportedParams="q"/>
				<movie-search available="yes" supportedParams="q,imdbid"/>
			</searching>
			<categories>
				<category id="2000" name="Movies"><subcat id="2040" name="HD"/></category>
			</categories>
		</caps>`))
	}))
	defer indexer.Close()

	// Unauthenticated request must be rejected.
	payload, err := json.Marshal(map[string]string{
		"json": `{"version":1,"id":"","name":"","adapter":"torznab","url":"` + indexer.URL + `","enabled":false,"auth":{"type":"query","name":"apikey","secret_ref":"demo_torznab_key"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unauth := fixture.request(http.MethodPost, "/api/providers/torznab-capabilities", string(payload), false, ""); unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauth.Code)
	}

	// Authenticated request discovers /api and resolves the Vault secret.
	response := fixture.request(http.MethodPost, "/api/providers/torznab-capabilities", string(payload), true, "")
	if response.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d, body = %s", response.Code, response.Body.String())
	}
	var result model.TorznabCapabilitiesResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Endpoint != indexer.URL+"/api" || result.Title != "Cinema Index" || result.SuggestedID != "cinema_index" {
		t.Fatalf("unexpected identity discovery: %+v", result)
	}
	if result.RecommendedPageSize != 150 || result.RecommendedMode != "search" {
		t.Fatalf("unexpected recommendations: %+v", result)
	}
	if len(result.Categories) != 1 || len(result.Categories[0].Subcategories) != 1 || result.Categories[0].Subcategories[0].ID != 2040 {
		t.Fatalf("unexpected categories: %+v", result.Categories)
	}

	// Missing secret returns 422 with clear error and no secret leaks.
	missingPayload, _ := json.Marshal(map[string]string{
		"json": `{"version":1,"adapter":"torznab","url":"` + indexer.URL + `/api","auth":{"type":"query","name":"apikey","secret_ref":"nonexistent_secret"}}`,
	})
	missing := fixture.request(http.MethodPost, "/api/providers/torznab-capabilities", string(missingPayload), true, "")
	if missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing secret status = %d, body = %s", missing.Code, missing.Body.String())
	}
	if !strings.Contains(missing.Body.String(), `"missing_secret"`) {
		t.Fatalf("expected missing_secret code, got %s", missing.Body.String())
	}

	// Non-torznab definition returns 422.
	nonTorznabPayload, _ := json.Marshal(map[string]string{
		"json": `{"version":1,"adapter":"json","url":"` + indexer.URL + `/api"}`,
	})
	nonTorznab := fixture.request(http.MethodPost, "/api/providers/torznab-capabilities", string(nonTorznabPayload), true, "")
	if nonTorznab.Code != http.StatusUnprocessableEntity {
		t.Fatalf("non-torznab status = %d, body = %s", nonTorznab.Code, nonTorznab.Body.String())
	}
}
