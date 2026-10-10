package connectors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRequestLimitsCannotRunWithoutDurableAdmission(t *testing.T) {
	provider := protocolProvider("http_json", "https://example.invalid/api")
	provider.RequestLimits = &model.RequestLimits{PerMinute: 30}
	client, err := NewClient(t.Context(), provider, Environment{})
	if client != nil || err == nil {
		if client != nil {
			_ = client.Close()
		}
		t.Fatal("configured request limits were silently ignored")
	}
}

func TestQuotaAdmissionWaitDoesNotConsumeNetworkTimeoutOrPacing(t *testing.T) {
	requests := make(chan time.Time, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- time.Now()
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	provider := protocolProvider("http_json", server.URL)
	provider.RequestInterval = "100ms"
	provider.RequestTimeout = "250ms"
	provider.RequestLimits = &model.RequestLimits{PerMinute: 2}
	var admissions atomic.Int32
	client, err := NewClient(t.Context(), provider, Environment{
		BeforeRequest: func(ctx context.Context) error {
			if admissions.Add(1) != 1 {
				return nil
			}
			timer := time.NewTimer(400 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for range 2 {
		if _, err := client.Do(t.Context(), Request{Method: http.MethodGet}); err != nil {
			t.Fatalf("quota wait consumed the network timeout: %v", err)
		}
	}
	first, second := <-requests, <-requests
	if second.Sub(first) < 90*time.Millisecond {
		t.Fatalf("requests burst after a long quota wait: spacing=%s", second.Sub(first))
	}
}

func TestCancelledAdmissionNeverReachesUpstream(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	provider := protocolProvider("http_json", server.URL)
	provider.RequestLimits = &model.RequestLimits{PerMinute: 1}
	waiting := make(chan struct{})
	client, err := NewClient(t.Context(), provider, Environment{
		BeforeRequest: func(ctx context.Context) error {
			close(waiting)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := client.Do(ctx, Request{Method: http.MethodGet})
		finished <- err
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach admission")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) || requests.Load() != 0 {
			t.Fatalf("cancelled admission sent a request: calls=%d error=%v", requests.Load(), err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation remained blocked behind admission")
	}
}

func TestRetryRequiresAnotherAdmissionAndRetainsRejectedResponse(t *testing.T) {
	var requests, admissions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("quota response"))
	}))
	t.Cleanup(server.Close)
	provider := protocolProvider("http_json", server.URL)
	provider.RequestInterval = "1ns"
	provider.RequestLimits = &model.RequestLimits{PerMinute: 1}
	client, err := NewClient(t.Context(), provider, Environment{
		BeforeRequest: func(context.Context) error {
			if admissions.Add(1) > 1 {
				return errors.New("admission blocked")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	response, err := client.Do(t.Context(), Request{Method: http.MethodGet})
	if err == nil || requests.Load() != 1 || response.StatusCode != http.StatusTooManyRequests || string(response.Body) != "quota response" {
		t.Fatalf("retry bypassed admission or lost the response: calls=%d status=%d error=%v", requests.Load(), response.StatusCode, err)
	}
}

func TestTransientRetryAdmitsEveryAttemptAndStopsAtBudget(t *testing.T) {
	for _, test := range []struct {
		name       string
		budget     int32
		requests   int32
		admissions int32
	}{
		{name: "all retries admitted", budget: 4, requests: 4, admissions: 4},
		{name: "retry budget exhausted", budget: 2, requests: 2, admissions: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests, admissions, archived atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := requests.Add(1)
				if admissions.Load() != n || archived.Load() != n-1 {
					t.Error("network attempt bypassed admission or response retention")
				}
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("gateway response"))
			}))
			t.Cleanup(server.Close)
			provider := protocolProvider("http_json", server.URL)
			provider.RequestLimits = &model.RequestLimits{PerMinute: int(test.budget)}
			client, err := NewClient(t.Context(), provider, Environment{
				BeforeRequest: func(context.Context) error {
					if admissions.Add(1) > test.budget {
						return context.DeadlineExceeded
					}
					return nil
				},
				TransientFailure: func(context.Context, Response, time.Time, int, bool) error {
					archived.Add(1)
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			client.returnEveryResponse = true
			response, err := client.Do(t.Context(), Request{})
			if err == nil || requests.Load() != test.requests || admissions.Load() != test.admissions || archived.Load() != test.requests {
				t.Fatalf("retry admission boundary failed: requests=%d admissions=%d archived=%d err=%v", requests.Load(), admissions.Load(), archived.Load(), err)
			}
			if test.budget < 4 && (!errors.Is(err, context.DeadlineExceeded) || response.StatusCode != 0 || client.lastResponse().StatusCode != 0) {
				t.Fatalf("budget stop replayed committed evidence: response=%+v last=%+v err=%v", response, client.lastResponse(), err)
			}
		})
	}
}
