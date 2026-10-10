package torznab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const transportCapsXML = `<caps><server title="Example indexer"/><limits default="2" max="100"/><searching><search available="yes" supportedParams="q"/></searching><categories><category id="5000" name="TV"><subcat id="5040" name="HD"/></category></categories></caps>`
const transportFeedXML = `<rss version="2.0"><channel><item><title>Authenticated release</title><guid>release-1</guid></item></channel></rss>`

func TestDiscoveryUsesAdvertisedEndpointAndCredentials(t *testing.T) {
	var externalCalls atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalCalls.Add(1)
		io.WriteString(w, transportCapsXML)
	}))
	defer external.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		username, password, ok := r.BasicAuth()
		cookie, cookieErr := r.Cookie("session")
		if !ok || username != "reader" || password != "basic-secret" || cookieErr != nil || cookie.Value != "cookie-secret" || r.URL.Query().Get("apikey") != "url-secret" {
			t.Error("discovery or search omitted configured authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("tenant") != "blue" {
			t.Error("fixed endpoint parameter was not preserved")
		}
		if r.URL.Query().Get("o") == "json" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"items":[]}`)
			return
		}
		if r.URL.Query().Get("q") == "stale" || r.URL.Query().Get("offset") == "99" {
			io.WriteString(w, `<rss><channel/></rss>`)
			return
		}
		switch r.URL.Path {
		case "/portal/":
			fmt.Fprintf(w, `<html><a href="%s/api">external</a><form action="/api"></form><a href="/ordinary">ordinary</a><a data-note='href="/decoy/api"' HREF="/feeds/special?t=caps&amp;apikey=advertised-secret">Torznab</a></html>`, external.URL)
		case "/feeds/special":
			if r.URL.Query().Get("t") == "caps" {
				io.WriteString(w, transportCapsXML)
			} else {
				io.WriteString(w, transportFeedXML)
			}
		default:
			t.Error("discovery fetched an unadvertised or non-API link")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := Open(context.Background(), Config{
		URL:    server.URL + "/portal/?APIKEY=url-secret&T=get&Q=stale&OFFSET=99&o=json&tenant=blue",
		Cookie: "session=cookie-secret", Username: "reader", Password: "basic-secret", RequestInterval: time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Endpoint() != server.URL+"/feeds/special" {
		t.Fatal("discovered endpoint did not omit its authentication and fixed query")
	}
	page, err := client.Search(context.Background(), Query{Text: "wanted"})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Authenticated release" {
		t.Fatalf("authenticated search failed: %v", err)
	}
	if calls.Load() != 3 || externalCalls.Load() != 0 {
		t.Fatal("discovery did not restrict itself to the advertised same-origin API")
	}
	if client.RateLimit().Limit != nil || client.RateLimit().Remaining != nil {
		t.Fatal("capability page sizes were incorrectly interpreted as request quotas")
	}
	caps := client.Capabilities()
	caps.Searches[SearchMode("search")] = SearchCapability{Available: false}
	caps.Categories[0].Subcategories[0].Name = "changed"
	fresh := client.Capabilities()
	if !fresh.Searches[SearchMode("search")].Available || fresh.Categories[0].Subcategories[0].Name != "HD" {
		t.Fatal("capabilities exposed mutable internal data")
	}
	search := fresh.Searches[SearchMode("search")]
	search.SupportedParams[0] = "changed"
	if client.Capabilities().Searches[SearchMode("search")].SupportedParams[0] != "q" {
		t.Fatal("supported parameters exposed mutable internal data")
	}
}

func TestSessionCookieRotationReplacesSeededCredentials(t *testing.T) {
	for _, suppliedJar := range []bool{false, true} {
		t.Run(fmt.Sprintf("supplied_jar_%t", suppliedJar), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sessions := r.CookiesNamed("session")
				expected := "initial session"
				if r.URL.Query().Get("t") != "caps" {
					expected = "rotated-session"
				}
				if len(sessions) != 1 || sessions[0].Value != expected {
					t.Error("session rotation sent a stale or duplicate cookie")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.URL.Query().Get("t") == "caps" {
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "rotated-session", Path: "/", HttpOnly: true})
					io.WriteString(w, transportCapsXML)
				} else {
					io.WriteString(w, transportFeedXML)
				}
			}))
			defer server.Close()
			callerClient := &http.Client{}
			if suppliedJar {
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				origin, _ := url.Parse(server.URL)
				jar.SetCookies(origin, []*http.Cookie{{Name: "session", Value: "older-session", Path: "/"}})
				callerClient.Jar = jar
			}
			client, err := Open(context.Background(), Config{
				URL: server.URL, Cookie: `session="initial session"`, HTTPClient: callerClient, RequestInterval: time.Nanosecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			page, err := client.Search(context.Background(), Query{})
			if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Authenticated release" {
				t.Fatalf("rotated authentication did not survive into search: %v", err)
			}
			if !suppliedJar && callerClient.Jar != nil {
				t.Fatal("Open replaced the caller's HTTPClient jar")
			}
		})
	}
}

func TestDiscoveryFindsCommonRouteUnderSubpath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/installed/api" {
			io.WriteString(w, transportCapsXML)
			return
		}
		io.WriteString(w, "<html><title>Indexer</title></html>")
	}))
	defer server.Close()
	client, err := Open(context.Background(), Config{URL: server.URL + "/installed/", RequestInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	if client.Endpoint() != server.URL+"/installed/api" {
		t.Fatal("common API route under the supplied subpath was not found")
	}
}

func TestDiscoveryBoundsUnsuccessfulProbes(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/" {
			for i := range 100 {
				fmt.Fprintf(w, `<a href="/api/candidate%d">API</a>`, i)
			}
			return
		}
		http.Error(w, "private response body", http.StatusNotFound)
	}))
	defer server.Close()
	_, err := Open(context.Background(), Config{URL: server.URL, APIKey: "private-key", RequestInterval: time.Nanosecond})
	var status *HTTPError
	if !errors.Is(err, ErrNotFound) || !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
		t.Fatalf("discovery did not preserve typed failure: %v", err)
	}
	if calls.Load() != 16 {
		t.Fatalf("discovery performed %d requests, want bounded 16", calls.Load())
	}
	transportAssertRedacted(t, err, server.URL, "private-key", "private response body")
}

func TestDiscoveryStopsAtDefiniteAuthenticationFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		api    bool
	}{
		{"api error with successful HTTP status", http.StatusOK, `<error code="100" description="private-key"/>`, true},
		{"HTTP authentication failure", http.StatusUnauthorized, "private-key", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer server.Close()
			_, err := Open(context.Background(), Config{URL: server.URL, APIKey: "private-key", RequestInterval: time.Nanosecond})
			if !errors.Is(err, ErrNotFound) || calls.Load() != 1 {
				t.Fatalf("discovery continued after definite authentication failure: %v", err)
			}
			if test.api {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != 100 {
					t.Fatal("XML API authentication error was lost")
				}
			} else {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != test.status {
					t.Fatal("HTTP authentication error was lost")
				}
			}
			transportAssertRedacted(t, err, server.URL, "private-key")
		})
	}
}

func TestRedirectCannotForwardCredentialsAcrossOrigins(t *testing.T) {
	var externalCalls atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalCalls.Add(1)
		io.WriteString(w, transportCapsXML)
	}))
	defer external.Close()
	for _, mutateHook := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom_hook_mutates_target_%t", mutateHook), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destination := external.URL + "/api"
				if mutateHook {
					destination = "/relay"
				}
				http.Redirect(w, r, destination, http.StatusFound)
			}))
			defer server.Close()
			callerClient := &http.Client{Timeout: time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if mutateHook {
					request.URL, _ = url.Parse(external.URL + "/api")
				}
				return nil
			}}
			_, err := Open(context.Background(), Config{
				URL: server.URL, APIKey: "private-key", Cookie: "session=private-cookie",
				Username: "private-user", Password: "private-password", HTTPClient: callerClient, RequestInterval: time.Nanosecond,
			})
			if !errors.Is(err, ErrNotFound) || externalCalls.Load() != 0 {
				t.Fatalf("cross-origin redirect was followed: %v", err)
			}
			transportAssertRedacted(t, err, server.URL, external.URL, "private-key", "private-cookie", "private-user", "private-password")
			// The caller's client retains its own redirect behavior after Open.
			response, originalErr := callerClient.Get(server.URL)
			if originalErr != nil {
				t.Fatalf("Open mutated the caller's client: %v", originalErr)
			}
			response.Body.Close()
			if externalCalls.Swap(0) != 1 {
				t.Fatal("caller client did not retain its original redirect behavior")
			}
		})
	}
}

func TestRedirectCannotDowngradeHTTPS(t *testing.T) {
	var insecureCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+"/api", http.StatusFound)
	}))
	defer server.Close()
	callerClient := server.Client()
	baseTransport := callerClient.Transport
	callerClient.Transport = transportRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" {
			insecureCalls.Add(1)
			return nil, errors.New("credentials reached insecure transport")
		}
		return baseTransport.RoundTrip(request)
	})
	_, err := Open(context.Background(), Config{URL: server.URL, APIKey: "private-key", HTTPClient: callerClient, RequestInterval: time.Nanosecond})
	if !errors.Is(err, ErrNotFound) || insecureCalls.Load() != 0 {
		t.Fatalf("HTTPS downgrade was followed: %v", err)
	}
	transportAssertRedacted(t, err, server.URL, "private-key")
}

func TestFetchRetriesOnlyWithValidRetryAfterAndBoundsRetries(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		attempts   int32
	}{
		{"rate limit with immediate retry", 429, "0", 4},
		{"unavailable with immediate retry", 503, "0", 4},
		{"rate limit without retry information", 429, "", 1},
		{"unavailable with invalid retry information", 503, "not a date", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") == "caps" {
					io.WriteString(w, transportCapsXML)
					return
				}
				attempts.Add(1)
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				if test.retryAfter == "0" {
					w.Header().Set("RateLimit-Remaining", "0")
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client := transportOpen(t, server.URL)
			_, err := client.Search(context.Background(), Query{})
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != test.status || attempts.Load() != test.attempts {
				t.Fatalf("retry contract failed after %d attempts: %v", attempts.Load(), err)
			}
			wantRateError := test.status == 429 && test.retryAfter == ""
			if errors.Is(err, ErrRateLimited) != wantRateError {
				t.Fatal("rate-limit identity did not distinguish missing timing from bounded retry exhaustion")
			}
		})
	}
}

func TestFetchHonorsRetryAfterDelay(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			io.WriteString(w, transportCapsXML)
			return
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, transportFeedXML)
	}))
	defer server.Close()
	client := transportOpen(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	page, err := client.Search(ctx, Query{})
	if err != nil || len(page.Items) != 1 || attempts.Load() != 2 {
		t.Fatalf("request did not recover after Retry-After: %v", err)
	}
	if time.Since(start) < time.Second {
		t.Fatal("request retried before Retry-After expired")
	}
}

func TestFetchRetryAfterHTTPDateIsContextCancellable(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			io.WriteString(w, transportCapsXML)
			return
		}
		attempts.Add(1)
		w.Header().Set("Retry-After", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := transportOpen(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.Search(ctx, Query{})
	if !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 1 {
		t.Fatalf("HTTP-date wait did not preserve cancellation: %v", err)
	}
}

func TestBodyQuotaBlocksThenExpiresWithoutInventingRemaining(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			io.WriteString(w, transportCapsXML)
			return
		}
		searches.Add(1)
		io.WriteString(w, transportFeedXML)
	}))
	defer server.Close()
	client := transportOpen(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	params := url.Values{"t": {"search"}}
	err := client.fetch(ctx, client.endpoint, params, func(data []byte) error {
		remaining := int64(0)
		client.observeRate(RateLimit{Remaining: &remaining})
		remaining = 12 // An observation must not retain the caller's pointer.
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Search(ctx, Query{})
	if !errors.Is(err, ErrRateLimited) || searches.Load() != 1 {
		t.Fatalf("quota without a reset allowed another request: %v", err)
	}
	remaining := int64(0)
	reset := time.Now().Add(40 * time.Millisecond)
	client.observeRate(RateLimit{Remaining: &remaining, ResetAt: &reset})
	snapshot := client.RateLimit()
	*snapshot.Remaining = 10
	*snapshot.ResetAt = time.Now().Add(time.Hour)
	_, err = client.Search(ctx, Query{})
	if err != nil || time.Now().Before(reset) {
		t.Fatalf("quota reset did not permit a correctly paced request: %v", err)
	}
	if client.RateLimit().Remaining != nil {
		t.Fatal("expired exhaustion was retained or a replenished quota was invented")
	}
	if _, err := client.Search(ctx, Query{}); err != nil || searches.Load() != 3 {
		t.Fatalf("stale exhausted quota permanently blocked later calls: %v", err)
	}
}

func TestXMLQuotaWithoutUsagePreservesExhaustedHeaders(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw_%t", raw), func(t *testing.T) {
			var searches atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") == "caps" {
					io.WriteString(w, transportCapsXML)
					return
				}
				searches.Add(1)
				w.Header().Set("RateLimit-Remaining", "0")
				io.WriteString(w, `<rss><channel><apilimits apiMax="100"/><item><guid>one</guid></item></channel></rss>`)
			}))
			defer server.Close()
			client := transportOpen(t, server.URL)
			search := func() (Page, error) {
				if raw {
					page, err := client.SearchRaw(context.Background(), Query{})
					return page.Page, err
				}
				return client.Search(context.Background(), Query{})
			}
			page, err := search()
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("initial response failed: %v", err)
			}
			if page.RateLimit == nil || page.RateLimit.Remaining != nil {
				t.Fatal("missing XML usage invented a remaining quota")
			}
			if _, err := search(); !errors.Is(err, ErrRateLimited) || searches.Load() != 1 {
				t.Fatalf("incomplete XML quota erased exhausted headers: requests=%d error=%v", searches.Load(), err)
			}
		})
	}
}

func TestHeaderQuotaUsesCorrectResetTimeConventions(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%t", legacy), func(t *testing.T) {
			before := time.Now()
			epoch := time.Now().Add(time.Minute).Unix()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				prefix := "RateLimit-"
				reset := "60"
				if legacy {
					prefix = "X-RateLimit-"
					reset = fmt.Sprint(epoch)
				}
				w.Header().Set(prefix+"Limit", "20")
				w.Header().Set(prefix+"Remaining", "0")
				w.Header().Set(prefix+"Reset", reset)
				io.WriteString(w, transportCapsXML)
			}))
			defer server.Close()
			client := transportOpen(t, server.URL)
			after := time.Now()
			rate := client.RateLimit()
			if rate.Limit == nil || *rate.Limit != 20 || rate.Remaining == nil || *rate.Remaining != 0 || rate.ResetAt == nil {
				t.Fatal("advertised quota was not retained")
			}
			if legacy {
				if rate.ResetAt.Unix() != epoch {
					t.Fatal("legacy epoch reset was treated as a delta")
				}
			} else if rate.ResetAt.Before(before.Add(time.Minute)) || rate.ResetAt.After(after.Add(time.Minute)) {
				t.Fatal("standard reset delta was treated as an epoch")
			}
			*rate.Limit = 999
			if *client.RateLimit().Limit != 20 {
				t.Fatal("quota snapshot exposed internal pointers")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := client.Search(ctx, Query{}); !errors.Is(err, context.Canceled) {
				t.Fatalf("quota wait ignored cancellation: %v", err)
			}
		})
	}
}

func TestRequestGateIsContextCancellable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, transportCapsXML)
	}))
	defer server.Close()
	client := transportOpen(t, server.URL)
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	defer close(release)
	go func() {
		finished <- client.fetch(context.Background(), client.endpoint, url.Values{"t": {"caps"}}, func(data []byte) error {
			// observeRate must not deadlock inside the gated consumer.
			client.observeRate(RateLimit{})
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("first gated request failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("consumer deadlocked while observing a quota")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := client.fetch(ctx, client.endpoint, url.Values{"t": {"caps"}}, func([]byte) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for request gate ignored cancellation: %v", err)
	}
}

func TestConfigRejectsUnsafeURLsAndHeaderInjection(t *testing.T) {
	for _, config := range []Config{
		{URL: "/api"},
		{URL: "file:///api"},
		{URL: "https://reader:secret@example.invalid/api"},
		{URL: "https://example.invalid/api#secret"},
		{URL: "https://example.invalid:70000/api"},
		{URL: "https://example.invalid:invalid/api"},
		{URL: "https://example.invalid:/api"},
		{URL: "https://example.invalid/api", Cookie: "session=secret\r\nX-Test: injected"},
		{URL: "https://example.invalid/api", Cookie: "session id=secret"},
		{URL: "https://example.invalid/api", Username: "reader:secret", Password: "password"},
		{URL: "https://example.invalid/api", Password: "secret"},
		{URL: "https://example.invalid/api", PageSize: -1},
		{URL: "https://example.invalid/api", RequestInterval: -time.Second},
	} {
		_, err := Open(context.Background(), config)
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatal("invalid configuration reached endpoint discovery")
		}
		transportAssertRedacted(t, err, "example.invalid", "secret", "injected")
	}
}

func TestPublishedAtUnitRejectsUnsupportedUnitsBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, transportCapsXML)
	}))
	defer server.Close()
	for _, unit := range []string{"microseconds", "Seconds", " seconds "} {
		if _, err := Open(context.Background(), Config{URL: server.URL, PublishedAtUnit: unit}); err == nil {
			t.Fatalf("unsupported unit %q accepted", unit)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported publication unit reached HTTP")
	}
}

func TestSearchPublicationDatesMatchRawAndRetainBytes(t *testing.T) {
	for _, unit := range []string{"", "seconds", "milliseconds"} {
		t.Run("unit="+unit, func(t *testing.T) {
			dates := []struct{ input, want string }{
				{"2026-09-22T03:02:03.123+02:00", "2026-09-22T01:02:03.123Z"},
				{"Tue, 16 Jul 2019 22:56:54 +0200", "2019-07-16T20:56:54Z"},
				{"", ""},
			}
			switch unit {
			case "seconds":
				dates = append(dates, []struct{ input, want string }{
					{"0", "1970-01-01T00:00:00Z"},
					{"-1", "1969-12-31T23:59:59Z"},
					{"1001", "1970-01-01T00:16:41Z"},
					{"-62135596800", "0001-01-01T00:00:00Z"},
					{"253402300799", "9999-12-31T23:59:59Z"},
				}...)
			case "milliseconds":
				dates = append(dates, []struct{ input, want string }{
					{"0", "1970-01-01T00:00:00Z"},
					{"-1", "1969-12-31T23:59:59.999Z"},
					{"1001", "1970-01-01T00:00:01.001Z"},
					{"-62135596800000", "0001-01-01T00:00:00Z"},
					{"253402300799999", "9999-12-31T23:59:59.999Z"},
				}...)
			}
			fragments := make([]string, len(dates))
			for i, date := range dates {
				fragments[i] = fmt.Sprintf("<item data-original='keep'><guid>%d</guid><title>A &amp; B</title><pubDate><![CDATA[%s]]></pubDate></item>", i, date.input)
			}
			fragments = append(fragments, `<item><guid>missing-date</guid></item>`)
			body := "<?xml version='1.0'?><rss><channel>\n" + strings.Join(fragments, "\n") + "\n</channel></rss>"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("t") == "caps" {
					io.WriteString(w, transportCapsXML)
				} else {
					io.WriteString(w, body)
				}
			}))
			defer server.Close()
			provider, err := LoadProvider(strings.NewReader(fmt.Sprintf(`{"version":1,"id":"dates","url":%q,"published_at_unit":%q}`, server.URL, unit)))
			if err != nil {
				t.Fatal(err)
			}
			config, err := provider.Config(nil)
			if err != nil {
				t.Fatal(err)
			}
			config.RequestInterval = time.Nanosecond
			client, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			page, err := client.Search(context.Background(), Query{})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := client.SearchRaw(context.Background(), Query{})
			if err != nil {
				t.Fatal(err)
			}
			if string(raw.Body) != body || len(raw.Items) != len(fragments) || len(page.Items) != len(fragments) {
				t.Fatal("publication normalization changed the response")
			}
			for i, fragment := range fragments {
				if string(raw.Items[i].Body) != fragment || raw.Items[i].Error != nil {
					t.Fatalf("raw item %d = %#v", i, raw.Items[i])
				}
				want := ""
				if i < len(dates) {
					want = dates[i].want
				}
				for _, item := range []Item{page.Items[i], raw.Items[i].Item} {
					if want == "" {
						if item.PublishedAt != nil {
							t.Fatalf("missing date invented: %v", item.PublishedAt)
						}
					} else if item.PublishedAt == nil || item.PublishedAt.Location() != time.UTC || item.PublishedAt.Format(time.RFC3339Nano) != want {
						t.Fatalf("publication date = %v, want %s", item.PublishedAt, want)
					}
				}
			}
		})
	}
}

func TestFetchClosesOversizedAndFailingResponseBodies(t *testing.T) {
	for _, readFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("reader_failure_%t", readFailure), func(t *testing.T) {
			var closed atomic.Bool
			callerClient := &http.Client{Transport: transportRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body io.ReadCloser = io.NopCloser(strings.NewReader(transportCapsXML))
				status := http.StatusOK
				if request.URL.Query().Get("t") != "caps" {
					body = &transportResponseBody{remaining: maxResponseBytes + 1, fail: readFailure, closed: &closed}
					if readFailure {
						status = http.StatusBadGateway
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: request}, nil
			})}
			client, err := Open(context.Background(), Config{URL: "https://example.invalid/api", HTTPClient: callerClient, RequestInterval: time.Nanosecond})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Search(context.Background(), Query{})
			if err == nil || !closed.Load() {
				t.Fatal("invalid response was accepted or its body was not closed")
			}
			if readFailure {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusBadGateway {
					t.Fatal("body read failure discarded the HTTP status")
				}
			}
			transportAssertRedacted(t, err, "example.invalid", "secret-reader-error")
		})
	}
}

func transportOpen(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := Open(context.Background(), Config{URL: endpoint, RequestInterval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func transportAssertRedacted(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range secrets {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error message disclosed private request or response information")
		}
	}
}

type transportRoundTripFunc func(*http.Request) (*http.Response, error)

func (f transportRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type transportResponseBody struct {
	remaining int
	fail      bool
	closed    *atomic.Bool
}

func (body *transportResponseBody) Read(p []byte) (int, error) {
	if body.fail {
		return 0, errors.New("secret-reader-error")
	}
	if body.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), body.remaining)
	clear(p[:n])
	body.remaining -= n
	return n, nil
}

func (body *transportResponseBody) Close() error {
	body.closed.Store(true)
	return nil
}
