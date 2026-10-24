package connectors

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRemoteCatalogAdditionalDataNegotiationAndOldPeerFallback(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(map[int]string{1: "older peer", 2: "shared data"}[version], func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(model.CatalogEntitiesHeader) != "metadata,availability" || r.URL.Query().Has("include") {
					t.Error("optional capabilities were not negotiated compatibly")
				}
				e := model.CatalogEnvelope{Version: version, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{}, Checkpoint: "done"}
				if version == 2 {
					e.Entities = model.CatalogEntities{Metadata: true}
					e.Supplement.Metadata = []model.CatalogMetadata{model.CatalogWork(model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Example Film"}, DisplayTitle: "Example Film"}, time.Now())}
				}
				_ = json.NewEncoder(w).Encode(e)
			}))
			defer server.Close()
			c := trustedCatalog(t, server)
			c.provider.CatalogEntities = model.CatalogEntities{Metadata: true, Availability: true}
			page, err := c.Fetch(t.Context(), nil)
			if err != nil || !page.Done || !page.ResetStaging {
				t.Fatalf("compatible shared import failed: %+v %v", page, err)
			}
			if len(page.CatalogSupplement.Metadata) != version-1 {
				t.Fatal("negotiation lost or invented metadata")
			}
		})
	}
}

func TestRemoteCatalogAdditionalDataRejectsSecretsAndUnrequestedEntities(t *testing.T) {
	for _, test := range []struct {
		name, body string
		selection  model.CatalogEntities
	}{
		{"not requested", `{"metadata":[{"type":"movie"}]}`, model.CatalogEntities{}},
		{"unknown fields", `{"api_key":"private"}`, model.CatalogEntities{Metadata: true, Availability: true}},
		{"private link", `{"availability":[{"service":"torbox","hash":"0123456789abcdef0123456789abcdef01234567","checked_at":"2026-01-01T00:00:00Z","files":[{"path":"file.mkv","size":1,"link":"https://private.invalid/"}]}]}`, model.CatalogEntities{Metadata: true, Availability: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := []byte(`{"version":2,"instance_id":"owner","mode":"full","entities":{"metadata":true,"availability":true},"items":[],"supplement":` + test.body + `,"checkpoint":"must-not-advance"}`)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(wire) }))
			defer server.Close()
			c := trustedCatalog(t, server)
			c.provider.CatalogEntities = test.selection
			previous := protocolCursor(t, model.CatalogCursor{Version: 1, Checkpoint: "last-good"})
			page, err := c.Fetch(t.Context(), previous)
			if err == nil || page.Done || !bytes.Equal(page.Next, previous) || !bytes.Equal(page.Body, wire) {
				t.Fatal("invalid shared data advanced or lost diagnostics")
			}
		})
	}
}

func TestRemoteCatalogTorrentDetailsVersionNegotiationAndSafety(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "source", SourceID: "opaque"}
			item := model.CatalogItem{ID: model.CatalogItemID(origin), Origin: origin, Fields: map[string]any{"title": "Example Series"}}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(model.CatalogEntitiesHeader) != "metadata" || r.Header.Get(model.CatalogDetailsHeader) != "identifiers,languages,technical,files" {
					t.Error("new details broke backward-compatible negotiation")
				}
				e := model.CatalogEnvelope{Version: version, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{item}, Checkpoint: "done"}
				if version >= 2 {
					e.Entities.Metadata = true
				}
				if version == 3 {
					e.Entities.Identifiers = true
					e.Items[0].Details = map[string]any{"tmdb_id": json.Number("9007199254740993"), "season": "1", "episode": "2"}
				}
				_ = json.NewEncoder(w).Encode(e)
			}))
			defer server.Close()
			c := trustedCatalog(t, server)
			c.provider.CatalogEntities = model.CatalogEntities{Metadata: true, Identifiers: true, Languages: true, Technical: true, Files: true}
			page, err := c.Fetch(t.Context(), nil)
			if err != nil || len(page.Items) != 1 || !page.Done {
				t.Fatal("compatible import failed", err)
			}
			if version == 3 && page.Items[0].Fields["tmdb_id"] != json.Number("9007199254740993") {
				t.Fatal("inline details were dropped or rounded")
			}
		})
	}
	for _, test := range []struct {
		name        string
		fields      map[string]any
		permissions model.CatalogEntities
	}{
		{"not requested", map[string]any{"language": "fr"}, model.CatalogEntities{}},
		{"nested secret", map[string]any{"attributes": map[string]any{"api_key": "private"}}, model.CatalogEntities{Identifiers: true}},
		{"subtitle url", map[string]any{"subtitles": []any{map[string]any{"language": "fr", "url": "https://private.invalid/"}}}, model.CatalogEntities{Languages: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "source", SourceID: "opaque"}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 3, InstanceID: "owner", Mode: "full", Entities: test.permissions,
					Items: []model.CatalogItem{{ID: model.CatalogItemID(origin), Origin: origin, Fields: map[string]any{"title": "Example"}, Details: test.fields}}, Checkpoint: "must-not-advance"})
			}))
			defer server.Close()
			c := trustedCatalog(t, server)
			c.provider.CatalogEntities = test.permissions
			previous := protocolCursor(t, model.CatalogCursor{Version: 1, Checkpoint: "last-good"})
			page, err := c.Fetch(t.Context(), previous)
			if err == nil || page.Done || !bytes.Equal(page.Next, previous) {
				t.Fatal("unsafe/unrequested torrent facts advanced the checkpoint")
			}
		})
	}
}

func TestRemoteCatalogDetailDeltasIgnoreTheirOwnOrigin(t *testing.T) {
	origin := model.CatalogOrigin{InstanceID: "local", ProviderID: "source", SourceID: "opaque"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 3, InstanceID: "owner", Mode: "full", Entities: model.CatalogEntities{Languages: true}, Items: []model.CatalogItem{},
			Supplement: model.CatalogSupplement{Torrents: []model.CatalogTorrentDetails{{ID: model.CatalogItemID(origin), Origin: origin, BaseFingerprint: model.CatalogFieldsFingerprint(map[string]any{}), Fields: map[string]any{"language": "fr"}}}}, Checkpoint: "done"})
	}))
	defer server.Close()
	c := trustedCatalog(t, server)
	c.localInstanceID = "local"
	c.provider.CatalogEntities.Languages = true
	page, err := c.Fetch(t.Context(), nil)
	if err != nil || !page.Done || len(page.CatalogSupplement.Torrents) != 0 {
		t.Fatal("detail deltas bypassed replication loop protection", err)
	}
}
