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
)

func TestPublicADOwnershipFailuresAndCancellationNeverBorrowAdministratorKey(t *testing.T) {
	for _, scenario := range []string{"cached", "uncached", "existing", "incomplete", "cancelled", "cleanup failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, s, conn := testService(t)
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			hash := strings.Repeat("b", 40)
			var adds, deletes atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer visitor-owned-check" {
					t.Error("visitor check used another account")
					w.WriteHeader(401)
					return
				}
				_ = r.ParseForm()
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					if scenario == "existing" {
						fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","statusCode":4}]}}`, hash)
					} else {
						fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
					}
				case "/v4/magnet/upload":
					adds.Add(1)
					if r.Form.Get("magnets[]") != hash {
						t.Error("cache probe exposed a source URL or tracker")
					}
					if scenario == "incomplete" {
						fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s"}]}}`, hash)
					} else {
						fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":%t}]}}`, hash, scenario != "uncached")
					}
				case "/v4/magnet/files":
					if scenario == "cancelled" {
						cancel()
						return
					}
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.mkv","s":100,"l":"https://provider.example/visitor-only"}]}]}}`)
				case "/v4/magnet/delete":
					deletes.Add(1)
					if r.Form.Get("id") != "77" {
						t.Error("deleted an unrelated entry")
					}
					if scenario == "cleanup failure" {
						w.WriteHeader(503)
						return
					}
					fmt.Fprint(w, `{"status":"success","data":{"message":"deleted"}}`)
				default:
					t.Error("unexpected provider request")
				}
			}))
			defer upstream.Close()
			s.Providers.ADBase = upstream.URL
			items, err := s.checkPublicService(request, "alldebrid", "visitor-owned-check", "example", []string{hash})
			wantError := scenario == "incomplete" || scenario == "cancelled" || scenario == "cleanup failure"
			if (err != nil) != wantError {
				t.Fatal("probe outcome", scenario, err)
			}
			if scenario == "existing" {
				if adds.Load() != 0 || deletes.Load() != 0 || len(items) != 1 || !items[0].Cached {
					t.Fatal("existing personal entry was modified")
				}
			} else if adds.Load() != 1 || deletes.Load() != 1 {
				t.Fatal("confirmed temporary entry was not cleaned", adds.Load(), deletes.Load())
			}
			var accountRows int
			if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_resources)+(SELECT count(*) FROM aiosources.aio_cleanup)+(SELECT count(*) FROM aiosources.aio_availability)`).Scan(&accountRows); err != nil || accountRows != 0 {
				t.Fatal("visitor state was persisted for background work", accountRows, err)
			}
			shared, err := s.DB.SharedAvailability(ctx, "alldebrid", []string{hash}, time.Now().Add(-time.Minute))
			wantShared := scenario == "cached" || scenario == "cleanup failure"
			if err != nil || (len(shared) == 1) != wantShared {
				t.Fatal("account-specific or incomplete evidence was shared", len(shared), err)
			}
			if wantShared && (shared[0].Files[0].Link != "" || shared[0].Files[0].ID != 0) {
				t.Fatal("account-bound file data leaked into shared cache")
			}
		})
	}
}

func TestPublicADCacheIsReusedAcrossVisitorsAdministratorsAndLimitedRequests(t *testing.T) {
	ctx, s, _ := testService(t)
	hash := strings.Repeat("a", 40)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer first-visitor" {
			t.Error("reused evidence required another credential or administrator key")
		}
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
		case "/v4/magnet/upload":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":77,"hash":"%s","ready":true}]}}`, hash)
		case "/v4/magnet/files":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":77,"files":[{"n":"Example.mkv","s":100,"l":"https://provider.example/private"}]}]}}`)
		case "/v4/magnet/delete":
			fmt.Fprint(w, `{"status":"success","data":{"message":"deleted"}}`)
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	s.Providers.ADBase = server.URL
	c, _ := s.DB.AIOConfig(ctx)
	c.ResponseSeconds = 5
	visitor := &PublicInstallation{Services: map[string]string{"alldebrid": "first-visitor"}}
	plan, err := s.planAvailability(ctx, c, []string{hash}, visitor)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plan.keys)
	states, err := s.limitedAvailability(ctx, c, "example", []string{hash}, plan, 1, Resource{Type: "movie"}, nil)
	if err != nil || !states["alldebrid"][hash].Cached || calls.Load() != 4 {
		t.Fatal("limited visitor check did not use its own key without an admin key", err, calls.Load())
	}
	visitor.Services["alldebrid"] = "second-visitor"
	states, err = s.publicAvailability(ctx, c, "other", []string{hash}, visitor)
	if err != nil || !states["alldebrid"][hash].Cached || calls.Load() != 4 {
		t.Fatal("second visitor failed to reuse shared availability", err)
	}
	if err = s.Vault.Put(ctx, "administrator", "unused-administrator-key"); err != nil {
		t.Fatal(err)
	}
	c.AllDebridSecret = "administrator"
	states, err = s.Availability(ctx, c, "admin", []string{hash})
	if err != nil || !states["alldebrid"][hash].Cached || calls.Load() != 4 {
		t.Fatal("administrator failed to reuse shared availability", err)
	}
}

func TestAutomaticCacheSettingStopsRechecksWithoutStoppingRequestedChecks(t *testing.T) {
	ctx, s, _ := testService(t)
	c, _ := s.DB.AIOConfig(ctx)
	if !c.AutoCache {
		t.Fatal("automatic rechecks should default on")
	}
	c.AutoCache = false
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	transport := &savedCacheTransport{}
	s.Providers.Client = &http.Client{Transport: transport}
	hash := strings.Repeat("a", 40)
	if err := s.checkResourceTriggered(ctx, "torbox", "synthetic-key", "example", []string{hash}, c, "automatic"); err != nil || transport.calls.Load() != 0 {
		t.Fatal("disabled automatic work contacted a provider", err)
	}
	if err := s.checkResourceTriggered(ctx, "torbox", "synthetic-key", "example", []string{hash}, c, "playback"); err == nil || transport.calls.Load() != 1 {
		t.Fatal("disabled automatic work prevented an explicit check", err)
	}
	flightCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.flights["queued-automatic"] = &flight{automatic: true, cancel: cancel}
	s.mu.Unlock()
	c, _ = s.DB.AIOConfig(ctx)
	if err := s.SaveConfig(ctx, c); err != nil || !errors.Is(flightCtx.Err(), context.Canceled) {
		t.Fatal("saving disabled did not cancel queued automatic work", err)
	}
	s.mu.Lock()
	delete(s.flights, "queued-automatic")
	s.mu.Unlock()
}
