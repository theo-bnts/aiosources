package inbound

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTrustedHeadersAndChainsNeverTrustUnapprovedPeers(t *testing.T) {
	for _, tc := range []struct {
		name, peer, header, format, value, want string
		extra                                   bool
	}{
		{"cloudflare", "10.0.0.2:1234", "CF-Connecting-IP", "single", "203.0.113.9", "203.0.113.9", false},
		{"direct spoof", "192.0.2.5:1234", "CF-Connecting-IP", "single", "203.0.113.9", "192.0.2.5", false},
		{"real ip", "10.0.0.2:1234", "X-Real-IP", "single", "2001:db8::1", "2001:db8::1", false},
		{"mapped ip", "[::ffff:10.0.0.2]:1234", "X-Real-IP", "single", "::ffff:203.0.113.9", "203.0.113.9", false},
		{"missing", "10.0.0.2:1234", "X-Real-IP", "single", "", "10.0.0.2", false},
		{"invalid", "10.0.0.2:1234", "X-Real-IP", "single", "invalid", "10.0.0.2", false},
		{"duplicate single", "10.0.0.2:1234", "X-Real-IP", "single", "203.0.113.9", "10.0.0.2", true},
		{"comma single", "10.0.0.2:1234", "CF-Connecting-IP", "single", "203.0.113.9, 192.0.2.5", "10.0.0.2", false},
		{"forwarded chain", "10.0.0.2:1234", "X-Forwarded-For", "chain", "203.0.113.9, 10.0.0.3", "203.0.113.9", false},
		{"injected leftmost", "10.0.0.2:1234", "X-Forwarded-For", "chain", "198.51.100.7, 203.0.113.9, 10.0.0.3", "203.0.113.9", false},
		{"untrusted intermediate", "10.0.0.2:1234", "X-Forwarded-For", "chain", "203.0.113.9, 192.0.2.5", "192.0.2.5", false},
		{"empty chain item", "10.0.0.2:1234", "X-Forwarded-For", "chain", "203.0.113.9,,10.0.0.3", "10.0.0.2", false},
		{"duplicate chain", "10.0.0.2:1234", "X-Forwarded-For", "chain", "203.0.113.9", "203.0.113.9", true},
		{"zone", "10.0.0.2:1234", "X-Real-IP", "single", "fe80::1%eth0", "10.0.0.2", false},
		{"huge chain", "10.0.0.2:1234", "X-Forwarded-For", "chain", strings.Repeat("10.0.0.3,", 1000), "10.0.0.2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(&model.IncomingNetwork{Enabled: true, IPHeader: tc.header, Format: tc.format, TrustedProxies: []string{"10.0.0.0/24"}})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set(tc.header, tc.value)
			if tc.extra {
				req.Header.Add(tc.header, "10.0.0.3")
			}
			if got := r.ClientIP(req); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestIncomingPolicyValidationAndDisabledTrust(t *testing.T) {
	for _, change := range []func(*model.IncomingNetwork){
		func(p *model.IncomingNetwork) { p.TrustedProxies = nil },
		func(p *model.IncomingNetwork) { p.TrustedProxies = []string{"0.0.0.0/0"} },
		func(p *model.IncomingNetwork) { p.TrustedProxies = []string{"::/0"} },
		func(p *model.IncomingNetwork) { p.TrustedProxies = []string{"fe80::1%eth0"} },
		func(p *model.IncomingNetwork) { p.TrustedProxies = []string{"proxy.example"} },
		func(p *model.IncomingNetwork) { p.IPHeader = "X-Real-IP\r\nOther" },
		func(p *model.IncomingNetwork) { p.Format = "regex" },
	} {
		p := model.IncomingNetwork{Enabled: true, IPHeader: "X-Real-IP", Format: "single", TrustedProxies: []string{"127.0.0.1"}}
		change(&p)
		if p.Validate() == nil {
			t.Fatal("unsafe incoming policy was accepted")
		}
	}
	r, err := New(&model.IncomingNetwork{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.5:1234"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if r.ClientIP(req) != "192.0.2.5" {
		t.Fatal("disabled trust accepted a header")
	}
}
