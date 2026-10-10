package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCacheResetCancelsWarmRetryAndItsExpiryFollowUp(t *testing.T) {
	ctx, s, _ := testService(t)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) != 1 {
			t.Error("reset allowed the queued expiry check to contact the provider")
			return
		}
		close(entered)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
	hashes := []string{strings.Repeat("a", 40)}
	previous, err := s.cacheFlightTriggered("torbox", "synthetic-key", "movie:example", hashes, c, "playback", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(previous.cancel)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first check did not contact the provider")
	}
	next, err := s.cacheFlightTriggered("torbox", "synthetic-key", "movie:example", hashes, c, "playback", false)
	if err != nil || previous == next {
		t.Fatal("expired resource did not queue a full follow-up", err)
	}
	t.Cleanup(next.cancel)
	reset, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Reset(reset, "cache"); err != nil {
		t.Fatal(err)
	}
	for _, check := range []*flight{previous, next} {
		select {
		case <-check.done:
			if !errors.Is(check.err, context.Canceled) {
				t.Fatalf("reset did not cancel a cache check: %v", check.err)
			}
		case <-reset.Done():
			t.Fatal("reset left an owned check running")
		}
	}
	if _, found, err := s.DB.AIOResource(ctx, "torbox", fingerprint("synthetic-key"), "movie:example"); err != nil || found {
		t.Fatal("a canceled check republished resource state after reset", err)
	}
}

func TestExpiredResourceJoiningWarmRetryRechecksEveryCandidate(t *testing.T) {
	ctx, s, conn := testService(t)
	key, known, pending := "synthetic-key", strings.Repeat("a", 40), strings.Repeat("b", 40)
	if err := s.Vault.Put(ctx, "debrid", key); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret, c.ResponseSeconds = "debrid", 3
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resource := model.AIOResource{Generation: generation, Service: "torbox", Account: fingerprint(key), Key: "movie:example", Hashes: []string{known, pending}, CheckedAt: time.Now().Add(-time.Hour), LastAttempt: time.Now().Add(-49 * time.Hour), Retry: 1}
	if err := s.DB.SaveAIOResource(ctx, resource); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.SaveAIOAvailability(ctx, "torbox", resource.Account, model.AIOAvailability{Generation: generation, Hash: known, Cached: true, CheckedAt: resource.CheckedAt}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			if r.URL.Query().Get("hash") != pending {
				t.Error("warm retry included an already positive candidate")
			}
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		case 2:
			if r.URL.Query().Get("hash") != known+","+pending {
				t.Error("full expiry check omitted a previously positive candidate")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example","size":100,"files":[]}}}`, known, known)
		default:
			t.Error("duplicated the full expiry check")
		}
	}))
	t.Cleanup(server.Close)
	var released bool
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
	if values, err := s.Availability(ctx, c, resource.Key, resource.Hashes); err != nil || !values["torbox"][known].Cached {
		t.Fatal("warm cache was not served", err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("warm retry did not start")
	}
	if _, err := conn.Exec(ctx, `UPDATE ingest.aio_resources SET checked_at=now()-interval '4 days'; UPDATE ingest.aio_availability SET checked_at=now()-interval '4 days'`); err != nil {
		t.Fatal(err)
	}
	type response struct {
		values map[string]map[string]model.AIOAvailability
		err    error
	}
	returned := make(chan response, 1)
	go func() {
		values, err := s.Availability(ctx, c, resource.Key, resource.Hashes)
		returned <- response{values, err}
	}()
	select {
	case <-returned:
		t.Fatal("expired resource returned before its full check")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	released = true
	select {
	case got := <-returned:
		if got.err != nil || len(got.values["torbox"]) != 2 || !got.values["torbox"][known].Cached || calls.Load() != 2 {
			t.Fatalf("expiry reused a partial retry: %+v %v calls=%d", got.values, got.err, calls.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("full check follow-up did not finish")
	}
}

func TestColdResourceJoinedDuringFirstCheckStillWaitsForItsResult(t *testing.T) {
	ctx, s, _ := testService(t)
	key, hash := "synthetic-key", strings.Repeat("a", 40)
	if err := s.Vault.Put(ctx, "debrid", key); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret, c.ResponseSeconds = "debrid", 3
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Example","size":100,"files":[]}}}`, hash, hash)
	}))
	t.Cleanup(server.Close)
	var released bool
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
	request := func() <-chan error {
		returned := make(chan error, 1)
		go func() {
			life, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			values, err := s.Availability(life, c, "movie:example", []string{hash})
			if err == nil && !values["torbox"][hash].Cached {
				err = fmt.Errorf("first cache check returned before its observation")
			}
			returned <- err
		}()
		return returned
	}
	first := request()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first check did not start")
	}
	second := request()
	select {
	case err := <-second:
		t.Fatalf("a joining cold request did not wait: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	released = true
	for _, returned := range []<-chan error{first, second} {
		select {
		case err := <-returned:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("shared check did not finish")
		}
	}
}

func TestWarmResourceReturnsObservationsWhileRetryContinues(t *testing.T) {
	ctx, s, _ := testService(t)
	key, known, pending := "synthetic-key", strings.Repeat("a", 40), strings.Repeat("b", 40)
	if err := s.Vault.Put(ctx, "debrid", key); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret, c.ResponseSeconds = "debrid", 3
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	generation, err := s.DB.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// PostgreSQL stores microseconds; host clock precision must not affect
	// the assertion that the original observation survived a background retry.
	checked := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	resource := model.AIOResource{Generation: generation, Service: "torbox", Account: fingerprint(key), Key: "movie:example", Hashes: []string{known, pending}, CheckedAt: checked, LastAttempt: time.Now().Add(-49 * time.Hour), Retry: 1}
	if err := s.DB.SaveAIOResource(ctx, resource); err != nil {
		t.Fatal(err)
	}
	observation := model.AIOAvailability{Generation: generation, Hash: known, Cached: true, CheckedAt: checked, Files: []model.AIOFile{{Path: "Example.mkv", Size: 100}}}
	if err := s.DB.SaveAIOAvailability(ctx, "torbox", fingerprint(key), observation); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents/checkcached" || r.URL.Query().Get("hash") != pending {
			t.Error("a warm retry rechecked an already positive hash")
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
	type response struct {
		values map[string]map[string]model.AIOAvailability
		err    error
	}
	returned := make(chan response, 1)
	go func() {
		values, err := s.Availability(ctx, c, resource.Key, resource.Hashes)
		returned <- response{values, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("retry did not start")
	}
	select {
	case result := <-returned:
		if result.err != nil || len(result.values["torbox"]) != 1 || !result.values["torbox"][known].Cached || !result.values["torbox"][known].CheckedAt.Equal(checked) {
			t.Fatalf("warm response lost a fresh observation: %+v %v", result.values, result.err)
		}
	case <-time.After(600 * time.Millisecond):
		t.Fatal("warm resource waits for its background retry instead of returning existing observations")
	}
}
