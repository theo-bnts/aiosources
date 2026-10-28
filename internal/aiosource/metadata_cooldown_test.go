package aiosource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestTMDBProviderCooldownSurvivesRestartAndManualRetry(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			ctx, s, conn := testService(t)
			if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.TMDBSecret = "tmdb"
			var calls atomic.Int32
			var blocked atomic.Bool
			blocked.Store(true)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if blocked.Load() {
					w.Header().Set("Retry-After", "3600")
					w.WriteHeader(status)
				} else {
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer server.Close()
			s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
			bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			err := s.Sync(bounded, c)
			var remote *metadataError
			if !errors.As(err, &remote) || remote.status != status || calls.Load() != 1 {
				t.Fatal("a long provider wait blocked the worker or issued extra requests", err, calls.Load())
			}
			before, err := s.DB.AIOAutomationStatus(ctx)
			if err != nil || before.ProviderRetryAt == nil || time.Until(*before.ProviderRetryAt) < 59*time.Minute || before.SyncRetryAt == nil || before.SyncRetryAt.Before(*before.ProviderRetryAt) {
				t.Fatal("provider cooldown was replaced by the ordinary retry delay", before, err)
			}
			if err := s.Close(ctx); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(ctx, s.DB, s.Vault, s.sources)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = restarted.Close(ctx) })
			restarted.TMDBBase, restarted.MetadataHTTP = server.URL, server.Client()
			err = restarted.Sync(ctx, c)
			if !errors.As(err, &remote) || remote.code != "cooldown" || calls.Load() != 1 {
				t.Fatal("restart or a manual retry bypassed the saved cooldown", err, calls.Load())
			}
			after, err := s.DB.AIOAutomationStatus(ctx)
			c.AutoSync = true
			plan := AutomationPlan(c, after, false, false, time.Now())
			if err != nil || plan.Updates != "retry" || plan.NextUpdate == nil || plan.NextUpdate.Before(*before.ProviderRetryAt) {
				t.Fatal("the displayed automatic schedule ignored the provider deadline", plan, err)
			}
			if _, err := conn.Exec(ctx, `UPDATE ingest.aio_state SET provider_retry_at=now()-interval '1 minute',sync_retry_at=now()-interval '1 minute'`); err != nil {
				t.Fatal(err)
			}
			blocked.Store(false)
			if err := restarted.Sync(ctx, c); !errors.Is(err, ErrAuth) || calls.Load() != 2 {
				t.Fatal("the expired cooldown did not release requests", err, calls.Load())
			}
		})
	}
}
