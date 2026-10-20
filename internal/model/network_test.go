package model

import (
	"encoding/json"
	"testing"
)

func TestPublicLimitsRequireExplicitValuesBeforeRemovingRestrictions(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"setup_per_minute":0,"requests_per_minute":0}`, `{"setup_per_minute":0,"requests_per_minute":0,"concurrent":null}`, `{"setup_per_minute":0,"requests_per_minute":0,"concurrent":0,"unknown":1}`} {
		var limits PublicLimits
		if json.Unmarshal([]byte(raw), &limits) == nil {
			t.Fatal("ambiguous limits silently removed a restriction")
		}
	}
	var limits PublicLimits
	if err := json.Unmarshal([]byte(`{"setup_per_minute":0,"requests_per_minute":0,"concurrent":0}`), &limits); err != nil || !limits.Valid() {
		t.Fatal("explicit unlimited values were rejected", err)
	}
}

func TestNetworkSettingsRejectCredentialsAndInvalidRoutes(t *testing.T) {
	base := NetworkSettings{Revision: 1, Default: NetworkSystem, Proxies: []NetworkProxy{{ID: "warp", Name: "WARP", URL: "socks5://127.0.0.1:40000", Enabled: true}}, Overrides: map[string]string{}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{
		"http://user:private@proxy.example:8080", "http://proxy.example/?token=private", "http://proxy.example/path", "http://proxy.example/#fragment", "ftp://proxy.example", "http://proxy.example:0", "http://proxy.example:65536", "http://proxy.example/?", "http://[fe80::1%25eth0]:8080", " http://proxy.example:8080",
	} {
		input := base
		input.Proxies = append([]NetworkProxy(nil), base.Proxies...)
		input.Proxies[0].URL = address
		if input.Validate() == nil {
			t.Error("Accepted a credential-bearing or unsupported proxy URL")
		}
	}
	for _, address := range []string{"http://proxy:8080", "https://proxy.example", "socks5h://[::1]:40000"} {
		input := base
		input.Proxies = append([]NetworkProxy(nil), base.Proxies...)
		input.Proxies[0].URL = address
		if err := input.Validate(); err != nil {
			t.Error("Rejected a supported proxy address")
		}
	}
	for target, route := range map[string]string{"source:valid": "missing", "http://arbitrary.example": "warp", "metadata:": "warp"} {
		input := base
		input.Overrides = map[string]string{target: route}
		if input.Validate() == nil {
			t.Error("Accepted an invalid connection assignment")
		}
	}
	input := base
	input.Proxies = append(append([]NetworkProxy(nil), base.Proxies...), base.Proxies[0])
	if input.Validate() == nil {
		t.Fatal("Accepted duplicate proxy IDs")
	}
}

func TestNetworkDownloadRoutingUsesOnlyExplicitOverrides(t *testing.T) {
	s := NetworkSettings{Default: "shared", Overrides: map[string]string{"debrid:alldebrid": "warp", "source:local": NetworkDirect}}
	for target, expected := range map[string]string{"debrid:alldebrid": "warp", "download:alldebrid": "warp", "download:torbox": "shared", "metadata:future": "shared", "source:new": "shared", "source:local": NetworkDirect} {
		if s.Route(target) != expected {
			t.Error("Routing did not preserve its scoped inheritance")
		}
	}
	s.Overrides["downloads"] = NetworkDirect
	if s.Route("download:alldebrid") != NetworkDirect {
		t.Fatal("An explicit browser-download override was ignored")
	}
}
