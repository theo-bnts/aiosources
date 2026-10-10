package aiosource

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestInventoryImportDoesNotConsumeHTTPRequestTimeout(t *testing.T) {
	ctx, s, conn := testService(t)
	// Enough compressed data to require another body read after the first
	// database batch. A per-request timeout formerly expired during that batch.
	var archive bytes.Buffer
	g := gzip.NewWriter(&archive)
	rng := rand.New(rand.NewPCG(1, 2))
	const records = 4001
	for id := 1; id <= records; id++ {
		fmt.Fprintf(g, "{\"id\":%d,\"original_title\":\"%016x%016x%016x%016x\"}\n", id, rng.Uint64(), rng.Uint64(), rng.Uint64(), rng.Uint64())
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `CREATE FUNCTION ingest.slow_inventory_batch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.2); RETURN NULL; END $$;
CREATE TRIGGER slow_inventory_batch AFTER INSERT ON ingest.aio_inventory FOR EACH STATEMENT EXECUTE FUNCTION ingest.slow_inventory_batch()`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive.Bytes())
	}))
	t.Cleanup(server.Close)
	s.ExportBase = server.URL
	s.MetadataHTTP = server.Client()
	s.MetadataHTTP.Timeout = 100 * time.Millisecond
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	if err := s.inventory(ctx, "movie", "2026-10-01"); err != nil {
		t.Fatalf("database processing consumed the download timeout: %v", err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_inventory`).Scan(&count); err != nil || count != records {
		t.Fatalf("incomplete imported inventory: %d %v", count, err)
	}
	files, err := os.ReadDir(temp)
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary inventory file remained after import: %v %v", files, err)
	}
}

func TestInformationFailuresAreSpecificLoggedRepeatedlyAndRecover(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	const token = "synthetic-token-that-must-not-be-logged"
	if err := s.Vault.Put(ctx, "tmdb", token); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/changes") {
			if phase.Load() < 2 {
				status := http.StatusBadGateway
				if phase.Load() == 1 {
					status = http.StatusUnauthorized
				} else {
					// A non-retryable response keeps this regression fast.
					status = http.StatusTeapot
				}
				w.WriteHeader(status)
				fmt.Fprint(w, token)
				return
			}
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".json.gz") {
			if phase.Load() == 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			g := gzip.NewWriter(w)
			if strings.Contains(r.URL.Path, "movie_ids") {
				fmt.Fprintln(g, `{"id":101}`)
			} else {
				fmt.Fprintln(g, `{"id":202}`)
			}
			_ = g.Close()
			return
		}
		if r.URL.Path == "/movie/101" {
			fmt.Fprint(w, movieDetail)
		} else {
			fmt.Fprint(w, tvDetail)
		}
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.ExportBase, s.MetadataHTTP = server.URL, server.URL, server.Client()
	for _, test := range []struct {
		phase           int32
		code, operation string
		status          int
	}{
		{0, "http", "changes", 418},
		{0, "http", "changes", 418},
		{1, "authentication", "changes", 401},
		{2, "http", "inventory", 503},
	} {
		phase.Store(test.phase)
		if err := s.Sync(ctx, c); err == nil {
			t.Fatal("failed update reported success")
		}
		status, err := s.DB.AIOStatus(ctx)
		if err != nil || status.Works != 1 || status.Matched != 1 || status.SyncRetryAt == nil || status.SyncError == "" {
			t.Fatalf("update lost committed data or retry: %+v %v", status, err)
		}
		if strings.Contains(status.SyncError, "token") != (test.code == "authentication") {
			t.Fatalf("failure incorrectly blamed the token: %s", status.SyncError)
		}
		logs, err := s.DB.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 200}, Service: "tmdb"})
		if err != nil || logs.Total == 0 {
			t.Fatalf("missing TMDB diagnostics: %+v %v", logs, err)
		}
		latest := logs.Items[0]
		if latest.Kind != "tmdb.sync_failed" || latest.Data["operation"] != test.operation || latest.Data["failure_code"] != test.code || fmt.Sprint(latest.Data["status_code"]) != fmt.Sprint(test.status) {
			t.Fatalf("imprecise diagnostic: %+v", latest)
		}
		encoded, _ := json.Marshal(logs)
		if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), server.URL) {
			t.Fatal("TMDB logs exposed a credential or URL")
		}
	}
	var failures, alerts int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='tmdb.sync_failed'`).Scan(&failures); err != nil || failures != 4 {
		t.Fatalf("repeated failure disappeared from logs: %d %v", failures, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE service='tmdb' AND kind='aiosource.incident'`).Scan(&alerts); err != nil || alerts != 1 {
		t.Fatalf("repeated failure created duplicate incident logs: %d %v", alerts, err)
	}
	phase.Store(3)
	if err := s.Sync(ctx, c); err != nil {
		t.Fatal(err)
	}
	status, _ := s.DB.AIOStatus(ctx)
	if status.SyncError != "" || status.SyncRetryAt != nil || status.LastSync == nil || status.Matched != 1 {
		t.Fatalf("healthy update did not recover: %+v", status)
	}
}

func TestInformationTimeoutAndStorageDiagnosticsDoNotBlameToken(t *testing.T) {
	ctx, s, conn := testService(t)
	if err := s.Vault.Put(ctx, "tmdb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			fmt.Fprint(w, `{}`)
		}
	}))
	t.Cleanup(server.Close)
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	s.MetadataHTTP.Timeout = 20 * time.Millisecond
	if err := s.Sync(ctx, c); err == nil || metadataDiagnostic("changes", err).FailureCode != "timeout" {
		t.Fatalf("request timeout was not classified: %v", err)
	}
	status, _ := s.DB.AIOStatus(ctx)
	if !strings.Contains(status.SyncError, "timed out") || strings.Contains(status.SyncError, "token") {
		t.Fatalf("incorrect timeout message: %s", status.SyncError)
	}
	if _, err := conn.Exec(ctx, `DROP TABLE ingest.aio_metadata_jobs`); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx, c); err == nil {
		t.Fatal("missing database table was accepted")
	}
	if err := conn.QueryRow(ctx, `SELECT sync_error FROM ingest.aio_state`).Scan(&status.SyncError); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.SyncError, "local database") || strings.Contains(status.SyncError, "token") {
		t.Fatalf("database failure incorrectly blamed TMDB: %s", status.SyncError)
	}
	var incidents int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE service='tmdb' AND kind='aiosource.incident'`).Scan(&incidents); err != nil || incidents != 0 {
		t.Fatalf("local database failure incorrectly opened a TMDB incident: %d %v", incidents, err)
	}
}

func TestInventoryFailuresRemoveTemporaryFilesAndRetainCheckpoint(t *testing.T) {
	ctx, s, _ := testService(t)
	var archive bytes.Buffer
	g := gzip.NewWriter(&archive)
	fmt.Fprintln(g, `{"id":101}`)
	_ = g.Close()
	// Corrupt the checksum after a valid record; never publish the inventory
	// checkpoint or reconcile missing works from an incomplete archive.
	broken := bytes.Clone(archive.Bytes())
	broken[len(broken)-8] ^= 0xff
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(broken) }))
	t.Cleanup(server.Close)
	s.ExportBase, s.MetadataHTTP = server.URL, server.Client()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	if err := s.inventory(ctx, "movie", "2026-10-01"); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("corrupt inventory was accepted: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(temp, "aiosources-tmdb-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("temporary failed download remained: %v %v", files, err)
	}
	status, _ := s.DB.AIOStatus(context.WithoutCancel(ctx))
	if status.InventoryDate != "" {
		t.Fatal("corrupt inventory advanced the checkpoint")
	}
}

func TestInterruptedInventoryDownloadPreservesDataAndRemovesTemporaryFile(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "partial compressed archive")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	s.ExportBase, s.MetadataHTTP = server.URL, server.Client()
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	download, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.inventory(download, "movie", "2026-10-01") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("download did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("interrupted download lost its cancellation cause", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted download did not stop")
	}
	files, err := os.ReadDir(temp)
	if err != nil || len(files) != 0 {
		t.Fatalf("interrupted inventory left temporary data: %v %v", files, err)
	}
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Works != 1 || status.Matched != 1 || status.InventoryDate != "" {
		t.Fatalf("interrupted inventory lost published data or advanced its checkpoint: %+v %v", status, err)
	}
}
