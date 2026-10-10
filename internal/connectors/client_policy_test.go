package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestConfiguredRequestTimeoutCancelsInflightResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	provider := protocolProvider("http_json", server.URL)
	provider.RequestTimeout = "250ms"
	client, err := NewClient(t.Context(), provider, Environment{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = client.Do(ctx, Request{Method: http.MethodGet})
	if err == nil || ctx.Err() != nil || calls.Load() != 1 {
		t.Fatalf("configured timeout must stop the received request before the caller deadline: err=%v caller=%v requests=%d", err, ctx.Err(), calls.Load())
	}
}

func TestConfiguredRateLimitResetDoesNotGuessByMagnitude(t *testing.T) {
	for _, mode := range []string{"relative", "epoch"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("X-RateLimit-Remaining", "0")
				// A future relative reset, but a past Unix timestamp. Its meaning
				// must depend only on configuration, not a guessed cutoff.
				w.Header().Set("X-RateLimit-Reset", "20000000")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			provider := protocolProvider("http_json", server.URL)
			provider.RateLimitReset = mode
			client, err := NewClient(t.Context(), provider, Environment{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			if _, err := client.Do(t.Context(), Request{Method: http.MethodGet}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			_, err = client.Do(ctx, Request{Method: http.MethodGet})
			if mode == "relative" {
				if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
					t.Fatalf("relative reset must block without another request: err=%v requests=%d", err, calls.Load())
				}
			} else if err != nil || calls.Load() != 2 {
				t.Fatalf("past epoch reset must permit the next request: err=%v requests=%d", err, calls.Load())
			}
		})
	}
}

func TestArchivedQuotaRetriesAreBoundedAndFailClosed(t *testing.T) {
	for _, test := range []struct {
		name     string
		reset    bool
		limit    int
		requests int32
	}{
		{name: "missing deadline", limit: -1, requests: 1},
		{name: "default retry limit", reset: true, limit: -1, requests: 4},
		{name: "disabled retries", reset: true, limit: 0, requests: 1},
		{name: "custom retry limit", reset: true, limit: 2, requests: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if archived.Load() != n-1 {
					t.Error("next attempt started before the rejected response was retained")
				}
				if test.reset {
					w.Header().Set("Retry-After", "0")
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("original quota response"))
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.RequestInterval = "1ns"
			var limit *int
			if test.limit >= 0 {
				limit = &test.limit
			}
			client, err := NewClient(t.Context(), p, Environment{
				MaxQuotaRetries: limit,
				RateLimited: func(_ context.Context, response Response, retryAt time.Time, retry bool) error {
					if response.StatusCode != http.StatusTooManyRequests || string(response.Body) != "original quota response" {
						t.Error("quota callback lost the original response")
					}
					if retryAt.IsZero() == test.reset || retry != (test.reset && requests.Load() < test.requests) {
						t.Errorf("quota callback lost its deadline or retry eligibility: deadline=%v retry=%t", retryAt, retry)
					}
					archived.Add(1)
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = true
			response, err := client.Do(t.Context(), Request{Method: http.MethodGet})
			if err == nil || response.StatusCode != http.StatusTooManyRequests || string(response.Body) != "original quota response" || requests.Load() != test.requests || archived.Load() != test.requests {
				t.Fatalf("quota did not fail with bounded intact evidence: status=%d requests=%d archived=%d error=%v", response.StatusCode, requests.Load(), archived.Load(), err)
			}
			if !test.reset {
				if _, err := client.Do(t.Context(), Request{Method: http.MethodGet}); err == nil || requests.Load() != 1 {
					t.Fatal("missing reset information allowed another source request")
				}
			}
		})
	}
}

func TestPublicRequestsRejectPercentEncodedProviderSecrets(t *testing.T) {
	for _, test := range []struct {
		name     string
		location string
		redirect bool
	}{
		{"initial path", "/%66ixture-secret-value", false},
		{"redirect path", "/%66ixture-secret-value", true},
		{"redirect query", "/public?view=%66ixture-secret-value", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var leaked atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					w.Header().Set("Location", test.location)
					w.WriteHeader(http.StatusFound)
					return
				}
				leaked.Add(1)
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.Auth = model.Auth{Type: "bearer", SecretRef: "fixture-key"}
			client, err := NewClient(t.Context(), p, Environment{Secrets: protocolSecrets})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			endpoint := server.URL + test.location
			if test.redirect {
				endpoint = server.URL + "/redirect"
			}
			origin, _ := url.Parse(server.URL)
			if _, err := client.DoPublic(t.Context(), Request{URL: endpoint}, []string{origin.Hostname()}); err == nil || leaked.Load() != 0 {
				t.Fatalf("encoded provider secret reached public endpoint: requests=%d err=%v", leaked.Load(), err)
			}
		})
	}
}

func TestTransientRetriesPreserveRequestAndEveryResponse(t *testing.T) {
	for _, test := range []struct {
		name      string
		recover   bool
		archive   bool
		attempts  int32
		callbacks int32
	}{
		{name: "archived recovery", recover: true, archive: true, attempts: 2, callbacks: 1},
		{name: "archived exhaustion", archive: true, attempts: 4, callbacks: 4},
		{name: "standalone exhaustion", attempts: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api" || r.URL.RawQuery != "offset=0&query=2020" {
					t.Errorf("retry changed the logical request: %s %s", r.Method, r.URL.RequestURI())
				}
				if test.archive && archived.Load() != n-1 {
					t.Error("retry reached the source before retaining the previous response")
				}
				if test.recover && n == 2 {
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = fmt.Fprintf(w, "gateway response %d", n)
			}))
			t.Cleanup(server.Close)
			quotaRetries := 0
			env := Environment{MaxQuotaRetries: &quotaRetries}
			if test.archive {
				env.TransientFailure = func(_ context.Context, response Response, retryAt time.Time, attempt int, retry bool) error {
					n := archived.Add(1)
					if attempt != int(n) || response.StatusCode != http.StatusGatewayTimeout || string(response.Body) != fmt.Sprintf("gateway response %d", n) {
						t.Errorf("transient response lost its evidence: attempt=%d response=%+v", attempt, response)
					}
					if retryAt.IsZero() || retry != (attempt < 4) {
						t.Errorf("incorrect retry boundary: attempt=%d deadline=%v retry=%t", attempt, retryAt, retry)
					}
					return nil
				}
			}
			client, err := NewClient(t.Context(), protocolProvider("http_json", server.URL+"/api"), env)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = test.archive
			response, err := client.Do(t.Context(), Request{Query: url.Values{"query": {"2020"}, "offset": {"0"}}})
			if requests.Load() != test.attempts || archived.Load() != test.callbacks {
				t.Fatalf("unbounded or uncommitted attempts: requests=%d archived=%d err=%v", requests.Load(), archived.Load(), err)
			}
			if test.recover {
				if err != nil || response.StatusCode != http.StatusOK || string(response.Body) != `{"items":[]}` {
					t.Fatalf("retry did not recover the same request: response=%+v err=%v", response, err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "HTTP 504 after 4 attempts") || FailureCode(err) != "http" ||
					response.StatusCode != http.StatusGatewayTimeout || string(response.Body) != "gateway response 4" {
					t.Fatalf("exhaustion lost its status, attempt or response: response=%+v err=%v", response, err)
				}
				if test.archive && client.lastResponse().StatusCode != 0 {
					t.Fatal("already archived final response remained available for replay")
				}
			}
		})
	}
}

func TestTransientRetryMethodAndStatusEligibility(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		for _, status := range []int{400, 401, 403, 404, 500, 501, 502, 503, 504} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				eligible := (method == http.MethodGet || method == http.MethodHead) &&
					(status == 500 || status == 502 || status == 503 || status == 504)
				var requests, archived atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Retry-After", "0")
					if requests.Add(1) == 1 {
						w.WriteHeader(status)
					}
				}))
				t.Cleanup(server.Close)
				client, err := NewClient(t.Context(), protocolProvider("http_json", server.URL), Environment{
					TransientFailure: func(_ context.Context, response Response, _ time.Time, attempt int, retry bool) error {
						archived.Add(1)
						if !eligible || response.StatusCode != status || attempt != 1 || !retry {
							t.Errorf("ineligible or incorrect transient callback: status=%d attempt=%d retry=%t", response.StatusCode, attempt, retry)
						}
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = client.Close() })
				client.returnEveryResponse = true
				response, err := client.Do(t.Context(), Request{Method: method})
				if eligible {
					if err != nil || response.StatusCode != http.StatusOK || requests.Load() != 2 || archived.Load() != 1 {
						t.Fatalf("eligible retry failed: requests=%d archives=%d response=%+v err=%v", requests.Load(), archived.Load(), response, err)
					}
				} else if err == nil || response.StatusCode != status || requests.Load() != 1 || archived.Load() != 0 {
					t.Fatalf("unsafe or ineligible request retried: requests=%d archives=%d response=%+v err=%v", requests.Load(), archived.Load(), response, err)
				}
			})
		}
	}
}

func TestTransientRetryRequiresCompleteArchivableResponse(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			var requests, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Retry-After", "0")
				if partial {
					w.Header().Set("Content-Length", "100")
				}
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("gateway bytes"))
			}))
			t.Cleanup(server.Close)
			env := Environment{}
			if partial {
				env.TransientFailure = func(context.Context, Response, time.Time, int, bool) error {
					archived.Add(1)
					return nil
				}
			}
			client, err := NewClient(t.Context(), protocolProvider("http_json", server.URL), env)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = true
			response, err := client.Do(t.Context(), Request{})
			if err == nil || response.StatusCode != http.StatusGatewayTimeout || string(response.Body) != "gateway bytes" || requests.Load() != 1 || archived.Load() != 0 {
				t.Fatalf("partial or unarchivable response was hidden: response=%+v requests=%d archived=%d err=%v", response, requests.Load(), archived.Load(), err)
			}
		})
	}
}

func TestTransientRetryBackoffAndRetryAfter(t *testing.T) {
	now := time.Date(2040, time.January, 2, 3, 4, 5, 0, time.UTC)
	for _, test := range []struct {
		header  string
		attempt int
		delay   time.Duration
	}{
		{attempt: 1, delay: 2 * time.Second},
		{attempt: 2, delay: 4 * time.Second},
		{attempt: 3, delay: 8 * time.Second},
		{attempt: 4, delay: 8 * time.Second},
		{header: "invalid", attempt: 2, delay: 4 * time.Second},
		{header: "-1", attempt: 1, delay: 2 * time.Second},
		{header: "9223372036854775807", attempt: 1, delay: 2 * time.Second},
		{header: "0", attempt: 3, delay: 0},
		{header: "19", attempt: 1, delay: 19 * time.Second},
		{header: now.Add(90 * time.Second).Format(http.TimeFormat), attempt: 1, delay: 90 * time.Second},
		{header: now.Add(-time.Minute).Format(http.TimeFormat), attempt: 1, delay: 0},
	} {
		t.Run(fmt.Sprintf("%s/attempt%d", test.header, test.attempt), func(t *testing.T) {
			header := http.Header{"Retry-After": {test.header}}
			if delay := transientRetryDelay(header, now, test.attempt); delay != test.delay {
				t.Fatalf("retry deadline violated backoff/server policy: delay=%v want=%v", delay, test.delay)
			}
		})
	}
}

func TestTransientRetryDeadlinesRespectPacingAndQuota(t *testing.T) {
	for _, test := range []struct {
		name     string
		interval string
		header   http.Header
		delay    time.Duration
	}{
		{name: "fallback", interval: "1ns", delay: 2 * time.Second},
		{name: "source pacing", interval: "1h", header: http.Header{"Retry-After": {"0"}}, delay: time.Hour},
		{name: "server retry after", interval: "1ns", header: http.Header{"Retry-After": {"60"}}, delay: time.Minute},
		{name: "server quota", interval: "1ns", header: http.Header{"Retry-After": {"60"}, "Ratelimit-Remaining": {"0"}, "Ratelimit-Reset": {"120"}}, delay: 2 * time.Minute},
		{name: "unknown quota reset", interval: "1ns", header: http.Header{"Ratelimit-Remaining": {"0"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				for name, values := range test.header {
					w.Header()[name] = values
				}
				w.WriteHeader(http.StatusGatewayTimeout)
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.RequestInterval = test.interval
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var archivedDeadline time.Time
			var waited bool
			start := time.Now()
			client, err := NewClient(ctx, p, Environment{
				TransientFailure: func(_ context.Context, _ Response, retryAt time.Time, attempt int, retry bool) error {
					archived.Add(1)
					archivedDeadline = retryAt
					if attempt != 1 || retry != (test.delay > 0) {
						t.Errorf("incorrect retry eligibility: attempt=%d retry=%t", attempt, retry)
					}
					if test.delay == 0 {
						if !retryAt.IsZero() {
							t.Error("fallback backoff invented an unknown quota reset")
						}
					} else if retryAt.Before(start.Add(test.delay)) || retryAt.After(time.Now().Add(test.delay)) {
						t.Errorf("retry ignored the controlling deadline: retryAt=%v expected delay=%v", retryAt, test.delay)
					}
					return nil
				},
				BeforeWait: func(_ context.Context, deadline time.Time) error {
					waited = true
					if !deadline.Equal(archivedDeadline) {
						t.Errorf("wait bypassed the archived deadline: waited=%v archived=%v", deadline, archivedDeadline)
					}
					cancel()
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = true
			response, err := client.Do(ctx, Request{})
			if test.delay > 0 {
				if !waited || !errors.Is(err, context.Canceled) || response.StatusCode != 0 || client.lastResponse().StatusCode != 0 {
					t.Fatalf("cancelled wait replayed committed diagnostics: waited=%t response=%+v last=%+v err=%v", waited, response, client.lastResponse(), err)
				}
			} else {
				if err == nil || waited {
					t.Fatalf("unknown quota reset allowed an automatic retry: waited=%t err=%v", waited, err)
				}
				if _, err := client.Do(ctx, Request{}); !errors.Is(err, errQuota) {
					t.Fatalf("unknown quota reset was bypassed by a later request: %v", err)
				}
			}
			if requests.Load() != 1 || archived.Load() != 1 {
				t.Fatalf("cancelled or quota-blocked response retried: requests=%d archived=%d", requests.Load(), archived.Load())
			}
		})
	}
}

func TestTransientCallbackFailuresAbortWithoutReplayingResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel bool
		err    error
	}{
		{name: "archive failure", err: errors.New("archive unavailable")},
		{name: "duration limit", err: context.DeadlineExceeded},
		{name: "cancelled after archive", cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("original gateway response"))
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client, err := NewClient(ctx, protocolProvider("http_json", server.URL), Environment{
				TransientFailure: func(context.Context, Response, time.Time, int, bool) error {
					archived.Add(1)
					if test.cancel {
						cancel()
					}
					return test.err
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = true
			response, err := client.Do(ctx, Request{})
			if err == nil || response.StatusCode != 0 || len(response.Body) != 0 || client.lastResponse().StatusCode != 0 || requests.Load() != 1 || archived.Load() != 1 {
				t.Fatalf("callback failure retried or replayed bytes: response=%+v requests=%d archived=%d last=%+v err=%v", response, requests.Load(), archived.Load(), client.lastResponse(), err)
			}
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation was obscured: %v", err)
			}
			if errors.Is(test.err, context.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("duration limit was obscured: %v", err)
			}
		})
	}
}
