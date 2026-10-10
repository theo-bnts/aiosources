package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestJSONReadRecoversHTMLAfterRateLimitWithinExistingAttemptLimit(t *testing.T) {
	for _, test := range []struct {
		name, method, adapter, body, contentType string
		quota, recover                           bool
		requests, htmlFailures                   int32
	}{
		{name: "recovery", quota: true, recover: true, requests: 3, htmlFailures: 1},
		{name: "exhaustion", quota: true, requests: 4, htmlFailures: 3},
		{name: "ordinary HTML", requests: 1},
		{name: "JSON mislabeled as HTML", quota: true, body: `{"items":[]}`, requests: 2},
		{name: "POST", quota: true, method: http.MethodPost, requests: 2},
		{name: "XML adapter", quota: true, adapter: "torznab", requests: 2},
		{name: "invalid JSON", quota: true, contentType: "application/json", body: `{invalid`, requests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, archived, htmlFailures atomic.Int32
			method, adapter, body, contentType := test.method, test.adapter, test.body, test.contentType
			if method == "" {
				method = http.MethodGet
			}
			if adapter == "" {
				adapter = "http_json"
			}
			if body == "" {
				body = "<html>private temporary response</html>"
			}
			if contentType == "" {
				contentType = "text/html; charset=utf-8"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				if r.Method != method || r.URL.RequestURI() != "/api?page=66292" {
					t.Error("recovery changed the original operation")
				}
				if test.htmlFailures > 0 && archived.Load() != n-1 {
					t.Error("retry started before archiving the rejected response")
				}
				w.Header().Set("Retry-After", "0")
				if n == 1 && test.quota {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				if test.recover && n == 3 {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"items":[]}`))
					return
				}
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			p := protocolProvider(adapter, server.URL+"/api?page=66292")
			p.RequestInterval = "1ns"
			client, err := NewClient(t.Context(), p, Environment{
				RateLimited: func(_ context.Context, r Response, _ time.Time, retry bool) error {
					archived.Add(1)
					if r.StatusCode != 429 || !retry {
						t.Error("rate-limit response lost its bounded retry")
					}
					return nil
				},
				TransientFailure: func(_ context.Context, r Response, deadline time.Time, attempt int, retry bool) error {
					archived.Add(1)
					htmlFailures.Add(1)
					if !r.UnexpectedHTML || r.StatusCode != 200 || string(r.Body) != body || deadline.IsZero() || retry != (attempt < 4) {
						t.Error("HTML response lost its evidence or bounded cooldown")
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
			if requests.Load() != test.requests || htmlFailures.Load() != test.htmlFailures {
				t.Fatalf("incorrect replay: requests=%d HTML failures=%d error=%v", requests.Load(), htmlFailures.Load(), err)
			}
			if test.name == "exhaustion" {
				if err == nil || FailureCode(err) != "parse" || !strings.Contains(err.Error(), "HTML instead of JSON") || strings.Contains(err.Error(), "private") || response.StatusCode != 200 {
					t.Fatalf("exhausted recovery lost its safe cause: error=%v status=%d", err, response.StatusCode)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
