package jobs

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestHTMLAfterQuotaPreservesStagingAndCheckpointUntilRecovery(t *testing.T) {
	for _, recover := range []bool{true, false} {
		name, status, failures, attempts := "exhausted", model.StatusFailed, 4, int32(4)
		if recover {
			name, status, failures, attempts = "recovered", model.StatusSucceeded, 2, 3
		}
		t.Run(name, func(t *testing.T) {
			ctx, endpoint, db := schedulerDatabase(t)
			control, err := pgx.Connect(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = control.Close(context.Background()) })
			var requests atomic.Int32
			var resumed atomic.Bool
			var saved atomic.Pointer[model.Run]
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("offset") == "0" {
					_, _ = w.Write([]byte(`{"items":[{"id":"before","title":"Before"}],"total":2}`))
					return
				}
				if r.URL.Query().Get("offset") != "1" {
					t.Error("retry advanced the requested offset")
				}
				n := requests.Add(1)
				if n > 1 {
					checkpoint := saved.Load()
					held, err := db.GetRun(ctx, checkpoint.ID)
					var archived int
					if err != nil || !bytes.Equal(held.Cursor, checkpoint.Cursor) || held.Pages != 1 || held.DistinctRecords != 1 {
						t.Error("rejected response advanced the committed checkpoint")
					}
					if err := control.QueryRow(ctx, "SELECT count(*) FROM ingest.pages WHERE run_id=$1 AND error<>''", checkpoint.ID).Scan(&archived); err != nil || archived != int(n)-1 {
						t.Error("response was replayed before committing its diagnostics")
					}
				}
				w.Header().Set("Retry-After", "0")
				if n == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				if recover && n == 3 || resumed.Load() {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"items":[{"id":"after","title":"After"}],"total":2}`))
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html>private upstream response</html>"))
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := schedulerProvider("quota-html-"+name, server.URL)
			p.Schedule = model.Schedule{}
			saveScheduledProvider(t, registry, p, "")
			manager := New(db, registry, nil, 1)
			maxPages := 1
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull, MaxPages: &maxPages})
			if err != nil {
				t.Fatal(err)
			}
			held := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusPaused)
			saved.Store(&held)
			if _, err := manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
			finished := collectScheduledAttempt(t, ctx, manager, db, run.ID, status)
			if requests.Load() != attempts || finished.Errors != failures {
				t.Fatalf("unbounded or unrecorded recovery: requests=%d errors=%d", requests.Load(), finished.Errors)
			}
			var htmlErrors int
			if err := control.QueryRow(ctx, `SELECT count(*) FROM ingest.pages WHERE run_id=$1 AND metadata->>'http_status'='200' AND metadata->>'failure_code'='parse' AND NOT payload_retained AND body=''::bytea`, run.ID).Scan(&htmlErrors); err != nil || htmlErrors != failures-1 {
				t.Fatalf("HTML diagnostics lost their cause or retained private bytes: count=%d error=%v", htmlErrors, err)
			}
			if !recover {
				var staged, published int
				if err := control.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1), (SELECT count(*) FROM ingest.torrents WHERE provider_id=$2)`, run.ID, p.ID).Scan(&staged, &published); err != nil || staged != 1 || published != 0 || !bytes.Equal(held.Cursor, finished.Cursor) || !strings.Contains(finished.Error, "HTML instead of JSON") || strings.Contains(finished.Error, "private") {
					t.Fatalf("failure discarded staging or published incomplete data: staged=%d published=%d error=%v", staged, published, err)
				}
				resumed.Store(true)
				manager = New(db, registry, nil, 1)
				if _, err := manager.Resume(ctx, run.ID); err != nil {
					t.Fatal(err)
				}
				finished = collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
			}
			var published int
			if err := control.QueryRow(ctx, "SELECT count(*) FROM ingest.torrents WHERE provider_id=$1", p.ID).Scan(&published); err != nil || published != 2 || finished.Pages != 2 || finished.DistinctRecords != 2 {
				t.Fatalf("recovery failed complete publication: count=%d run=%+v error=%v", published, finished, err)
			}
		})
	}
}
