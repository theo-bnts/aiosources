package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSavedSourceRetryPolicyControlsAttemptsAndPreservesEveryFailure(t *testing.T) {
	for _, retries := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			var calls, observations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(503)
				_, _ = w.Write([]byte("synthetic unavailable response"))
			}))
			defer server.Close()
			client, err := NewClient(t.Context(), protocolProvider("json", server.URL), Environment{
				SourceRetries: model.SourceRetryPolicy{Retries: retries, InitialSeconds: 2, MaximumSeconds: 8},
				TransientFailure: func(_ context.Context, r Response, _ time.Time, attempt int, retry bool) error {
					observations.Add(1)
					if retry != (attempt <= retries) || string(r.Body) != "synthetic unavailable response" {
						t.Error("retry policy lost response evidence")
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			client.returnEveryResponse = true
			_, err = client.Do(t.Context(), Request{})
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("after %d attempts", retries+1)) || calls.Load() != int32(retries+1) || observations.Load() != calls.Load() {
				t.Fatal("saved source retry count ignored", calls.Load(), observations.Load(), err)
			}
		})
	}
}

func TestSourceRetryDelayHonorsConfiguredBackoffAndProviderDeadline(t *testing.T) {
	policy := model.SourceRetryPolicy{Retries: 5, InitialSeconds: 3, MaximumSeconds: 5}
	now := time.Now()
	if sourceRetryDelay(policy, nil, now, 1) != 3*time.Second || sourceRetryDelay(policy, nil, now, 2) != 5*time.Second || sourceRetryDelay(policy, nil, now, 100000) != 5*time.Second {
		t.Fatal("source backoff did not saturate at configured maximum")
	}
	if got := sourceRetryDelay(policy, http.Header{"Retry-After": []string{"60"}}, now, 1); got != time.Minute {
		t.Fatal("source policy overrode a provider quota deadline", got)
	}
}
