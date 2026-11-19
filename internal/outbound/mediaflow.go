package outbound

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func mediaFlowRoundTrip(transport *http.Transport, request *http.Request, relay *url.URL, password string) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Hostname() == "" || request.URL.User != nil || request.URL.Opaque != "" || request.URL.Fragment != "" {
		if request.Body != nil {
			request.Body.Close()
		}
		return nil, ErrUnavailable
	}
	// Only headers used by provider APIs cross the relay. Authentication and
	// destination parameters are carried inside a short-lived MediaFlow token.
	payload := map[string]any{
		"d":                 request.URL.String(),
		"exp":               time.Now().Add(time.Minute).Unix(),
		"h_accept-encoding": "identity",
	}
	for _, name := range []string{"Accept", "Authorization", "Content-Type", "User-Agent"} {
		if value := request.Header.Get(name); value != "" {
			payload["h_"+strings.ToLower(name)] = value
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		if request.Body != nil {
			request.Body.Close()
		}
		return nil, ErrUnavailable
	}
	defer clear(data)
	endpoint := *relay
	endpoint.Path, endpoint.RawPath = "/proxy/forward", ""
	endpoint.RawQuery = "token=" + mediaFlowToken(password, data)
	forward := (&http.Request{
		Method:        request.Method,
		URL:           &endpoint,
		Header:        http.Header{"Cache-Control": {"no-store"}},
		Body:          request.Body,
		ContentLength: request.ContentLength,
	}).WithContext(request.Context())
	response, err := transport.RoundTrip(forward)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		return nil, ErrUnavailable
	}
	response.Request = request
	return response, nil
}

// MediaFlow's token format is AES-256-CBC with a random IV and PKCS#7 padding.
// HTTPS protects transport to the relay. Tokens stay between the two servers.
func mediaFlowToken(password string, data []byte) string {
	key := [32]byte{}
	for i := range key {
		key[i] = ' '
	}
	copy(key[:], password)
	block, _ := aes.NewCipher(key[:])
	clear(key[:])
	const size = aes.BlockSize
	padding := size - len(data)%size
	envelope := make([]byte, size+len(data)+padding)
	rand.Read(envelope[:size])
	plaintext := envelope[size:]
	copy(plaintext, data)
	copy(plaintext[len(data):], bytes.Repeat([]byte{byte(padding)}, padding))
	cipher.NewCBCEncrypter(block, envelope[:size]).CryptBlocks(plaintext, plaintext)
	return base64.RawURLEncoding.EncodeToString(envelope)
}
