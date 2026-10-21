package aiosource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInterruptedAutomaticUpdateRetainsClockAndResumesAfterRestart(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "source", "1", strings.Repeat("a", 40))
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "tmdb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	blocked := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(blocked.Close)
	s.TMDBBase, s.MetadataHTTP = blocked.URL, blocked.Client()
	c, _ := s.DB.AIOConfig(ctx)
	c.AutoSync, c.AutoMatch, c.TMDBSecret = true, true, "tmdb"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic update did not start")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := s.DB.AIOStatus(ctx)
	if err != nil || status.Pending != 1 || status.Matched != 1 || status.LastSync != nil || status.SyncRetryAt == nil || !strings.Contains(status.SyncError, "interrupted") {
		t.Fatalf("shutdown lost committed progress or retry: %+v %v", status, err)
	}
	retry := *status.SyncRetryAt
	restarted, err := New(ctx, s.DB, s.Vault, s.sources)
	if err != nil {
		t.Fatal(err)
	}
	var unavailable atomic.Bool
	var requests atomic.Int32
	automaticTMDB(t, restarted, &unavailable, &requests)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = restarted.Close(cleanup)
	})
	status, err = restarted.DB.AIOStatus(ctx)
	if err != nil || status.SyncRetryAt == nil || !status.SyncRetryAt.Equal(retry) {
		t.Fatalf("restart reset retry clock: %+v %v", status, err)
	}
	restarted.runAutomation()
	if requests.Load() != 0 {
		t.Fatal("restart bypassed the retry delay")
	}
	if _, err := conn.Exec(ctx, `UPDATE ingest.aio_state SET sync_retry_at=now()-INTERVAL '1 second',last_match=now()-INTERVAL '31 seconds'`); err != nil {
		t.Fatal(err)
	}
	restarted.wakeAutomation()
	waitAutomation(t, func() bool {
		status, _ := restarted.DB.AIOStatus(ctx)
		active, _, _ := restarted.State()
		return status.LastSync != nil && status.Matched == 1 && !active
	})
	status, err = restarted.DB.AIOStatus(ctx)
	if err != nil || status.Pending != 0 || status.Matched != 1 || status.SyncRetryAt != nil || status.SyncError != "" {
		t.Fatalf("restart did not recover queued work: %+v %v", status, err)
	}
}
