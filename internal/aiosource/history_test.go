package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestOperationHistoryMatchingResultsAndDebridRetry(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "one", "1", strings.Repeat("a", 40))
	matches, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "matching"})
	if err != nil || matches.Total != 1 || matches.Items[0].Status != "succeeded" || matches.Items[0].Processed != 1 || matches.Items[0].Results["matched"] != 1 {
		t.Fatalf("identification history %+v %v", matches, err)
	}
	var failing atomic.Bool
	failing.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "synthetic-key-must-not-be-recorded")
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	defer server.Close()
	s.Providers.TBBase = server.URL
	c, _ := s.DB.AIOConfig(ctx)
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	if err = s.checkResource(ctx, "torbox", "synthetic-key-must-not-be-recorded", "movie:tt1234567", hashes, c); !errors.Is(err, ErrAuth) {
		t.Fatalf("expected provider failure %v", err)
	}
	failed, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
	if err != nil || failed.Total != 1 || failed.Items[0].Status != "failed" || failed.Items[0].Errors != 2 {
		t.Fatalf("failed cache history %+v %v", failed, err)
	}
	encoded, _ := json.Marshal(failed)
	if strings.Contains(string(encoded), "synthetic-key-must-not-be-recorded") {
		t.Fatal("credential exposed in history")
	}
	failing.Store(false)
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_resources SET checked_at=now()-interval '60 days'`); err != nil {
		t.Fatal(err)
	}
	if err = s.checkResource(ctx, "torbox", "synthetic-key-must-not-be-recorded", "movie:tt1234567", hashes, c); err != nil {
		t.Fatal(err)
	}
	if err = s.checkResource(ctx, "torbox", "synthetic-key-must-not-be-recorded", "movie:tt1234567", hashes, c); err != nil {
		t.Fatal(err)
	}
	checks, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cache"})
	if err != nil || checks.Total != 2 || checks.Items[0].Status != "succeeded" || checks.Items[0].Processed != 2 || checks.Items[0].Results["uncached"] != 2 || checks.Items[0].Errors != 0 {
		t.Fatalf("retry or skipped check history %+v %v", checks, err)
	}
}

func TestOperationHistoryMetadataFailureAndCancellationAreSafe(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-private-token"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "synthetic-private-token")
	}))
	defer server.Close()
	s.TMDBBase = server.URL
	s.MetadataHTTP = server.Client()
	if err := s.Sync(ctx, c); !errors.Is(err, ErrAuth) {
		t.Fatalf("expected TMDB authentication failure %v", err)
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata"})
	if err != nil || runs.Total != 1 || runs.Items[0].Status != "failed" || runs.Items[0].Phase != "changes" || runs.Items[0].FinishedAt == nil {
		t.Fatalf("information history %+v %v", runs, err)
	}
	encoded, _ := json.Marshal(runs)
	if strings.Contains(string(encoded), "synthetic-private-token") {
		t.Fatal("token exposed")
	}
	release, locked, err := s.DB.AIOLock(ctx, "history-cancellation")
	if err != nil || !locked {
		t.Fatal(err)
	}
	defer release()
	history, err := s.beginOperation(ctx, "matching", "local", "manual", "", "history-cancellation")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	history.finish(cancelled, context.Canceled)
	stopped, err := s.DB.Operation(ctx, history.run.ID)
	if err != nil || stopped.Status != "cancelled" || stopped.Errors != 0 {
		t.Fatalf("stop history %+v %v", stopped, err)
	}
}

func TestOperationHistoryAllDebridCleanupRetainsPreviousFailures(t *testing.T) {
	ctx, s, _ := testService(t)
	key := "synthetic-cleanup-key"
	job := model.AIOCleanup{Account: fingerprint(key), Hash: strings.Repeat("a", 40), ID: 77}
	if err := s.DB.AddAIOCleanup(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.FinishAIOCleanup(ctx, job, false); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
	}))
	defer server.Close()
	s.Providers.ADBase = server.URL
	if err := s.RecoverCleanup(ctx, key, false); err != nil {
		t.Fatal(err)
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "cleanup"})
	if err != nil || runs.Total != 1 || runs.Items[0].Status != "succeeded" || runs.Items[0].Results["cleaned"] != 1 {
		t.Fatalf("cleanup history %+v %v", runs, err)
	}
	remaining, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(remaining) != 0 {
		t.Fatalf("manually removed failed magnet not recovered: %+v %v", remaining, err)
	}
}
