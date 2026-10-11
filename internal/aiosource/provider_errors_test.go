package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestProviderFailuresKeepSafeCategoriesAndNeverRetainPayloads(t *testing.T) {
	for _, tc := range []struct {
		service, apiCode, category string
		status                     int
	}{
		{"alldebrid", "MAGNET_TOO_MANY", "account_limit", 200},
		{"alldebrid", "MAGNET_TOO_MANY_ACTIVE", "account_limit", 429},
		{"alldebrid", "MAGNET_MUST_BE_PREMIUM", "subscription", 200},
		{"alldebrid", "MAGNET_TOO_LARGE", "file_too_large", 200},
		{"alldebrid", "MAGNET_INVALID_URI", "invalid_torrent", 200},
		{"alldebrid", "MAGNET_INVALID_ID", "not_found", 200},
		{"alldebrid", "MAINTENANCE", "provider_unavailable", 503},
		{"alldebrid", "AUTH_BAD_APIKEY", "authentication", 200},
		{"alldebrid", "AUTH_BAD_APIKEY", "rate_limit", 429},
		{"torbox", "ACTIVE_LIMIT", "account_limit", 400},
		{"torbox", "MONTHLY_LIMIT", "account_limit", 400},
		{"torbox", "COOLDOWN_LIMIT", "rate_limit", 400},
		{"torbox", "COOLDOWN_LIMIT", "rate_limit", 200},
		{"torbox", "PLAN_RESTRICTED_FEATURE", "subscription", 403},
		{"torbox", "DOWNLOAD_TOO_LARGE", "file_too_large", 400},
		{"torbox", "BOZO_TORRENT", "invalid_torrent", 400},
		{"torbox", "ITEM_NOT_FOUND", "not_found", 400},
		{"torbox", "BAD_TOKEN", "authentication", 403},
		{"torbox", "DATABASE_ERROR", "provider_unavailable", 500},
		{"torbox", "DOWNLOAD_SERVER_ERROR", "provider_unavailable", 200},
		{"torbox", "synthetic-private-key https://private.example/announce", "provider_error", 400},
	} {
		t.Run(tc.service+"/"+tc.apiCode, func(t *testing.T) {
			const private = "synthetic-private-key https://private.example/announce"
			p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				if tc.service == "alldebrid" {
					fmt.Fprintf(w, `{"status":"error","error":{"code":%q,"message":%q},"data":null}`, tc.apiCode, private)
				} else {
					fmt.Fprintf(w, `{"success":false,"error":%q,"detail":%q,"data":null}`, tc.apiCode, private)
				}
			})
			history := &playbackCalls{}
			ctx := context.WithValue(context.Background(), providerHistoryKey{}, history)
			_, err := p.request(ctx, tc.service, "synthetic-provider-key", "POST", "v4/magnet/upload", nil, "")
			var failure *ProviderError
			if !errors.As(err, &failure) || failure.Code != tc.category || (errors.Is(err, ErrAuth) != (tc.category == "authentication")) {
				t.Fatalf("incorrect provider failure: %v", err)
			}
			if strings.Contains(err.Error(), "synthetic-private") || strings.Contains(err.Error(), "private.example") {
				t.Fatal("provider error retained credential-bearing text")
			}
			if (tc.category == "rate_limit" || tc.category == "provider_unavailable") && failure.RetryAfter != 120 {
				t.Fatalf("provider retry header lost: %d", failure.RetryAfter)
			}
			if len(history.calls) != 1 || history.calls[0].ErrorCode != tc.category || history.calls[0].HTTPStatus != tc.status || !history.calls[0].Valid() {
				t.Fatalf("actual failed call not classified: %+v", history.calls)
			}
		})
	}
}

func TestAllDebridItemAndNestedErrorsRemainDiagnosable(t *testing.T) {
	for _, data := range []string{
		`{"error":{"code":"MAGNET_TOO_MANY","message":"synthetic-private-key"}}`,
		`{"magnets":[{"error":{"code":"MAGNET_TOO_MANY","message":"synthetic-private-key"}}]}`,
	} {
		p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `{"status":"success","data":%s}`, data)
		})
		history := &playbackCalls{}
		ctx := context.WithValue(context.Background(), providerHistoryKey{}, history)
		magnet, err := p.ADAdd(ctx, "synthetic-key", strings.Repeat("a", 40), strings.Repeat("a", 40))
		var failure *ProviderError
		if !errors.As(err, &failure) || failure.Code != "account_limit" || magnet.ID != 0 || len(history.calls) != 1 || history.calls[0].ErrorCode != "account_limit" {
			t.Fatal("item error became a parse error or a successful call", err, history.calls)
		}
	}
}

func TestProviderHTTPFailuresAndRetryHeadersDoNotDependOnValidJSON(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 503} {
		p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "999999")
			w.WriteHeader(status)
			fmt.Fprint(w, "<html>synthetic-private-provider-key</html>")
		})
		_, err := p.request(context.Background(), "torbox", "synthetic-key", "GET", "test", nil, "")
		var failure *ProviderError
		if !errors.As(err, &failure) || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("HTTP error required valid JSON or retained its body", err)
		}
		if status == 429 && (failure.Code != "rate_limit" || failure.RetryAfter != 3600) {
			t.Fatal("rate limit lost or unbounded", failure)
		}
	}
	for _, value := range []string{"", "0", "-1", "999999", "invalid", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)} {
		err := newProviderError("rate_limit", value)
		if err.RetryAfter < 1 || err.RetryAfter > 3600 {
			t.Fatal("unbounded retry header", err.RetryAfter)
		}
	}
}

func TestAccountLimitStopsAllDebridBatchWithoutNegativeCacheOrUserDeletion(t *testing.T) {
	ctx, s, _ := testService(t)
	hashes := make([]string, 80)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%040x", i+1)
	}
	var requests atomic.Int32
	p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		// Requests are paced, but different response goroutines can overlap.
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
		case "/v4/magnet/upload":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"error":{"code":"MAGNET_TOO_MANY"}}]}}`)
		default:
			t.Error("account magnets were deleted to free capacity")
			w.WriteHeader(500)
		}
	})
	// Count actual transport calls without sharing an unsynchronized handler counter.
	p.Client.Transport = providerCountingTransport{base: p.Client.Transport, count: &requests}
	s.Providers = p
	c, _ := s.DB.AIOConfig(ctx)
	if err := s.checkResource(ctx, "alldebrid", "synthetic-key", "movie:tt1234567", hashes, c); !stopProviderChecks(err) || providerFailureCode(err) != "account_limit" {
		t.Fatal("full-account failure did not stop the batch", err)
	}
	if requests.Load() > 12 {
		t.Fatalf("continued probing a full account: %d calls", requests.Load())
	}
	availability, err := s.DB.AIOAvailability(ctx, "alldebrid", fingerprint("synthetic-key"), hashes, time.Now().Add(-time.Hour))
	if err != nil || len(availability) != 0 {
		t.Fatal("quota failure became uncached availability", err)
	}
	before := requests.Load()
	if err := s.checkResource(ctx, "alldebrid", "synthetic-key", "movie:tt1234567", hashes, c); err != nil || requests.Load() != before {
		t.Fatal("durable retry delay was bypassed", err)
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
	if err != nil || runs.Total != 1 || !strings.Contains(runs.Items[0].Error, "limit") {
		t.Fatal("quota absent from operation history", runs, err)
	}
	encoded, _ := json.Marshal(runs)
	if strings.Contains(string(encoded), "synthetic-key") {
		t.Fatal("provider key retained in history")
	}
}

func TestProviderLimitPreservesCachedResultsAndRecoversAfterSavedRetryDelay(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, _ := testService(t)
			const key = "synthetic-key"
			known, pending := strings.Repeat("a", 40), strings.Repeat("b", 40)
			c, err := s.DB.AIOConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			c.CacheMinutes = 60
			generation, err := s.DB.AIOCacheGeneration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			resource := model.AIOResource{Generation: generation, Service: service, Account: fingerprint(key), Key: "movie:example", Hashes: []string{known, pending}, CheckedAt: time.Now(), LastAttempt: time.Now().Add(-20 * time.Minute), Retry: 1}
			if err := s.DB.SaveAIOResource(ctx, resource); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.SaveAIOAvailability(ctx, service, resource.Account, model.AIOAvailability{Generation: generation, Hash: known, Cached: true, CheckedAt: resource.CheckedAt}); err != nil {
				t.Fatal(err)
			}
			var blocked atomic.Bool
			blocked.Store(true)
			var requests atomic.Int32
			p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				if blocked.Load() {
					w.WriteHeader(http.StatusTooManyRequests)
					if service == "alldebrid" {
						fmt.Fprint(w, `{"status":"error","error":{"code":"MAGNET_TOO_MANY_ACTIVE"}}`)
					} else {
						fmt.Fprint(w, `{"success":false,"error":"ACTIVE_LIMIT","data":null}`)
					}
					return
				}
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":"%s","statusCode":4}]}}`, pending)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Example.mp4","s":100,"l":"https://files.example/item"}]}]}}`)
				case "/torrents/checkcached":
					if r.URL.Query().Get("hash") != pending {
						t.Error("retry included previously confirmed cached results")
					}
					fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example","size":100,"files":[]}}}`, pending, pending)
				default:
					t.Error("retry created or deleted a personal torrent")
					w.WriteHeader(500)
				}
			})
			p.Client.Transport = providerCountingTransport{base: p.Client.Transport, count: &requests}
			s.Providers = p
			if err := s.checkResource(ctx, service, key, resource.Key, resource.Hashes, c); !stopProviderChecks(err) {
				t.Fatal("provider limit did not stop verification", err)
			}
			values, err := s.DB.AIOAvailability(ctx, service, resource.Account, resource.Hashes, time.Now().Add(-time.Hour))
			if err != nil || len(values) != 1 || !values[0].Cached || values[0].Hash != known {
				t.Fatal("quota failure changed saved availability", values, err)
			}
			before := requests.Load()
			if err := s.checkResource(ctx, service, key, resource.Key, resource.Hashes, c); err != nil || requests.Load() != before {
				t.Fatal("provider limit bypassed the saved retry delay", err)
			}
			resource, found, err := s.DB.AIOResource(ctx, service, resource.Account, resource.Key)
			if err != nil || !found {
				t.Fatal("retry checkpoint lost", err)
			}
			resource.LastAttempt = time.Now().Add(-RetryDelay(resource.Retry-1) - time.Minute)
			resource.Generation = generation
			if err := s.DB.SaveAIOResource(ctx, resource); err != nil {
				t.Fatal(err)
			}
			blocked.Store(false)
			if err := s.checkResource(ctx, service, key, resource.Key, resource.Hashes, c); err != nil || requests.Load() <= before {
				t.Fatal("next due check did not recover", err)
			}
			values, err = s.DB.AIOAvailability(ctx, service, resource.Account, resource.Hashes, time.Now().Add(-time.Hour))
			if err != nil || len(values) != 2 || !values[0].Cached || !values[1].Cached {
				t.Fatal("recovered check lost availability", values, err)
			}
			runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
			if err != nil || runs.Total != 2 || runs.Items[0].Status != "succeeded" || runs.Items[1].Status != "failed" {
				t.Fatal("failure and recovery missing from history", runs, err)
			}
		})
	}
}

type providerCountingTransport struct {
	base  http.RoundTripper
	count *atomic.Int32
}

func (p providerCountingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p.count.Add(1)
	return p.base.RoundTrip(r)
}
