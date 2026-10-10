package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type metadataRoundTrip func(*http.Request) (*http.Response, error)

func (f metadataRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMetadataLimiterSharesCooldownAndCancelsQueuedRequests(t *testing.T) {
	var limiter metadataRequestLimiter
	limiter.setRate(40)
	limiter.cooldown(120 * time.Millisecond)
	start := time.Now()
	var workers sync.WaitGroup
	admitted := make(chan time.Time, 4)
	for range 4 {
		workers.Go(func() {
			if err := limiter.wait(context.Background()); err != nil {
				t.Error(err)
			}
			admitted <- time.Now()
		})
	}
	workers.Wait()
	close(admitted)
	previous := start
	for at := range admitted {
		if at.Sub(start) < 110*time.Millisecond || (previous != start && at.Sub(previous) < 20*time.Millisecond) {
			t.Fatal("cooldown or request spacing was bypassed")
		}
		previous = at
	}
	limiter.cooldown(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- limiter.wait(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop waited for the server cooldown")
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, c := range []struct {
		value string
		want  time.Duration
	}{{"120", 2 * time.Minute}, {now.Add(2 * time.Minute).Format(http.TimeFormat), 2 * time.Minute}, {"invalid", time.Second}, {"9223372036854775807", time.Second}, {"-1", time.Second}} {
		if got := metadataRetryAfter(c.value, time.Second, now); got != c.want {
			t.Errorf("Retry-After %q: %s, expected %s", c.value, got, c.want)
		}
	}
}

func prepareMetadataPipeline(t *testing.T, s *Service) model.AIOConfig {
	t.Helper()
	ctx := context.Background()
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.AIOInventoryDate(ctx, time.Now().UTC().Add(-24*time.Hour).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.AIOChangesThrough(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.TMDBSecret = "tmdb"
	return c
}

func TestMetadataPipelineOverlapsRequestsAndPreservesQueueOnStop(t *testing.T) {
	ctx, s, conn := testService(t)
	c := prepareMetadataPipeline(t, s)
	ids := []string{}
	for i := 1; i <= 48; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", ids, true); err != nil {
		t.Fatal(err)
	}
	var requests, active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/changes") {
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movie/"))
		requests.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		fmt.Fprint(w, strings.Replace(movieDetail, `"id":101`, fmt.Sprintf(`"id":%d`, id), 1))
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	work, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.Sync(work, c) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var committed int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_works`).Scan(&committed); err != nil {
			t.Fatal(err)
		}
		if committed > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pipeline did not publish useful progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("stop was ignored", err)
	}
	var committed, pending int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_metadata_jobs)`).Scan(&committed, &pending); err != nil || committed == 0 || pending == 0 || committed+pending != len(ids) {
		t.Fatalf("interruption lost work: committed=%d pending=%d %v", committed, pending, err)
	}
	if peak.Load() < 2 || peak.Load() > metadataWorkers {
		t.Fatalf("invalid HTTP concurrency: %d", peak.Load())
	}
	if err := s.Sync(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_metadata_jobs`).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("stopped work did not resume", pending, err)
	}
}

func TestMetadataPipelineStopsOutageWithoutDiscardingRemainingJobs(t *testing.T) {
	ctx, s, conn := testService(t)
	c := prepareMetadataPipeline(t, s)
	ids := []string{}
	for i := 1; i <= 100; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", ids, true); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/changes") {
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		requests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	// Simulate a transport outage without waiting through HTTP retry delays.
	s.MetadataHTTP.Transport = metadataRoundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/changes") {
			requests.Add(1)
			return nil, errors.New("synthetic transport outage")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if err := s.Sync(ctx, c); err == nil {
		t.Fatal("outage was reported as success")
	}
	var pending, deferred int
	if err := conn.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE attempts=1 AND next_attempt>now()) FROM ingest.aio_metadata_jobs`).Scan(&pending, &deferred); err != nil || pending != 100 || deferred != 3 || requests.Load() >= 100 {
		t.Fatalf("outage drained the catalog or lost its retries: pending=%d deferred=%d requests=%d %v", pending, deferred, requests.Load(), err)
	}
}

func TestTMDBSeasonsAreAppendedAndUnknownSourceSeasonIsIgnored(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.aio_season_requests(tmdb_id,season) VALUES('202',1),('202',2),('202',99)`); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("append_to_response") == "season/1,season/2" {
			fmt.Fprint(w, `{"id":202,"season/1":{"season_number":1,"air_date":"2019-01-01","episodes":[{"season_number":1,"episode_number":2,"air_date":"2020-01-01"}]},"season/2":{"season_number":2,"air_date":"2021-01-01","episodes":[]}}`)
			return
		}
		fmt.Fprint(w, strings.Replace(tvDetail, `"seasons":[`, `"seasons":[{"season_number":2,"air_date":""},`, 1))
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	if err := s.DB.QueueAIOMetadata(ctx, "tv", []string{"202"}, true); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.DB.AIOMetadataJobs(ctx)
	if ok, err := s.details(ctx, "synthetic", jobs[0]); !ok || err != nil {
		t.Fatal(ok, err)
	}
	works, _, err := s.DB.AIOCandidates(ctx, "example serial", "202", "", "")
	if err != nil || len(works) != 1 || works[0].SeasonYears[2] != 2021 || works[0].EpisodeYears["1:2"] != 2020 || calls.Load() != 2 {
		t.Fatal("season data or batching was lost", works, calls.Load(), err)
	}
}

func TestTVDecodeRejectsIncompleteSeasonResponseBeforePublication(t *testing.T) {
	ctx, s, _ := testService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/season/") {
			fmt.Fprint(w, `{"season_number":1}`)
			return
		}
		fmt.Fprint(w, tvDetail)
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	work, err := decodeWork("tv", "202", []byte(tvDetail))
	if err != nil {
		t.Fatal(err)
	}
	work.EpisodeYears["1:2"] = 2020
	if err := s.DB.SaveAIOWork(ctx, work, []string{Normalize(work.Titles[0])}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.RequestAIOSeason(ctx, "202", 1); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.DB.AIOMetadataJobs(ctx)
	if _, err := s.loadDetails(ctx, "synthetic", jobs[0]); !errors.Is(err, ErrInvalidResponse) {
		t.Fatal("incomplete season was accepted", err)
	}
	works, _, err := s.DB.AIOCandidates(ctx, "example serial", "202", "", "")
	if err != nil || len(works) != 1 || works[0].EpisodeYears["1:2"] != 2020 {
		t.Fatal("incomplete season erased known episode years", works, err)
	}
}
