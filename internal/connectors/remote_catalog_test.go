package connectors

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func remoteProvider(endpoint string) model.Provider {
	p := protocolProvider("http_json", endpoint)
	p.HTTP.Catalog = true
	p.Auth = model.Auth{Type: "bearer", SecretRef: "remote-password"}
	return p
}

func trustedCatalog(t *testing.T, server *httptest.Server) *httpJSONConnector {
	t.Helper()
	c := protocolOpen(t, remoteProvider(server.URL), model.ModeIncremental, Environment{Secrets: protocolSecrets, LocalInstanceID: "local-instance"}).(*httpJSONConnector)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	c.client.transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return c
}

func TestRemoteCatalogScopeResetResumeAndExactValues(t *testing.T) {
	origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "original-provider", SourceID: "original-record"}
	own := model.CatalogOrigin{InstanceID: "local-instance", ProviderID: "local-provider", SourceID: "local-record"}
	fields := map[string]any{"title": "  Exact title  ", "size": json.Number("9007199254740993"), "published_at": json.Number("1704164645123"), "category": nil, "categories": []any{json.Number("42"), "original"}}
	item := model.CatalogItem{ID: model.CatalogItemID(origin), Origin: origin, Fields: fields}
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-secret-value" || r.Method != http.MethodGet {
			t.Error("request did not use read-only bearer authentication")
		}
		if r.URL.Query().Get("limit") != "2" {
			t.Error("page bound was not sent")
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			if r.URL.Query().Get("checkpoint") != "previous-scope" {
				t.Error("new delta did not start from its captured checkpoint")
			}
			_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 1, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{item}, NextCursor: "resume-page"})
		case "resume-page":
			if r.URL.Query().Has("checkpoint") {
				t.Error("resume mixed durable checkpoint with per-run cursor")
			}
			_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 1, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{{ID: model.CatalogItemID(own), Origin: own, Fields: map[string]any{"title": "Own origin"}}}, Checkpoint: "new-scope"})
		default:
			t.Error("unexpected continuation")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	first, err := trustedCatalog(t, server).Fetch(t.Context(), protocolCursor(t, model.CatalogCursor{Version: 1, Checkpoint: "previous-scope"}))
	if err != nil || first.Done || !first.ResetStaging || len(first.Items) != 1 || !reflect.DeepEqual(first.Items[0].Fields, fields) || first.Items[0].Origin == nil || *first.Items[0].Origin != origin {
		t.Fatalf("scope reset did not preserve exact fields/provenance: %+v %v", first, err)
	}
	// A fresh connector must use only the run's saved continuation.
	last, err := trustedCatalog(t, server).Fetch(t.Context(), first.Next)
	if err != nil || !last.Done || last.ResetStaging || len(last.Items) != 1 || !last.Items[0].Ignored || len(last.Items[0].Raw) == 0 {
		t.Fatalf("resume or loop filtering failed: %+v %v", last, err)
	}
	var state model.CatalogCursor
	if err := json.Unmarshal(last.Next, &state); err != nil || state.Mode != "full" || state.Checkpoint != "new-scope" || !state.Done {
		t.Fatalf("completion state lost publication kind: %+v %v", state, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("unexpected requests: %d", requests.Load())
	}
}

func TestRemoteCatalogExpiredSnapshotRestartsOnlyWithExplicitProtocolCode(t *testing.T) {
	for _, code := range []string{model.CatalogSnapshotExpiredCode, "unrelated_failure"} {
		t.Run(code, func(t *testing.T) {
			var requests atomic.Int32
			origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "original", SourceID: "fresh"}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Query().Get("cursor") == "expired" {
					w.WriteHeader(http.StatusGone)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": "Expired"})
					return
				}
				if r.URL.Query().Has("cursor") || r.URL.Query().Has("checkpoint") {
					t.Error("fresh Full retained old traversal parameters")
				}
				_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 1, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{{ID: model.CatalogItemID(origin), Origin: origin, Fields: map[string]any{"title": "Fresh catalog"}}}, Checkpoint: "fresh-checkpoint"})
			}))
			defer server.Close()
			previous := model.CatalogCursor{Version: 1, InstanceID: "owner", Mode: "incremental", Cursor: "expired", BaseEndpoint: server.URL, BaseCheckpoint: "committed-before-run"}
			page, err := trustedCatalog(t, server).Fetch(t.Context(), protocolCursor(t, previous))
			if code != model.CatalogSnapshotExpiredCode {
				if err == nil || page.ResetStaging || requests.Load() != 1 {
					t.Fatalf("unrelated failure reset collection: %+v %v", page, err)
				}
				return
			}
			if err != nil || !page.Done || !page.ResetStaging || requests.Load() != 2 || page.Metadata["catalog_snapshot_restarted"] != true {
				t.Fatalf("expired snapshot did not restart once: %+v %v", page, err)
			}
			var next model.CatalogCursor
			if err = json.Unmarshal(page.Next, &next); err != nil || next.Mode != "full" || next.Checkpoint != "fresh-checkpoint" || next.BaseEndpoint != previous.BaseEndpoint || next.BaseCheckpoint != previous.BaseCheckpoint {
				t.Fatalf("restart lost publication fence: %+v %v", next, err)
			}
		})
	}
}

func TestRemoteCatalogRejectsRedirectsAndUntrustedCertificates(t *testing.T) {
	var forwarded atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/credential-target" {
			forwarded.Add(1)
		}
		http.Redirect(w, r, "/credential-target", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	untrusted := protocolOpen(t, remoteProvider(server.URL), model.ModeFull, Environment{Secrets: protocolSecrets, LocalInstanceID: "local-instance"})
	if _, err := untrusted.Fetch(t.Context(), nil); err == nil {
		t.Fatal("untrusted TLS certificate was accepted")
	}
	page, err := trustedCatalog(t, server).Fetch(t.Context(), nil)
	if err == nil || page.Done || forwarded.Load() != 0 {
		t.Fatalf("redirect forwarded credentials or certified a response: %+v %v requests=%d", page, err, forwarded.Load())
	}
}

func TestRemoteCatalogRejectsUnsafeDestinationsWithoutRequest(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/api/catalogs/share", "https://user:password@example.com/", "https://example.com/?token=secret", "https://example.com/#fragment", "https://169.254.169.254/", "https://[::ffff:169.254.169.254]/", "https://[::]/", "https://224.0.0.1/", "https://100.100.100.200/"} {
		if err := Validate(remoteProvider(endpoint)); err == nil {
			t.Errorf("unsafe catalogue destination accepted: %s", endpoint)
		}
	}
	if connection, err := catalogDial(t.Context(), "tcp", "169.254.169.254:443"); err == nil || connection != nil {
		t.Fatal("dial-time metadata address check was bypassed")
	}
}

func TestRemoteCatalogRejectsProtocolViolationsRetainingWireBytes(t *testing.T) {
	origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "provider", SourceID: "record"}
	valid := model.CatalogEnvelope{Version: 1, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{{ID: model.CatalogItemID(origin), Origin: origin, Fields: map[string]any{"title": "Exact"}}}, Checkpoint: "completed"}
	cases := []struct {
		name   string
		change func(*model.CatalogEnvelope)
	}{
		{"unsupported version", func(e *model.CatalogEnvelope) { e.Version = 99 }},
		{"forged identity", func(e *model.CatalogEnvelope) { e.Items[0].ID = "forged" }},
		{"full tombstone", func(e *model.CatalogEnvelope) { e.Items[0].Deleted = true; e.Items[0].Fields = nil }},
		{"ambiguous completion", func(e *model.CatalogEnvelope) { e.NextCursor = "next" }},
		{"unexpected delta", func(e *model.CatalogEnvelope) { e.Mode = "incremental" }},
		{"unsafe nested field", func(e *model.CatalogEnvelope) {
			e.Items[0].Fields = map[string]any{"title": map[string]any{"secret": "value"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := valid
			envelope.Items = append([]model.CatalogItem(nil), valid.Items...)
			tc.change(&envelope)
			body, _ := json.Marshal(envelope)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
			t.Cleanup(server.Close)
			page, err := trustedCatalog(t, server).Fetch(t.Context(), nil)
			if err == nil || page.Done || len(page.Next) != 0 || !bytes.Equal(page.Body, body) || len(page.Items) != 1 || len(page.Items[0].Raw) == 0 {
				t.Fatalf("invalid response lost bytes or advanced continuation: %+v %v", page, err)
			}
		})
	}
}

func TestRemoteCatalogReturnsRetryableResponseBeforeRetrying(t *testing.T) {
	failure := []byte(`{"error":"temporary upstream response","original":"unmodified"}`)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(failure)
			return
		}
		_ = json.NewEncoder(w).Encode(model.CatalogEnvelope{Version: 1, InstanceID: "owner", Mode: "full", Items: []model.CatalogItem{}, Checkpoint: "completed"})
	}))
	t.Cleanup(server.Close)
	connector := trustedCatalog(t, server)
	page, err := connector.Fetch(t.Context(), nil)
	if err == nil || !bytes.Equal(page.Body, failure) || page.Done || len(page.Next) != 0 {
		t.Fatalf("retry discarded the response before archival: %+v %v", page, err)
	}
	// A resumed attempt starts from the same durable cursor only after the
	// caller has had the opportunity to persist the failed response.
	resumed, err := connector.Fetch(t.Context(), nil)
	if err != nil || !resumed.Done || len(resumed.Next) == 0 {
		t.Fatalf("explicit retry did not complete: %+v %v", resumed, err)
	}
}
