package aiosource

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestNetworkOverridesCoverMetadataAndIndependentDebridServices(t *testing.T) {
	ctx, service, _ := testService(t)
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","success":true,"data":{}}`)
	}))
	defer origin.Close()
	service.Providers.ADBase, service.Providers.TBBase = origin.URL, origin.URL
	settings, err := service.DB.NetworkSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Proxies = []model.NetworkProxy{{ID: "blocked", Name: "Disabled", URL: "socks5://127.0.0.1:1", Enabled: false}}
	settings.Overrides = map[string]string{"debrid:alldebrid": "blocked", "metadata:tmdb": "blocked"}
	if err := service.DB.UpdateNetworkSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Providers.request(ctx, "alldebrid", "synthetic-key", http.MethodGet, "status", nil, ""); err == nil || requests.Load() != 0 {
		t.Fatal("AllDebrid did not use its selected connection")
	}
	if _, err := service.MetadataHTTP.Get(origin.URL); err == nil || requests.Load() != 0 {
		t.Fatal("Metadata or export requests ignored their assigned connection")
	}
	if _, err := service.Providers.request(ctx, "torbox", "synthetic-key", http.MethodGet, "status", nil, ""); err != nil || requests.Load() != 1 {
		t.Fatal("An AllDebrid override changed the TorBox connection")
	}
	settings, err = service.DB.NetworkSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Overrides["debrid:alldebrid"] = model.NetworkDirect
	if err := service.DB.UpdateNetworkSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Providers.request(ctx, "alldebrid", "synthetic-key", http.MethodGet, "status", nil, ""); err != nil || requests.Load() != 2 {
		t.Fatal("A saved routing change did not take effect without restarting")
	}
}
