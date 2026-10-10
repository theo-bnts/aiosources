package aiosource

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestAutomationPlanUsesDurableProgressAndSavedChoices(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	recent, expired, retry, gap := now.Add(-10*time.Minute), now.Add(-15*time.Minute), now.Add(time.Minute), now.Add(-15*24*time.Hour)
	c := store.DefaultAIOConfig()
	c.TMDBSecret = "tmdb"
	for _, test := range []struct {
		name                    string
		config                  func(*model.AIOConfig)
		status                  model.AIOStatus
		syncing, matching       bool
		updates, identification string
		next                    *time.Time
	}{
		{name: "initial download", updates: "due", identification: "waiting_information", next: &now},
		{name: "recent completion survives restart", status: model.AIOStatus{LastSync: &recent, Works: 1}, updates: "scheduled", identification: "up_to_date", next: ptrTime(recent.Add(15 * time.Minute))},
		{name: "new season details run without waiting for the next interval", status: model.AIOStatus{LastSync: &recent, Works: 1, MetadataDue: true}, updates: "due", identification: "up_to_date", next: &now},
		{name: "new details respect outage cooldown", status: model.AIOStatus{LastSync: &recent, MetadataDue: true, SyncRetryAt: &retry}, updates: "retry", identification: "waiting_information", next: &retry},
		{name: "interval due at exact boundary", status: model.AIOStatus{LastSync: &expired, Works: 1, MatchingPending: 1}, updates: "due", identification: "pending", next: &now},
		{name: "shorter interval applies immediately", config: func(c *model.AIOConfig) { c.SyncMinutes = 5 }, status: model.AIOStatus{LastSync: &recent}, updates: "due", identification: "waiting_information", next: ptrTime(recent.Add(5 * time.Minute))},
		{name: "retry precedes normal cadence", status: model.AIOStatus{LastSync: &recent, SyncRetryAt: &retry}, updates: "retry", identification: "waiting_information", next: &retry},
		{name: "initial failure waits", status: model.AIOStatus{SyncRetryAt: &retry}, updates: "retry", identification: "waiting_information", next: &retry},
		{name: "expired retry resumes", status: model.AIOStatus{SyncRetryAt: &expired}, updates: "due", identification: "waiting_information", next: &expired},
		{name: "missing token", config: func(c *model.AIOConfig) { c.TMDBSecret = "" }, updates: "missing_token", identification: "waiting_information"},
		{name: "saved manual settings", config: func(c *model.AIOConfig) { c.AutoSync, c.AutoMatch = false, false }, updates: "disabled", identification: "disabled"},
		{name: "information first", status: model.AIOStatus{MatchingPending: 1}, updates: "due", identification: "waiting_information", next: &now},
		{name: "gap needs deliberate reset", status: model.AIOStatus{ChangesThrough: &gap}, updates: "blocked", identification: "waiting_information"},
		{name: "active operations", syncing: true, matching: true, updates: "running", identification: "running"},
		{name: "manual operation remains visible", config: func(c *model.AIOConfig) { c.AutoSync, c.AutoMatch = false, false }, syncing: true, matching: true, updates: "running", identification: "running"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := c
			if test.config != nil {
				test.config(&config)
			}
			p := AutomationPlan(config, test.status, test.syncing, test.matching, now)
			if p.Updates != test.updates || p.Identification != test.identification || (p.NextUpdate == nil) != (test.next == nil) || (p.NextUpdate != nil && !p.NextUpdate.Equal(*test.next)) {
				t.Fatalf("plan %+v, wanted %s / %s / %v", p, test.updates, test.identification, test.next)
			}
		})
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

func TestInformationRetryBackoffIsBounded(t *testing.T) {
	for attempt, minutes := range map[int]int{-1: 1, 0: 1, 1: 1, 2: 2, 3: 4, 4: 8, 5: 15, 30: 15} {
		if got := syncRetryDelay(attempt); got != time.Duration(minutes)*time.Minute {
			t.Fatalf("attempt %d: %v", attempt, got)
		}
	}
}

func waitAutomation(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("automatic operation did not settle")
}

func automaticTMDB(t *testing.T, s *Service, unavailable *atomic.Bool, requests *atomic.Int32) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if unavailable.Load() {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ".json.gz"):
			g := gzip.NewWriter(w)
			if strings.Contains(r.URL.Path, "movie_ids") {
				fmt.Fprintln(g, `{"id":101}`)
			} else {
				fmt.Fprintln(g, `{"id":202}`)
			}
			_ = g.Close()
		case strings.HasSuffix(r.URL.Path, "/changes"):
			fmt.Fprint(w, `{"page":1,"total_pages":1,"results":[]}`)
		case r.URL.Path == "/movie/101":
			fmt.Fprint(w, movieDetail)
		case r.URL.Path == "/tv/202":
			fmt.Fprint(w, tvDetail)
		default:
			t.Errorf("unexpected TMDB path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.ExportBase, s.MetadataHTTP = server.URL, server.URL, server.Client()
}

func TestAutomaticInformationThenMatchingAndRestartCadence(t *testing.T) {
	ctx, s, conn := testService(t)
	var unavailable atomic.Bool
	var requests atomic.Int32
	automaticTMDB(t, s, &unavailable, &requests)
	if err := s.Vault.Put(ctx, "tmdb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','1','{"title":"Example.Movie.2021.1080p"}')`); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.AutoSync, c.AutoMatch, c.TMDBSecret = true, true, "tmdb"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	waitAutomation(t, func() bool {
		status, err := s.DB.AIOStatus(ctx)
		active, _, _ := s.State()
		return err == nil && status.LastSync != nil && status.Matched == 1 && !active
	})
	count := requests.Load()
	for range 5 {
		s.runAutomation()
	}
	if requests.Load() != count {
		t.Fatal("wake redownloaded a completed catalog")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','2','{"title":"Example.Movie.2021.720p"}')`); err != nil {
		t.Fatal(err)
	}
	s.runAutomation()
	waitAutomation(t, func() bool { status, _ := s.DB.AIOStatus(ctx); return status.Matched == 2 })
	if requests.Load() != count {
		t.Fatal("new import unnecessarily downloaded information")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// No custom endpoint is needed: the persisted completion must prevent any
	// TMDB call when the actual constructor restarts the automatic service.
	restarted, err := New(ctx, s.DB, s.Vault, s.sources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = restarted.Close(cleanup)
	})
	restarted.runAutomation()
	status, err := restarted.DB.AIOStatus(ctx)
	c, _ = restarted.DB.AIOConfig(ctx)
	p := AutomationPlan(c, status, false, false, time.Now())
	if err != nil || p.Updates != "scheduled" || p.NextUpdate == nil || !p.NextUpdate.Equal(status.LastSync.Add(15*time.Minute)) {
		t.Fatalf("restart plan %+v %v", p, err)
	}
}

func TestAutomaticFailuresRetryWithoutLosingProgressOrHidingErrors(t *testing.T) {
	ctx, s, conn := testService(t)
	var unavailable atomic.Bool
	unavailable.Store(true)
	var requests atomic.Int32
	automaticTMDB(t, s, &unavailable, &requests)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	if err := s.Vault.Put(ctx, "tmdb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.AutoSync, c.AutoMatch, c.TMDBSecret = true, true, "tmdb"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	waitAutomation(t, func() bool {
		status, _ := s.DB.AIOStatus(ctx)
		active, _, _ := s.State()
		return status.SyncError != "" && !active
	})
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Matched != 1 || status.LastSync != nil || status.SyncFailures != 1 || status.SyncRetryAt == nil {
		t.Fatalf("failure lost data %+v %v", status, err)
	}
	count := requests.Load()
	for range 5 {
		s.runAutomation()
	}
	if requests.Load() != count {
		t.Fatal("retry ignored the durable backoff")
	}
	if err := s.StartMatch(); err != nil {
		t.Fatal(err)
	}
	waitAutomation(t, func() bool { _, active, _ := s.State(); return !active })
	status, _ = s.DB.AIOStatus(ctx)
	if status.SyncError == "" {
		t.Fatal("successful matching hid the TMDB error")
	}
	for attempt := 2; attempt <= 3; attempt++ {
		if attempt == 3 {
			unavailable.Store(false)
		}
		if _, err := conn.Exec(ctx, `UPDATE ingest.aio_state SET sync_retry_at=now()-INTERVAL '1 second'`); err != nil {
			t.Fatal(err)
		}
		s.wakeAutomation()
		waitAutomation(t, func() bool {
			status, _ := s.DB.AIOStatus(ctx)
			active, _, _ := s.State()
			return !active && (status.SyncFailures == attempt || status.LastSync != nil)
		})
	}
	status, err = s.DB.AIOStatus(ctx)
	if err != nil || status.LastSync == nil || status.SyncError != "" || status.SyncFailures != 0 || status.SyncRetryAt != nil || status.Matched != 1 {
		t.Fatalf("recovery %+v %v", status, err)
	}
}

func TestAutomaticAdmissionRechecksClockAndConfigurationUnderLock(t *testing.T) {
	ctx, s, _ := testService(t)
	var unavailable atomic.Bool
	var requests atomic.Int32
	automaticTMDB(t, s, &unavailable, &requests)
	c, _ := s.DB.AIOConfig(ctx)
	c.AutoSync, c.TMDBSecret = true, "stale-reference"
	// A poll's old snapshot is deliberately different from the current manual
	// configuration. No credential resolution or network request may follow it.
	if err := s.syncMetadata(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("automatic admission ignored the saved opt-out")
	}
	status, _ := s.DB.AIOStatus(ctx)
	if status.SyncRetryAt != nil || status.SyncFailures != 0 {
		t.Fatal("skipped admission changed the retry clock")
	}
}
