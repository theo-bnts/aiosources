package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
)

func TestMetadataProviderDeadlinesPersistAfterCallerCancellation(t *testing.T) {
	ctx, s, _ := testService(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	var remote *metadataError
	if err := s.metadataResponseFailure(cancelled, http.StatusTooManyRequests, "120", time.Minute); !errors.As(err, &remote) || remote.status != http.StatusTooManyRequests {
		t.Fatal("cancelled caller lost the TMDB response", err)
	}
	var book *bookRequestFailure
	if err := s.bookProviderFailure(cancelled, "120"); !errors.As(err, &book) {
		t.Fatal("cancelled caller lost the Open Library response", err)
	}
	videoState, err := s.DB.AIOAutomationStatus(ctx)
	bookState, bookErr := s.DB.BookChangesState(ctx)
	if err != nil || bookErr != nil || videoState.ProviderRetryAt == nil || bookState.ProviderRetryAt == nil || time.Until(*videoState.ProviderRetryAt) < time.Minute || time.Until(*bookState.ProviderRetryAt) < time.Minute {
		t.Fatal("caller cancellation lost a provider-requested wait", videoState, bookState, err, bookErr)
	}
}

func TestBookFailuresUseTheSharedRetrySettings(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	policy, err := s.DB.RuntimeSection(ctx, "metadata_retries")
	if err != nil {
		t.Fatal(err)
	}
	policy.Values["sync_retry_seconds"], policy.Values["sync_retry_max_seconds"] = 120, 300
	policy.Values["detail_retry_seconds"], policy.Values["detail_retry_max_seconds"] = 120, 300
	if err = s.DB.UpdateRuntimeSection(ctx, "metadata_retries", policy); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.feedStatus = http.StatusBadRequest
	f.mu.Unlock()
	for attempt := 1; attempt <= 2; attempt++ {
		if err := s.SyncBooks(ctx, "manual"); err == nil {
			t.Fatal("the invalid feed was accepted")
		}
		var retry time.Time
		var failures int
		if err := conn.QueryRow(ctx, `SELECT retry_at,sync_failures FROM ingest.ol_import_state`).Scan(&retry, &failures); err != nil || failures != attempt || time.Until(retry) < time.Duration(attempt*120-10)*time.Second || time.Until(retry) > time.Duration(attempt*120+1)*time.Second {
			t.Fatal("book retry ignored the common policy", retry, failures, err)
		}
	}
	f.mu.Lock()
	f.feedStatus = 0
	f.mu.Unlock()
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal("manual update could not recover before its automatic retry", err)
	}
	var failures int
	if err := conn.QueryRow(ctx, `SELECT sync_failures FROM ingest.ol_import_state`).Scan(&failures); err != nil || failures != 0 {
		t.Fatal("successful recovery retained its failure count", failures, err)
	}
	f.mu.Lock()
	f.documentStatus = http.StatusBadRequest
	f.mu.Unlock()
	if err := s.DB.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL400W", Revision: 1, At: f.time}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var next time.Time
	if err := conn.QueryRow(ctx, `SELECT next_attempt FROM ingest.ol_change_jobs WHERE id='OL400W'`).Scan(&next); err != nil || time.Until(next) < 110*time.Second || time.Until(next) > 121*time.Second {
		t.Fatal("individual book retries ignored the shared policy", next, err)
	}
}

func TestBookShortCooldownIsAbsoluteAndManualRetriesDoNotExtendIt(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx, s, conn := testService(t)
			f := newBookAPIFixture(t, s)
			f.mu.Lock()
			f.feedStatus, f.retryAfter = status, "120"
			f.mu.Unlock()
			if err := s.SyncBooks(ctx, "manual"); err == nil {
				t.Fatal("provider cooldown was ignored")
			}
			before, err := s.DB.BookChangesState(ctx)
			if err != nil || before.ProviderRetryAt == nil || time.Until(*before.ProviderRetryAt) > 3*time.Minute || time.Until(*before.ProviderRetryAt) < time.Minute {
				t.Fatal("a short provider wait was inflated", before, err)
			}
			for range 3 {
				if err := s.SyncBooks(ctx, "manual"); err == nil {
					t.Fatal("manual retry bypassed the provider deadline")
				}
			}
			after, err := s.DB.BookChangesState(ctx)
			next, nextErr := s.DB.NextOpenLibraryUpdate(ctx, 15)
			if err != nil || nextErr != nil || after.ProviderRetryAt == nil || !after.ProviderRetryAt.Equal(*before.ProviderRetryAt) || !next.Equal(*before.ProviderRetryAt) {
				t.Fatal("manual retries postponed the saved deadline", before, after, next, err, nextErr)
			}
			var attempts int
			if err := conn.QueryRow(ctx, `SELECT sync_failures FROM ingest.ol_import_state`).Scan(&attempts); err != nil || attempts != 1 {
				t.Fatal("blocked clicks were counted as provider attempts", attempts, err)
			}
			f.mu.Lock()
			calls, dumps := f.requests["/recentchanges.json"], f.dumpCalls
			f.mu.Unlock()
			if calls != 1 || dumps != 0 {
				t.Fatal("blocked clicks made new provider requests", calls, dumps)
			}
		})
	}
}

func TestBookFeedFailureDoesNotBlockAlreadyQueuedDocuments(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.BookChangesState(ctx)
	f.mu.Lock()
	f.feedStatus = http.StatusBadRequest
	f.documents["OL400W"] = bookAPIRecord("OL400W", 1, map[string]any{"title": "The Azure Almanac"})
	f.mu.Unlock()
	if err := s.DB.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL400W", Revision: 1, At: f.time.Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("the invalid change feed was reported as successful")
	}
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title FROM ingest.aio_works WHERE openlibrary_id='OL400W'`).Scan(&title); err != nil || title != "The Azure Almanac" {
		t.Fatal("a feed-only error prevented useful document updates", title, err)
	}
	after, err := s.DB.BookChangesState(ctx)
	if err != nil || after.ProviderRetryAt != nil || after.ChangesThrough == nil || before.ChangesThrough == nil || !after.ChangesThrough.Equal(*before.ChangesThrough) {
		t.Fatal("a feed error became a global quota or advanced freshness", before, after, err)
	}
}

func TestBookManualUpdateChecksForChangesWithoutWaitingForTheInterval(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.documents["OL400W"] = bookAPIRecord("OL400W", 1, map[string]any{"title": "The Azure Almanac"})
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL400W", "revision": 1}}}}, f.rows...)
	f.mu.Unlock()
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title FROM ingest.aio_works WHERE openlibrary_id='OL400W'`).Scan(&title); err != nil || title != "The Azure Almanac" {
		t.Fatal("manual update silently waited for the automatic interval", title, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dumpCalls != 1 || f.requests["/recentchanges.json"] != 2 {
		t.Fatal("manual update repeated the dump or skipped its change check", f.dumpCalls, f.requests)
	}
}

func TestBookAutomaticAdmissionRechecksSavedChoiceAndSchedule(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "automatic"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_import_state SET complete=TRUE; UPDATE ingest.ol_changes_state SET bootstrap_complete=TRUE,checked_at=now()`); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.OpenLibraryEnabled = true
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "automatic"); err != nil {
		t.Fatal(err)
	}
	var operations int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.operation_runs WHERE service='openlibrary'`).Scan(&operations); err != nil || operations != 0 {
		t.Fatal("inadmissible automatic work created a history attempt", operations, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 0 {
		t.Fatal("automatic work ignored the saved opt-out or future schedule", f.requests)
	}
}
