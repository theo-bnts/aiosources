package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestRateLimitedCollectionsSaveDiagnosticsBeforeRetry(t *testing.T) {
	for _, kind := range []string{"json", "bounded", "torznab-caps", "torznab-search"} {
		t.Run(kind, func(t *testing.T) {
			ctx, endpoint, db := schedulerDatabase(t)
			control, err := pgx.Connect(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = control.Close(context.Background()) })
			const rejected = `{"error":"quota","original":"unchanged"}`
			var limited atomic.Bool
			var before atomic.Int64
			var tooEarly atomic.Bool
			var recordedBeforeRetry atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				caps := r.URL.Query().Get("t") == "caps"
				eligible := kind != "torznab-caps" && !caps || kind == "torznab-caps" && caps
				if eligible && limited.CompareAndSwap(false, true) {
					deadline := time.Now().Add(time.Second)
					switch kind {
					case "bounded":
						w.Header().Set("RateLimit-Reset", "1")
					case "torznab-caps":
						deadline = deadline.Truncate(time.Second)
						w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(deadline.Unix(), 10))
					default:
						w.Header().Set("Retry-After", "1")
					}
					before.Store(deadline.UnixNano())
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(rejected))
					return
				}
				if eligible {
					if time.Now().UnixNano() < before.Load() {
						tooEarly.Store(true)
					}
					var count int
					if err := control.QueryRow(ctx, `SELECT count(*) FROM ingest.pages WHERE metadata->>'http_status'='429' AND octet_length(body)=0 AND NOT payload_retained`).Scan(&count); err == nil && count == 1 {
						recordedBeforeRetry.Store(true)
					}
				}
				if caps {
					_, _ = w.Write([]byte(`<caps><limits max="100" default="1"/><searching><search available="yes" supportedParams="q"/></searching></caps>`))
				} else if kind == "torznab-caps" || kind == "torznab-search" {
					_, _ = w.Write([]byte(`<rss version="2.0" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><newznab:response offset="0" total="1"/><item><guid>native-1</guid><title>Recovered release</title><newznab:attr name="size" value="1"/></item></channel></rss>`))
				} else {
					_, _ = w.Write([]byte(`{"items":[{"id":"native-1","title":"Recovered release","category_id":1}],"total":1}`))
				}
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("limited", server.URL)
			p.Schedule = model.Schedule{}
			if kind == "bounded" {
				p.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1}
				p.Mapping.Fields["category_id"] = "/category_id"
				p.Traversal = &model.JSONTraversal{WindowPages: 100, TotalMode: "at_least", TotalPaths: []string{"/total"}, Scopes: []model.JSONScope{{ID: "selected", Match: map[string]any{"category_id": 1}}}}
			} else if kind == "torznab-caps" || kind == "torznab-search" {
				p.Adapter = "torznab"
				p.HTTP.ItemsPath = ""
				p.Mapping = model.Mapping{}
				p.Pagination = model.Pagination{}
				p.Options = map[string]any{"id_source": "guid"}
			}
			saveScheduledProvider(t, registry, p, "")
			manager := New(db, registry, nil, 1)
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
			if err != nil {
				t.Fatal(err)
			}
			finished := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
			if tooEarly.Load() || !recordedBeforeRetry.Load() || finished.Pages != 1 || finished.DistinctRecords != 1 || finished.Errors != 1 {
				t.Fatalf("quota retry lost its diagnostics, deadline or checkpoint: early=%t recorded=%t run=%+v", tooEarly.Load(), recordedBeforeRetry.Load(), finished)
			}
			var published int
			if err := control.QueryRow(ctx, "SELECT count(*) FROM ingest.torrents WHERE provider_id=$1 AND source_id='native-1'", p.ID).Scan(&published); err != nil || published != 1 {
				t.Fatalf("recovered native release was not published: count=%d error=%v", published, err)
			}
		})
	}
}

func TestRateLimitPausePreservesCooldownAcrossResume(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var requests atomic.Int32
	var deadline atomic.Int64
	var early atomic.Bool
	received := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			deadline.Store(time.Now().Add(2 * time.Second).UnixNano())
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("original quota response"))
			close(received)
			return
		}
		if time.Now().UnixNano() < deadline.Load() {
			early.Store(true)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"recovered","title":"Recovered"}],"total":1}`))
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("paused-quota", server.URL)
	p.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	finished := make(chan error, 1)
	go func() {
		status, message, reason := manager.collect(ctx, *claimed)
		_, err := db.FinishRun(ctx, run.ID, status, message, reason)
		if err == nil && status != model.StatusPaused {
			err = fmt.Errorf("quota pause finished as %s: %s", status, message)
		}
		finished <- err
	}()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("quota response was not requested")
	}
	if _, err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Pause remained blocked behind quota cooldown")
	}
	held, err := db.GetRun(ctx, run.ID)
	if err != nil || held.PauseReason != model.PauseManual || held.Pages != 0 || !bytes.Equal(held.Cursor, claimed.Cursor) || requests.Load() != 1 {
		t.Fatalf("quota pause advanced the checkpoint: %+v requests=%d error=%v", held, requests.Load(), err)
	}
	// A fresh connector has no in-memory quota state; the archived deadline
	// must prevent Resume from sending the same request early.
	restarted := New(db, registry, nil, 1)
	if _, err := restarted.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed := collectScheduledAttempt(t, ctx, restarted, db, run.ID, model.StatusSucceeded)
	if early.Load() || requests.Load() != 2 || completed.DistinctRecords != 1 || completed.Errors != 1 {
		t.Fatalf("Resume lost the durable quota boundary: early=%t requests=%d run=%+v", early.Load(), requests.Load(), completed)
	}
}

func TestTransientFailurePreservesCommittedCheckpoint(t *testing.T) {
	for _, recover := range []bool{true, false} {
		name, wantStatus, wantErrors, wantPages := "exhausted", model.StatusFailed, 4, 1
		if recover {
			name, wantStatus, wantErrors, wantPages = "recovered", model.StatusSucceeded, 1, 2
		}
		t.Run(name, func(t *testing.T) {
			ctx, endpoint, db := schedulerDatabase(t)
			control, err := pgx.Connect(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = control.Close(context.Background()) })
			var checkpoint atomic.Pointer[model.Run]
			var attempts atomic.Int32
			var observedBoundaries atomic.Int32
			var wrongOffset atomic.Bool
			var resumeRecovery atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				offset := r.URL.Query().Get("offset")
				if offset == "0" {
					_, _ = w.Write([]byte(`{"items":[{"id":"before","title":"Before failure"}],"total":2}`))
					return
				}
				if offset != "1" {
					wrongOffset.Store(true)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				attempt := attempts.Add(1)
				saved := checkpoint.Load()
				if attempt > 1 && saved != nil {
					held, readErr := db.GetRun(ctx, saved.ID)
					var archived, staged int
					archiveErr := control.QueryRow(ctx, `SELECT
						(SELECT count(*) FROM ingest.pages WHERE run_id=$1 AND error<>'' AND metadata->>'http_status'='504'),
						(SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1 AND source_id='before')`, saved.ID).Scan(&archived, &staged)
					if readErr == nil && archiveErr == nil && archived == int(attempt)-1 && staged == 1 &&
						held.Pages == saved.Pages && held.Errors == int(attempt)-1 &&
						held.DistinctRecords == saved.DistinctRecords && bytes.Equal(held.Cursor, saved.Cursor) {
						observedBoundaries.Add(1)
					}
				}
				if recover && attempt > 1 || resumeRecovery.Load() {
					_, _ = w.Write([]byte(`{"items":[{"id":"after","title":"After recovery"}],"total":2}`))
					return
				}
				w.Header().Set("Retry-After", "0")
				w.Header().Set("X-Private", "private-response-header")
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte("private upstream failure payload"))
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("transient-"+name, server.URL)
			p.Schedule = model.Schedule{}
			saveScheduledProvider(t, registry, p, "")
			manager := New(db, registry, nil, 1)
			maxPages := 1
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull, MaxPages: &maxPages})
			if err != nil {
				t.Fatal(err)
			}
			held := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusPaused)
			if held.Pages != 1 || held.DistinctRecords != 1 || held.Errors != 0 {
				t.Fatalf("initial committed page was lost: %+v", held)
			}
			checkpoint.Store(&held)
			if _, err := manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			finished := collectScheduledAttempt(t, ctx, manager, db, run.ID, wantStatus)
			wantAttempts := int32(wantErrors)
			if recover {
				wantAttempts++
			}
			if wrongOffset.Load() || attempts.Load() != wantAttempts || observedBoundaries.Load() != wantAttempts-1 ||
				finished.Pages != wantPages || finished.DistinctRecords != int64(wantPages) || finished.Errors != wantErrors {
				t.Fatalf("transient response advanced or lost its boundary: attempts=%d recorded=%d wrong_offset=%t run=%+v",
					attempts.Load(), observedBoundaries.Load(), wrongOffset.Load(), finished)
			}
			assertTransientObservations(t, ctx, control, run.ID, http.StatusGatewayTimeout, wantErrors, recover)
			if !recover {
				if !bytes.Equal(finished.Cursor, held.Cursor) || finished.TraversalDone ||
					!strings.Contains(finished.Error, "HTTP 504") || !strings.Contains(finished.Error, "4 attempts") {
					t.Fatalf("exhaustion lost its resumable checkpoint or safe diagnosis: %+v", finished)
				}
				var staged, published int
				if err := control.QueryRow(ctx, `SELECT
					(SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1 AND source_id='before'),
					(SELECT count(*) FROM ingest.torrents WHERE provider_id=$2)`, run.ID, p.ID).Scan(&staged, &published); err != nil || staged != 1 || published != 0 {
					t.Fatalf("exhaustion discarded staging or published an incomplete Full: staged=%d published=%d error=%v", staged, published, err)
				}
				resumeRecovery.Store(true)
				restarted := New(db, registry, nil, 1)
				if _, err := restarted.Resume(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
				completed := collectScheduledAttempt(t, ctx, restarted, db, run.ID, model.StatusSucceeded)
				if attempts.Load() != 5 || observedBoundaries.Load() != 4 || completed.Pages != 2 || completed.DistinctRecords != 2 || completed.Errors != 4 {
					t.Fatalf("Resume did not recover the exhausted request: attempts=%d boundaries=%d run=%+v", attempts.Load(), observedBoundaries.Load(), completed)
				}
				assertTransientObservations(t, ctx, control, run.ID, http.StatusGatewayTimeout, 4, false)
			}
			var published int
			if err := control.QueryRow(ctx, "SELECT count(*) FROM ingest.torrents WHERE provider_id=$1 AND source_id IN ('before','after')", p.ID).Scan(&published); err != nil || published != 2 {
				t.Fatalf("successful retry failed Full publication: published=%d error=%v", published, err)
			}
		})
	}
}

func TestTransientFailurePausePreservesCooldownAcrossResume(t *testing.T) {
	ctx, endpoint, db := schedulerDatabase(t)
	control, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close(context.Background()) })
	var requests atomic.Int32
	var deadline atomic.Int64
	var early atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			deadline.Store(time.Now().Add(2 * time.Second).UnixNano())
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("original transient failure"))
			return
		}
		if time.Now().UnixNano() < deadline.Load() {
			early.Store(true)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"recovered","title":"Recovered"}],"total":1}`))
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := schedulerProvider("paused-transient", server.URL)
	p.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	notices, unsubscribe := manager.Subscribe()
	t.Cleanup(unsubscribe)
	finished := make(chan error, 1)
	go func() {
		status, message, reason := manager.collect(ctx, *claimed)
		_, err := db.FinishRun(ctx, run.ID, status, message, reason)
		if err == nil && status != model.StatusPaused {
			err = fmt.Errorf("transient pause finished as %s: %s", status, message)
		}
		finished <- err
	}()
	// Wait for the committed failure, not merely for its HTTP response, so
	// Pause exercises the durable cooldown rather than an in-flight request.
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
waitForArchive:
	for {
		select {
		case <-notices:
			held, err := db.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if held.Errors == 1 {
				break waitForArchive
			}
		case err := <-finished:
			t.Fatalf("collection stopped before its transient boundary: %v", err)
		case <-timeout.C:
			t.Fatal("transient response was not archived")
		}
	}
	if _, err := manager.Pause(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Pause remained blocked behind transient cooldown")
	}
	held, err := db.GetRun(ctx, run.ID)
	if err != nil || held.PauseReason != model.PauseManual || held.Pages != 0 || held.Errors != 1 ||
		!bytes.Equal(held.Cursor, claimed.Cursor) || requests.Load() != 1 {
		t.Fatalf("transient pause changed its checkpoint: %+v requests=%d error=%v", held, requests.Load(), err)
	}
	retryAt, err := db.RunRetryAt(ctx, run.ID)
	if err != nil || retryAt.UnixNano() < deadline.Load() {
		t.Fatalf("transient cooldown was not durable: retry_at=%v error=%v", retryAt, err)
	}
	assertTransientObservations(t, ctx, control, run.ID, http.StatusServiceUnavailable, 1, true)
	restarted := New(db, registry, nil, 1)
	if _, err := restarted.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	completed := collectScheduledAttempt(t, ctx, restarted, db, run.ID, model.StatusSucceeded)
	if early.Load() || requests.Load() != 2 || completed.Pages != 1 || completed.DistinctRecords != 1 || completed.Errors != 1 {
		t.Fatalf("Resume lost or duplicated the transient boundary: early=%t requests=%d run=%+v", early.Load(), requests.Load(), completed)
	}
	assertTransientObservations(t, ctx, control, run.ID, http.StatusServiceUnavailable, 1, true)
	if retryAt, err := db.RunRetryAt(ctx, run.ID); err != nil || !retryAt.IsZero() {
		t.Fatalf("successful response retained a stale cooldown: retry_at=%v error=%v", retryAt, err)
	}
}

func assertTransientObservations(t *testing.T, ctx context.Context, control *pgx.Conn, runID string, status, attempts int, lastRetry bool) {
	t.Helper()
	rows, err := control.Query(ctx, "SELECT metadata,body,payload_retained,error FROM ingest.pages WHERE run_id=$1 AND error<>'' ORDER BY id", runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw, body []byte
		var retained bool
		var message string
		if err := rows.Scan(&raw, &body, &retained, &message); err != nil {
			t.Fatal(err)
		}
		count++
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil {
			t.Fatal(err)
		}
		retry := count < attempts || lastRetry
		if len(metadata) != 5 || metadata["failure_code"] != "http" || metadata["http_status"] != float64(status) ||
			metadata["retry_attempt"] != float64(count) || metadata["retry"] != retry {
			t.Fatalf("transient diagnostic lost its safe retry metadata: %s", raw)
		}
		retryAt, _ := metadata["retry_at"].(string)
		if _, err := time.Parse(time.RFC3339Nano, retryAt); err != nil {
			t.Fatalf("invalid durable retry deadline %q: %v", retryAt, err)
		}
		if retained || len(body) != 0 || strings.Contains(message, "private") {
			t.Fatalf("failure observation retained private payload or lost its diagnosis: retained=%t body=%q error=%q", retained, body, message)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != attempts {
		t.Fatalf("transient responses were duplicated or omitted: got %d want %d", count, attempts)
	}
}
