package outbound

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMediaFlowForwardsProviderRequestsAndKeepsOtherRoutesProtected(t *testing.T) {
	const password = "synthetic-relay-password"
	const providerURL = "https://api.example.test/add?category=games&tag=one%2Btwo"
	received := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/forward" || len(r.URL.Query()) != 1 || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("Sent credentials outside the relay token")
		}
		value := r.URL.Query().Get("token")
		payload := decodeMediaFlowToken(t, value, password)
		if payload["d"] != providerURL || payload["h_authorization"] != "Bearer synthetic-provider-key" || payload["h_content-type"] != "application/x-www-form-urlencoded" || payload["h_accept-encoding"] != "identity" || payload["h_x-forwarded-for"] != nil || payload["h_proxy-authorization"] != nil {
			t.Error("Relay token lost provider request data or included unrelated headers")
		}
		expiry, ok := payload["exp"].(float64)
		if !ok || time.Until(time.Unix(int64(expiry), 0)) <= 0 || time.Until(time.Unix(int64(expiry), 0)) > time.Minute {
			t.Error("Relay token did not have a short expiry")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || r.Method != http.MethodPost || string(body) != "test=value" {
			t.Error("Provider method or body changed")
		}
		received <- value
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"synthetic rate limit"}`)
	}))
	defer server.Close()
	settings := settingsFor(server.URL)
	settings.Default = model.NetworkSystem
	settings.Overrides["debrid:torbox"] = "relay"
	settings.Proxies[0].Type = model.NetworkMediaFlow
	settings.Proxies[0].APIPasswordRef = "relay-secret"
	router := &Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }, Secrets: func(_ context.Context, name string) (string, error) {
		if name != "relay-secret" {
			t.Error("Resolved an unrelated secret")
		}
		return password, nil
	}}
	transport := router.Transport("", HTTPTransport())
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	request := func(target string) *http.Request {
		req, err := http.NewRequestWithContext(WithTarget(t.Context(), target), http.MethodPost, providerURL, strings.NewReader("test=value"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer synthetic-provider-key")
		req.Header.Set("Proxy-Authorization", "synthetic-hop-credential")
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	for range 2 {
		req := request("debrid:torbox")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 429 || response.Header.Get("Retry-After") != "12" || string(body) != `{"error":"synthetic rate limit"}` || response.Request.URL.String() != providerURL {
			t.Fatal("Relay status, retry policy, body or original response URL was not preserved")
		}
	}
	if <-received == <-received {
		t.Fatal("Two independent requests reused a relay token")
	}
	if _, err := client.Do(request("download:torbox")); !errors.Is(err, ErrUnavailable) {
		t.Fatal("An inherited MediaFlow route allowed a browser file download")
	}
	settings.Proxies[0].Enabled = false
	if _, err := client.Do(request("debrid:torbox")); !errors.Is(err, ErrUnavailable) || len(received) != 0 {
		t.Fatal("A disabled relay did not fail closed")
	}
}

func decodeMediaFlowToken(t *testing.T, token, password string) map[string]any {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data) < 2*aes.BlockSize || len(data)%aes.BlockSize != 0 {
		t.Error("Invalid MediaFlow token encoding")
		return nil
	}
	key := []byte(password + strings.Repeat(" ", 32))
	block, _ := aes.NewCipher(key[:32])
	body := data[aes.BlockSize:]
	cipher.NewCBCDecrypter(block, data[:aes.BlockSize]).CryptBlocks(body, body)
	padding := int(body[len(body)-1])
	var result map[string]any
	if padding == 0 || padding > aes.BlockSize || json.Unmarshal(body[:len(body)-padding], &result) != nil {
		t.Error("Relay could not decrypt the token")
	}
	return result
}
