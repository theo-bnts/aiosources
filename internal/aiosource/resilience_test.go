package aiosource

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMetadataRepeatedFailuresAndRestartRetainPublishedMapping(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	token, err := s.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch phase.Load() {
		case 1:
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		case 3:
			fmt.Fprint(w, movieDetail)
		default:
			fmt.Fprint(w, `{"id":101,"title":"Incomplete response"}`)
		}
	}))
	t.Cleanup(server.Close)
	for attempt := 0; attempt < 4; attempt++ {
		phase.Store(int32(attempt))
		s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
		jobs, err := s.DB.AIOMetadataJobs(ctx)
		if err != nil || len(jobs) != 1 || jobs[0].Attempts != attempt {
			t.Fatalf("restart lost the durable metadata retry: %+v %v", jobs, err)
		}
		ok, err := s.details(ctx, "synthetic-key", jobs[0])
		if attempt < 3 && (ok || err == nil) {
			t.Fatalf("temporary invalid or interrupted response was published: %v", err)
		}
		if attempt == 3 && (!ok || err != nil) {
			t.Fatalf("healthy response did not recover: %v", err)
		}
		if err := s.DB.FinishAIOMetadata(ctx, jobs[0], ok && err == nil); err != nil {
			t.Fatal(err)
		}
		if attempt == 3 {
			break
		}
		out, err := s.Streams(ctx, "http://local", token, "movie", "tt1234567")
		if err != nil || len(out) != 1 {
			t.Fatalf("repeated failures erased the published IMDb correspondence: %+v %v", out, err)
		}
		var deferred bool
		if err := conn.QueryRow(ctx, `SELECT next_attempt>now() FROM ingest.aio_metadata_jobs`).Scan(&deferred); err != nil || !deferred {
			t.Fatalf("failed metadata lost its retry deadline: %t %v", deferred, err)
		}
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}
		s, err = New(ctx, s.DB, s.Vault, s.sources)
		if err != nil {
			t.Fatal(err)
		}
		current := s
		t.Cleanup(func() { _ = current.Close(context.Background()) })
		if got, err := s.Token(ctx); err != nil || got != token {
			t.Fatal("restart changed the installation token", err)
		}
		// Advance the durable deadline instead of waiting for minutes in a test.
		if _, err := conn.Exec(ctx, `UPDATE ingest.aio_metadata_jobs SET next_attempt=now()`); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := s.DB.AIOMetadataJobs(ctx)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("recovered metadata did not finish its queue entry: %+v %v", jobs, err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := s.Streams(ctx, "http://local", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 {
		t.Fatalf("mapping was not republished after recovery: %+v %v", out, err)
	}
}

func TestCacheFailuresAndInterruptedCheckKeepPositiveObservationsAcrossRestart(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			key := "synthetic-account"
			known, pending := strings.Repeat("a", 40), strings.Repeat("b", 40)
			if err := s.Vault.Put(ctx, "debrid-key", key); err != nil {
				t.Fatal(err)
			}
			c, err := s.DB.AIOConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid-key"
			} else {
				c.TorBoxSecret = "debrid-key"
			}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			generation, err := s.DB.AIOCacheGeneration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			positive := model.AIOAvailability{Hash: known, Cached: true, CheckedAt: time.Now().UTC(), Generation: generation,
				Files: []model.AIOFile{{ID: 0, Path: "Example.mkv", Size: 123}}}
			if err := s.DB.SaveAIOAvailability(ctx, service, fingerprint(key), positive); err != nil {
				t.Fatal(err)
			}
			before, err := s.DB.AIOAvailability(ctx, service, fingerprint(key), []string{known, pending}, time.Now().Add(-time.Hour))
			if err != nil || len(before) != 1 {
				t.Fatal("positive fixture missing", err)
			}
			var phase atomic.Int32
			entered := make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch phase.Load() {
				case 0:
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case 1:
					once.Do(func() { close(entered) })
					<-r.Context().Done()
					return
				}
				switch r.URL.Path {
				case "/torrents/checkcached":
					fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","name":"Recovered","size":123,"files":[]}}}`, pending, pending)
				case "/v4.1/magnet/status":
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":10,"hash":"%s","statusCode":4}]}}`, pending)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":10,"files":[{"n":"Recovered.mkv","s":123,"l":"https://example.com/file"}]}]}}`)
				default:
					t.Errorf("cache recovery attempted an unexpected remote mutation: %s", r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			configure := func() {
				s.Providers.ADBase, s.Providers.TBBase, s.Providers.Client = server.URL, server.URL, server.Client()
			}
			restart := func() {
				t.Helper()
				if err := s.Close(ctx); err != nil {
					t.Fatal(err)
				}
				s, err = New(ctx, s.DB, s.Vault, s.sources)
				if err != nil {
					t.Fatal(err)
				}
				current := s
				t.Cleanup(func() { _ = current.Close(context.Background()) })
				configure()
			}
			due := func() {
				t.Helper()
				if _, err := conn.Exec(ctx, `UPDATE ingest.aio_resources SET last_attempt=now()-interval '49 hours'`); err != nil {
					t.Fatal(err)
				}
			}
			unchanged := func() {
				t.Helper()
				got, err := s.DB.AIOAvailability(ctx, service, fingerprint(key), []string{known, pending}, time.Now().Add(-time.Hour))
				if err != nil || !reflect.DeepEqual(got, before) {
					t.Fatalf("outage erased a positive or manufactured a negative: %+v %v", got, err)
				}
			}
			configure()
			for attempt := 1; attempt <= 3; attempt++ {
				due()
				if err := s.checkResource(ctx, service, key, "movie:example", []string{known, pending}, c); err == nil {
					t.Fatal("provider outage was silently accepted")
				}
				resource, found, err := s.DB.AIOResource(ctx, service, fingerprint(key), "movie:example")
				if err != nil || !found || resource.Retry != attempt {
					t.Fatalf("repeated error lost retry state: %+v %v", resource, err)
				}
				unchanged()
				restart()
			}
			phase.Store(1)
			due()
			flight, err := s.cacheFlight(service, key, "movie:example", []string{known, pending}, c)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("cache check did not reach its in-flight boundary")
			}
			restart()
			select {
			case <-flight.done:
			default:
				t.Fatal("shutdown left an owned cache task running")
			}
			unchanged()
			phase.Store(2)
			due()
			if err := s.checkResource(ctx, service, key, "movie:example", []string{known, pending}, c); err != nil {
				t.Fatal("healthy provider failed to resume the resource", err)
			}
			recovered, err := s.DB.AIOAvailability(ctx, service, fingerprint(key), []string{known, pending}, time.Now().Add(-time.Hour))
			if err != nil || len(recovered) != 2 {
				t.Fatalf("restart permanently lost uncompleted cache work: %+v %v", recovered, err)
			}
			for _, item := range recovered {
				if !item.Cached || (item.Hash == known && !reflect.DeepEqual(item, before[0])) {
					t.Fatalf("recovery changed an unrelated positive: %+v", recovered)
				}
			}
		})
	}
}
