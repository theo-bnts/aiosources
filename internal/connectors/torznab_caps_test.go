package connectors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestInspectTorznabCapabilitiesDiscoversEndpointAndMetadata(t *testing.T) {
	const apiKey = "super-secret-torznab-key-99"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body>Not a caps document</body></html>`))
			return
		}
		if r.URL.Path != "/api" || r.URL.Query().Get("t") != "caps" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("apikey") != apiKey {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<error code="100" description="Bad key ` + r.URL.Query().Get("apikey") + `"/>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Header().Set("X-RateLimit-Limit", "500")
		w.Header().Set("X-RateLimit-Remaining", "499")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Médiathèque &amp; Archive"/>
  <limits default="50" max="200"/>
  <searching>
    <search available="yes" supportedParams="q,cat"/>
    <tv-search available="yes" supportedParams="q,tvdbid,season,ep"/>
    <movie-search available="no" supportedParams="imdbid"/>
    <audio-search available="yes" supportedParams="artist,album"/>
    <book-search available="no" supportedParams=""/>
  </searching>
  <categories>
    <category id="2000" name="Movies">
      <subcat id="2040" name="HD"/>
      <subcat id="2045" name="UHD"/>
    </category>
    <category id="5000" name="TV">
      <subcat id="5070" name="Anime"/>
    </category>
  </categories>
</caps>`))
	}))
	defer server.Close()

	// Incomplete new draft: empty name, empty id, empty categories, root URL (without /api).
	result, err := InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "torznab",
		URL:     server.URL,
		Auth: model.Auth{
			Type:      "query",
			Name:      "apikey",
			SecretRef: "tracker_key",
		},
	}, Environment{
		Secrets: func(_ context.Context, ref string) (string, error) {
			if ref == "tracker_key" {
				return apiKey, nil
			}
			return "", model.ErrNotFound
		},
	})
	if err != nil {
		t.Fatalf("InspectTorznabCapabilities failed: %v", err)
	}
	if result.Endpoint != server.URL+"/api" {
		t.Fatalf("endpoint = %q, want %q", result.Endpoint, server.URL+"/api")
	}
	if result.Title != "Médiathèque & Archive" {
		t.Fatalf("title = %q", result.Title)
	}
	if result.SuggestedID != "mediatheque_archive" {
		t.Fatalf("suggested_id = %q, want %q", result.SuggestedID, "mediatheque_archive")
	}
	if result.Limits != (model.TorznabLimits{Default: 50, Max: 200}) {
		t.Fatalf("limits = %+v", result.Limits)
	}
	if result.RecommendedPageSize != 200 {
		t.Fatalf("recommended_page_size = %d, want 200", result.RecommendedPageSize)
	}
	if result.RecommendedMode != "search" {
		t.Fatalf("recommended_search_mode = %q, want search", result.RecommendedMode)
	}
	wantModes := []string{"search", "tvsearch", "movie", "music", "book"}
	gotModes := make([]string, len(result.Searches))
	for i, s := range result.Searches {
		gotModes[i] = s.Mode
	}
	if !reflect.DeepEqual(gotModes, wantModes) {
		t.Fatalf("search modes = %v, want %v", gotModes, wantModes)
	}
	if result.Searches[2].Available {
		t.Fatalf("expected movie mode to be unavailable")
	}
	if len(result.Categories) != 2 || len(result.Categories[0].Subcategories) != 2 || result.Categories[1].Subcategories[0].ID != 5070 {
		t.Fatalf("unexpected categories: %+v", result.Categories)
	}
	if result.RateLimit == nil || result.RateLimit.Limit == nil || *result.RateLimit.Limit != 500 || result.RateLimit.Remaining == nil || *result.RateLimit.Remaining != 499 {
		t.Fatalf("unexpected rate limit: %+v", result.RateLimit)
	}
}

func TestInspectTorznabCapabilitiesPreservesNonReservedQueryAndCleansCapsParam(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" || r.URL.Query().Get("site") != "films" || r.URL.Query().Get("t") != "caps" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<caps><server title=""/><limits default="25"/><searching><tv-search available="yes" supportedParams="q,season,ep"/></searching></caps>`))
	}))
	defer server.Close()

	result, err := InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "torznab",
		URL:     server.URL + "/api.php?site=films&t=caps",
		Auth:    model.Auth{Type: "none"},
	}, Environment{})
	if err != nil {
		t.Fatalf("InspectTorznabCapabilities failed: %v", err)
	}
	if result.Endpoint != server.URL+"/api.php?site=films" {
		t.Fatalf("endpoint = %q, want %q", result.Endpoint, server.URL+"/api.php?site=films")
	}
	if result.RecommendedPageSize != 25 {
		t.Fatalf("recommended_page_size = %d, want 25", result.RecommendedPageSize)
	}
	if result.RecommendedMode != "tvsearch" {
		t.Fatalf("recommended_search_mode = %q, want tvsearch", result.RecommendedMode)
	}
	if !model.ValidSourceID(result.SuggestedID) {
		t.Fatalf("fallback suggested_id %q is invalid", result.SuggestedID)
	}
	if id := suggestTorznabSourceID("Settings", "https://movies.tracker.test/api"); id != "movies_tracker_test" {
		t.Fatalf("reserved title suggested_id = %q, want movies_tracker_test", id)
	}
	if id := suggestTorznabSourceID("New", "https://cinema.example.org/api"); id != "cinema_example_org" {
		t.Fatalf("reserved title suggested_id = %q, want cinema_example_org", id)
	}
}

func TestInspectTorznabCapabilitiesErrorsDoNotLeakSecrets(t *testing.T) {
	const wrongKey = "never-echo-this-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<error code="100" description="Invalid apikey ` + wrongKey + `"/>`))
	}))
	defer server.Close()

	// Auth failure from Torznab APIError must never leak Description or secret value.
	_, err := InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "torznab",
		URL:     server.URL + "/api",
		Auth:    model.Auth{Type: "query", Name: "apikey", SecretRef: "bad_key"},
	}, Environment{
		Secrets: func(_ context.Context, _ string) (string, error) {
			return wrongKey, nil
		},
	})
	var capErr *TorznabCapabilityError
	if !errors.As(err, &capErr) {
		t.Fatalf("expected TorznabCapabilityError, got %v", err)
	}
	if capErr.Code != "authentication" || capErr.Status != http.StatusBadGateway {
		t.Fatalf("got code=%q status=%d", capErr.Code, capErr.Status)
	}
	if strings.Contains(capErr.Message, wrongKey) {
		t.Fatalf("error message leaked secret: %q", capErr.Message)
	}

	// Missing secret yields clear missing_secret error.
	_, err = InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "torznab",
		URL:     server.URL + "/api",
		Auth:    model.Auth{Type: "query", Name: "apikey", SecretRef: "missing_key"},
	}, Environment{
		Secrets: func(_ context.Context, _ string) (string, error) {
			return "", model.ErrNotFound
		},
	})
	if !errors.As(err, &capErr) || capErr.Code != "missing_secret" || capErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected missing_secret 422, got %v", err)
	}

	// Credential in URL is rejected safely.
	_, err = InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "torznab",
		URL:     server.URL + "/api?apikey=" + wrongKey,
		Auth:    model.Auth{Type: "none"},
	}, Environment{})
	if !errors.As(err, &capErr) || capErr.Code != "configuration" || strings.Contains(capErr.Message, wrongKey) {
		t.Fatalf("expected safe configuration error, got %v", err)
	}

	// Non-torznab adapter rejected.
	_, err = InspectTorznabCapabilities(context.Background(), model.Provider{
		Adapter: "json",
		URL:     server.URL + "/api",
	}, Environment{})
	if !errors.As(err, &capErr) || capErr.Code != "configuration" {
		t.Fatalf("expected configuration error for non-torznab, got %v", err)
	}
}
