package outbound

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func connectProxy(t *testing.T, authorization func() string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var connections atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		if r.Method != http.MethodConnect || (authorization != nil && r.Header.Get("Proxy-Authorization") != authorization()) {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		if _, err := buffer.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
			return
		}
		if err := buffer.Flush(); err != nil {
			return
		}
		go func() { _, _ = io.Copy(upstream, buffer); upstream.Close() }()
		_, _ = io.Copy(client, upstream)
	}))
	t.Cleanup(server.Close)
	return server, &connections
}

func settingsFor(endpoint string) model.NetworkSettings {
	return model.NetworkSettings{Revision: 1, Default: "relay", Proxies: []model.NetworkProxy{{ID: "relay", Name: "Test proxy", URL: endpoint, Enabled: true}}, Overrides: map[string]string{}}
}

func fetch(t *testing.T, client *http.Client, address, target string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(WithTarget(context.Background(), target), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer synthetic-service-key")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestHTTPConnectRoutingRotationAndNoOriginCredentialLeak(t *testing.T) {
	var calls atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Authorization") != "Bearer synthetic-service-key" {
			t.Error("Proxy credentials reached the origin or service authorization changed")
		}
		_, _ = io.WriteString(w, "verified")
	}))
	t.Cleanup(origin.Close)
	var password atomic.Value
	password.Store("first-password")
	var auth atomic.Value
	auth.Store("Basic cHJveHktdXNlcjpmaXJzdC1wYXNzd29yZA==")
	relay, tunnels := connectProxy(t, func() string { return auth.Load().(string) })
	initial := settingsFor(relay.URL)
	initial.Proxies[0].UsernameRef, initial.Proxies[0].PasswordRef = "proxy-user", "proxy-password"
	var current atomic.Pointer[model.NetworkSettings]
	current.Store(&initial)
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return *current.Load(), nil }, Secrets: func(_ context.Context, ref string) (string, error) {
		if ref == "proxy-user" {
			return "proxy-user", nil
		}
		return password.Load().(string), nil
	}}
	base := HTTPTransport()
	base.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client := &http.Client{Transport: router.Transport("", base), Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	if fetch(t, client, origin.URL, "debrid:alldebrid") != "verified" || tunnels.Load() != 1 {
		t.Fatal("The selected proxy was not used")
	}
	password.Store("second-password")
	auth.Store("Basic cHJveHktdXNlcjpzZWNvbmQtcGFzc3dvcmQ=")
	if fetch(t, client, origin.URL, "debrid:alldebrid") != "verified" || tunnels.Load() != 2 {
		t.Fatal("Credential rotation reused an authenticated connection")
	}
	changed := initial
	changed.Revision++
	changed.Overrides = map[string]string{"debrid:torbox": model.NetworkDirect}
	current.Store(&changed)
	fetch(t, client, origin.URL, "debrid:torbox")
	if tunnels.Load() != 2 || calls.Load() != 3 {
		t.Fatal("A direct override did not take effect")
	}
}

func TestSelectedProxyFailureNeverFallsBackAndKeepsErrorsPrivate(t *testing.T) {
	var calls atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	t.Cleanup(origin.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = io.WriteString(w, "private-proxy-error-body")
	}))
	t.Cleanup(proxy.Close)
	for _, enabled := range []bool{true, false} {
		settings := settingsFor(proxy.URL)
		settings.Proxies[0].Enabled = enabled
		router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
		client := &http.Client{Transport: router.Transport("source:test", HTTPTransport()), Timeout: time.Second}
		_, err := client.Get(origin.URL)
		client.CloseIdleConnections()
		if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "private-proxy-error-body") || calls.Load() != 0 {
			t.Fatal("An unavailable proxy leaked details or fell back to the origin")
		}
	}
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) {
		return model.NetworkSettings{}, errors.New("private database failure")
	}}
	client := &http.Client{Transport: router.Transport("metadata:tmdb", HTTPTransport())}
	_, err := client.Get(origin.URL)
	if !errors.Is(err, ErrSettings) || calls.Load() != 0 || strings.Contains(err.Error(), "private database failure") {
		t.Fatal("A settings failure must stop before connecting")
	}
}

func TestProxyPreservesDestinationTLSVerification(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("Untrusted origin was reached") }))
	t.Cleanup(origin.Close)
	relay, _ := connectProxy(t, nil)
	settings := settingsFor(relay.URL)
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	client := &http.Client{Transport: router.Transport("metadata:tmdb", HTTPTransport()), Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	if _, err := client.Get(origin.URL); err == nil {
		t.Fatal("A proxy bypassed origin certificate verification")
	}
}

func TestSystemConnectionPreservesCallerProxyAndDirectBypassesIt(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	t.Cleanup(origin.Close)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "system-proxy")
	}))
	t.Cleanup(relay.Close)
	endpoint, _ := url.Parse(relay.URL)
	base := HTTPTransport()
	base.Proxy = func(*http.Request) (*url.URL, error) { return endpoint, nil }
	settings := model.NetworkSettings{Revision: 1, Default: model.NetworkSystem}
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	client := &http.Client{Transport: router.Transport("source:sample", base), Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	if fetch(t, client, origin.URL, "") != "system-proxy" {
		t.Fatal("An unchanged installation lost its existing proxy policy")
	}
	settings.Revision++
	settings.Default = model.NetworkDirect
	if fetch(t, client, origin.URL, "") != "direct" {
		t.Fatal("An explicit direct connection still used the system proxy")
	}
}

func TestHTTPSProxyRequiresItsOwnTrustedCertificate(t *testing.T) {
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("An untrusted HTTPS proxy received a request")
	}))
	t.Cleanup(relay.Close)
	settings := settingsFor(relay.URL)
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	client := &http.Client{Transport: router.Transport("source:test", HTTPTransport()), Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	if _, err := client.Get("https://example.invalid/"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("HTTPS proxy certificate verification was bypassed")
	}
}

func TestConnectHandshakeHonorsCancellation(t *testing.T) {
	entered := make(chan struct{})
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	t.Cleanup(relay.Close)
	settings := settingsFor(relay.URL)
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }}
	client := &http.Client{Transport: router.Transport("source:test", HTTPTransport())}
	t.Cleanup(client.CloseIdleConnections)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/", nil)
	done := make(chan error, 1)
	go func() { _, err := client.Do(req); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Proxy handshake did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("Cancellation was not preserved")
		}
	case <-time.After(time.Second):
		t.Fatal("Proxy handshake ignored cancellation")
	}
}

func TestSOCKS5RoutesHostnamesWithAuthentication(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("SOCKS credentials reached the origin")
		}
		_, _ = io.WriteString(w, "socks-verified")
	}))
	t.Cleanup(origin.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	hosts := make(chan string, 1)
	go func() {
		client, err := listener.Accept()
		if err != nil {
			return
		}
		defer client.Close()
		_ = client.SetDeadline(time.Now().Add(5 * time.Second))
		var greeting [2]byte
		if _, err := io.ReadFull(client, greeting[:]); err != nil || greeting[0] != 5 {
			return
		}
		methods := make([]byte, greeting[1])
		if _, err := io.ReadFull(client, methods); err != nil {
			return
		}
		_, _ = client.Write([]byte{5, 2})
		if _, err := io.ReadFull(client, greeting[:]); err != nil || greeting[0] != 1 {
			return
		}
		username := make([]byte, greeting[1])
		if _, err := io.ReadFull(client, username); err != nil {
			return
		}
		var length [1]byte
		if _, err := io.ReadFull(client, length[:]); err != nil {
			return
		}
		password := make([]byte, length[0])
		if _, err := io.ReadFull(client, password); err != nil || string(username) != "socks-user" || string(password) != "socks-password" {
			return
		}
		_, _ = client.Write([]byte{1, 0})
		var header [5]byte
		if _, err := io.ReadFull(client, header[:]); err != nil || header[0] != 5 || header[1] != 1 || header[3] != 3 {
			return
		}
		host := make([]byte, header[4])
		if _, err := io.ReadFull(client, host); err != nil {
			return
		}
		var port [2]byte
		if _, err := io.ReadFull(client, port[:]); err != nil {
			return
		}
		hosts <- net.JoinHostPort(string(host), strconv.Itoa(int(binary.BigEndian.Uint16(port[:]))))
		upstream, err := net.DialTimeout("tcp", origin.Listener.Addr().String(), time.Second)
		if err != nil {
			return
		}
		defer upstream.Close()
		_, _ = client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
		_ = client.SetDeadline(time.Time{})
		go func() { _, _ = io.Copy(upstream, client); upstream.Close() }()
		_, _ = io.Copy(client, upstream)
	}()
	settings := settingsFor("socks5://" + listener.Addr().String())
	settings.Proxies[0].UsernameRef, settings.Proxies[0].PasswordRef = "socks-user", "socks-password"
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }, Secrets: func(_ context.Context, ref string) (string, error) { return ref, nil }}
	base := HTTPTransport()
	base.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	client := &http.Client{Transport: router.Transport("source:test", base), Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	name := origin.Certificate().DNSNames[0]
	address := "https://" + net.JoinHostPort(name, port)
	if fetch(t, client, address, "source:test") != "socks-verified" {
		t.Fatal("SOCKS5 did not reach the destination")
	}
	if got := <-hosts; got != net.JoinHostPort(name, port) {
		t.Fatal("SOCKS5 did not receive the intended hostname")
	}
}
