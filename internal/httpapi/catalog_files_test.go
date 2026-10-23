package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

type catalogFileRoundTrip func(*http.Request) (*http.Response, error)

func (f catalogFileRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCatalogFileDestinationGuardPrecedesProxyConnection(t *testing.T) {
	var requests atomic.Int64
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer relay.Close()
	settings := model.NetworkSettings{Revision: 1, Default: "relay", Proxies: []model.NetworkProxy{{ID: "relay", Name: "Relay", URL: relay.URL, Enabled: true}}}
	router := &outbound.Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	client := newCatalogFileClient()
	client.Transport = router.Transport("downloads", client.Transport.(*http.Transport))
	client.Timeout = time.Second
	defer client.CloseIdleConnections()
	if _, err := client.Get("https://127.0.0.1/file"); err == nil || requests.Load() != 0 {
		t.Fatal("Download address protection was bypassed by proxy routing")
	}
}

func TestCatalogFileAttachmentsAreSessionBoundAndStreamWithoutProviderCredentials(t *testing.T) {
	f, addon, _ := addonAPI(t)
	auth, err := newAuthentication("synthetic-admin-password", "https://addon.example", f.db, addon.Vault)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s := &server{options: Options{Store: f.db, Vault: addon.Vault}, auth: auth, files: make(chan struct{}, 1), fileClient: &http.Client{Transport: catalogFileRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" || r.Header.Get("Range") != "bytes=2-4" {
			t.Fatal("unsafe download headers", r.Header)
		}
		return &http.Response{StatusCode: 206, Body: io.NopCloser(strings.NewReader("abc")), Header: http.Header{"Content-Type": []string{"video/mp4"}, "Content-Length": []string{"3"}, "Content-Range": []string{"bytes 2-4/6"}, "Content-Disposition": []string{`inline; filename="example.mp4"`}, "Set-Cookie": []string{"provider-secret=private"}}}, nil
	})}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /issue", auth.protect(func(w http.ResponseWriter, r *http.Request) {
		s.writeCatalogDownload(w, r, model.CatalogDownload{Status: "ready", URL: "https://cdn.example/ready", OperationID: "example"}, nil)
	}))
	mux.HandleFunc("GET /api/catalog-downloads/{token}", auth.protect(s.catalogFile))
	f.handler = securityHeaders(mux)
	issued := f.request("GET", "/issue", "", true, "")
	var result model.CatalogDownload
	if issued.Code != 200 || json.Unmarshal(issued.Body.Bytes(), &result) != nil || !strings.HasPrefix(result.URL, "/api/catalog-downloads/") || strings.Contains(issued.Body.String(), "cdn.example") {
		t.Fatal("file ticket not issued safely", issued.Code)
	}
	if w := f.request("GET", result.URL, "", false, ""); w.Code != 401 {
		t.Fatal("unauthenticated file access", w.Code)
	}
	request := httptest.NewRequest("GET", result.URL, nil).WithContext(f.ctx)
	request.AddCookie(f.cookie)
	request.Header.Set("Range", "bytes=2-4")
	request.Header.Set("Authorization", "Bearer must-not-leak")
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if response.Code != 206 || response.Body.String() != "abc" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") || response.Header().Get("Content-Range") != "bytes 2-4/6" || response.Header().Get("Set-Cookie") != "" || response.Header().Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'none'; frame-ancestors 'self'") || calls != 1 {
		t.Fatal("attachment delivery changed", response.Code, response.Header())
	}
	for _, ticket := range []catalogFileTicket{{URL: "https://cdn.example/ready", Session: "different-session", Expires: time.Now().Add(time.Minute).Unix()}, {URL: "https://cdn.example/ready", Expires: 1}} {
		data, _ := json.Marshal(ticket)
		sealed, _ := addon.Vault.Seal(catalogFilePurpose, data)
		if w := f.request("GET", "/api/catalog-downloads/"+base64.RawURLEncoding.EncodeToString(sealed), "", true, ""); w.Code != 404 {
			t.Fatal("invalid file ticket accepted", w.Code)
		}
	}
	if w := f.request("GET", result.URL+"x", "", true, ""); w.Code != 404 {
		t.Fatal("tampered file ticket accepted", w.Code)
	}
	if calls != 1 {
		t.Fatal("rejected ticket contacted a file server")
	}
}

func TestCatalogFileNetworkPolicyRejectsPrivateAndRedirectedDestinations(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.1.2.3", "192.168.1.1", "169.254.169.254", "100.100.100.200", "100.64.1.1", "198.18.0.1", "2001:db8::1", "2002:7f00:1::", "64:ff9b::7f00:1", "fe80::1", "224.0.0.1", "0.0.0.0"} {
		if catalogFileAddressAllowed(netip.MustParseAddr(value)) {
			t.Fatalf("unsafe address allowed: %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !catalogFileAddressAllowed(netip.MustParseAddr(value)) {
			t.Fatal("public address blocked", value)
		}
	}
	client := newCatalogFileClient()
	defer client.CloseIdleConnections()
	for _, value := range []string{"http://cdn.example/file", "https://user:secret@cdn.example/file", "https://cdn.example:8443/file", "https://cdn.example/file#fragment"} {
		if validCatalogFileURL(value) {
			t.Fatal("unsafe file URL allowed", value)
		}
	}
	req, _ := http.NewRequest("GET", "http://cdn.example/file", nil)
	if client.CheckRedirect(req, []*http.Request{{}}) == nil {
		t.Fatal("HTTPS downgrade allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", "https://127.0.0.1/file", nil)
	if response, err := client.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("private address reached")
	}
}
