package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

func TestSourceConnectorsUseTheirAssignedConnection(t *testing.T) {
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer origin.Close()
	for _, adapter := range []string{"http_json", "torznab"} {
		t.Run(adapter, func(t *testing.T) {
			settings := model.NetworkSettings{Revision: 1, Default: model.NetworkDirect, Proxies: []model.NetworkProxy{{ID: "blocked", Name: "Disabled proxy", URL: "socks5://127.0.0.1:1", Enabled: false}}, Overrides: map[string]string{"source:sample": "blocked"}}
			router := &outbound.Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
			provider := model.Provider{Version: 1, ID: "sample", Name: "Sample", Adapter: adapter, URL: origin.URL, Enabled: true, RequestInterval: "1ms", HTTP: model.HTTPConfig{ItemsPath: "/items"}, Mapping: model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title"}}}
			if adapter == "torznab" {
				provider.HTTP, provider.Mapping = model.HTTPConfig{}, model.Mapping{}
			}
			source, err := New(context.Background(), provider, model.ModePreview, Environment{Network: router})
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := source.Fetch(ctx, nil); err == nil || requests.Load() != 0 {
				t.Fatal("The source connector ignored its proxy assignment")
			}
		})
	}
}

func TestRemoteDestinationCheckPrecedesProxyConnection(t *testing.T) {
	var requests atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer relay.Close()
	settings := model.NetworkSettings{Revision: 1, Default: "relay", Proxies: []model.NetworkProxy{{ID: "relay", Name: "Relay", URL: relay.URL, Enabled: true}}}
	router := &outbound.Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	base := outbound.HTTPTransport()
	base.Proxy, base.DialContext = nil, catalogDial
	client := &http.Client{Transport: router.Transport("source:remote", base), Timeout: time.Second}
	defer client.CloseIdleConnections()
	if _, err := client.Get("https://169.254.169.254/"); err == nil || requests.Load() != 0 {
		t.Fatal("Remote address protection was bypassed by proxy routing")
	}
}
