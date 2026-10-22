package aiosource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDebridCapacityEnforcesSavedLimitsAndResizing(t *testing.T) {
	ctx, s, _ := testService(t)
	settings, err := s.DB.RuntimeSection(ctx, "debrid")
	if err != nil {
		t.Fatal(err)
	}
	settings.Values["download_jobs"] = 2
	if err := s.DB.UpdateRuntimeSection(ctx, "debrid", settings); err != nil {
		t.Fatal(err)
	}
	gate := &capacityGate{}
	for range 2 {
		if err := s.acquireCapacity(ctx, gate, "download_jobs", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.acquireCapacity(ctx, gate, "download_jobs", false); err == nil {
		t.Fatal("exceeded saved capacity")
	}
	settings, _ = s.DB.RuntimeSection(ctx, "debrid")
	settings.Values["download_jobs"] = 1
	if err := s.DB.UpdateRuntimeSection(ctx, "debrid", settings); err != nil {
		t.Fatal(err)
	}
	gate.release()
	if err := s.acquireCapacity(ctx, gate, "download_jobs", false); err == nil {
		t.Fatal("shrink admitted work while active")
	}
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := s.acquireCapacity(wait, gate, "download_jobs", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting cancellation", err)
	}
	gate.release()
	if err := s.acquireCapacity(ctx, gate, "download_jobs", false); err != nil {
		t.Fatal(err)
	}
	gate.release()
	if gate.count() != 0 {
		t.Fatal("capacity permit leak")
	}
}

func TestSavedOutgoingTimeoutsReachActualProviderAndMetadataRequests(t *testing.T) {
	ctx, s, _ := testService(t)
	policy, err := s.DB.RuntimeSection(ctx, "outgoing")
	if err != nil {
		t.Fatal(err)
	}
	policy.Values["debrid_seconds"], policy.Values["metadata_seconds"] = 20, 17
	if err := s.DB.UpdateRuntimeSection(ctx, "outgoing", policy); err != nil {
		t.Fatal(err)
	}
	check := func(expected time.Duration, body string) metadataRoundTrip {
		return func(r *http.Request) (*http.Response, error) {
			deadline, ok := r.Context().Deadline()
			remaining := time.Until(deadline)
			if !ok || remaining < expected-time.Second || remaining > expected+time.Second {
				t.Errorf("configured timeout not applied: %v, expected %v", remaining, expected)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
	}
	s.Providers.Client.Transport = check(20*time.Second, `{"success":true,"data":{}}`)
	if _, err := s.Providers.request(ctx, "torbox", "synthetic-service-key", http.MethodGet, "torrents/mylist", nil, ""); err != nil {
		t.Fatal(err)
	}
	s.MetadataHTTP.Transport = check(17*time.Second, `{}`)
	var out map[string]any
	if found, err := s.metadataGET(ctx, "synthetic-metadata-key", "movie/1", &out); err != nil || !found {
		t.Fatal(found, err)
	}
}

func TestCleanupCapacityAlsoBoundsRecoveryDeletionPaths(t *testing.T) {
	ctx, s, _ := testService(t)
	policy, _ := s.DB.RuntimeSection(ctx, "debrid")
	policy.Values["cleanup_jobs"] = 1
	if err := s.DB.UpdateRuntimeSection(ctx, "debrid", policy); err != nil {
		t.Fatal(err)
	}
	const key = "synthetic-cleanup-capacity-key"
	for i, hash := range []string{strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		if err := s.DB.AddAIOCleanup(ctx, model.AIOCleanup{Account: fingerprint(key), Hash: hash, ID: int64(i + 1)}); err != nil {
			t.Fatal(err)
		}
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	s.Providers.Client.Transport = metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); count > previous && !peak.CompareAndSwap(previous, count); previous = peak.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"success","data":{"message":"deleted"}}`)), Request: r}, nil
	})
	finished := make(chan error, 1)
	go func() { finished <- s.cleanupHash(ctx, key, strings.Repeat("a", 40), false) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first cleanup did not begin")
	}
	waiting, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := s.cleanupHash(waiting, key, strings.Repeat("b", 40), false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("excess cleanup did not wait for capacity", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 1 {
		t.Fatal("cleanup exceeded saved capacity", peak.Load())
	}
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(jobs) != 1 || jobs[0].ID != 2 {
		t.Fatal("waiting cleanup lost durable ownership", err)
	}
	if err := s.cleanupHash(ctx, key, strings.Repeat("b", 40), false); err != nil {
		t.Fatal(err)
	}
}
