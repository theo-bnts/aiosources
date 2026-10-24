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
