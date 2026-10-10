package torznab

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDiscoveryPreservesCertificateFailureWithoutDisclosure(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request reached an untrusted TLS server")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}
	defer transport.CloseIdleConnections()
	_, err := Open(context.Background(), Config{
		URL: server.URL + "/private-endpoint", APIKey: "private-key",
		Username: "private-user", Password: "private-password", Cookie: "session=private-cookie",
		HTTPClient: &http.Client{Transport: transport, Timeout: time.Second}, RequestInterval: time.Nanosecond,
	})
	var verification *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &verification) || !errors.As(err, &authority) {
		t.Fatal("discovery discarded the transport certificate failure")
	}
	transportAssertRedacted(t, err, server.URL, "private-endpoint", "private-key", "private-user", "private-password", "private-cookie")
}

func TestDiscoveryPreservesRedirectFailureWithoutDisclosure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/private-redirect", http.StatusFound)
	}))
	defer server.Close()
	cause := errors.New("private-redirect-failure: " + server.URL + "?apikey=private-key&password=private-password")
	_, err := Open(context.Background(), Config{
		URL: server.URL + "/private-endpoint", APIKey: "private-key", Username: "private-user", Password: "private-password",
		HTTPClient:      &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return cause }},
		RequestInterval: time.Nanosecond,
	})
	var request *url.Error
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, cause) || !errors.As(err, &request) {
		t.Fatal("discovery discarded the HTTP redirect failure cause")
	}
	transportAssertRedacted(t, err, server.URL, "private-endpoint", "private-redirect", "private-key", "private-user", "private-password")
}

func TestFetchPreservesTruncatedHTTPFailureWithoutDisclosure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			io.WriteString(w, transportCapsXML)
			return
		}
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "private-response-body")
	}))
	defer server.Close()
	client, err := Open(context.Background(), Config{
		URL: server.URL + "/private-endpoint", APIKey: "private-key", Username: "private-user", Password: "private-password",
		RequestInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatal("could not discover the fixture endpoint")
	}
	_, err = client.Search(context.Background(), Query{})
	var status *HTTPError
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.As(err, &status) || status.StatusCode != http.StatusBadGateway {
		t.Fatal("truncated response lost its HTTP status or transport cause")
	}
	transportAssertRedacted(t, err, server.URL, "private-endpoint", "private-key", "private-user", "private-password", "private-response-body")
}
