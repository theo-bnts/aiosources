package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestFullPublicationCompletesOrResumesWithoutRefetch(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "long publication"
		if shutdown {
			name = "shutdown during publication"
		}
		t.Run(name, func(t *testing.T) {
			ctx, endpoint, db := schedulerDatabase(t)
			control, err := pgx.Connect(ctx, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = control.Close(context.Background()) })
			if _, err := control.Exec(ctx, `
CREATE FUNCTION ingest.block_test_publication() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(976215::bigint);
    RETURN NULL;
END $$;
CREATE TRIGGER block_test_publication BEFORE INSERT ON ingest.torrents
FOR EACH STATEMENT EXECUTE FUNCTION ingest.block_test_publication();
SELECT pg_advisory_lock(976215::bigint);`); err != nil {
				t.Fatal(err)
			}
			const body = `{"items":[{"id":"original","title":"Original release"}],"total":1}`
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			provider := schedulerProvider("publication", server.URL)
			provider.Schedule = model.Schedule{}
			saveScheduledProvider(t, registry, provider, "")
			manager := New(db, registry, nil, 1)
			start := func(manager *Manager) {
				if err := manager.Start(ctx); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := manager.Close(cleanup); err != nil {
						t.Error(err)
					}
				})
			}
			start(manager)
			unlocked := false
			release := func() {
				if unlocked {
					return
				}
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := control.Exec(cleanup, "SELECT pg_advisory_unlock(976215::bigint)"); err != nil {
					t.Error(err)
				}
				unlocked = true
			}
			t.Cleanup(release)
			duration := 1
			run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull, MaxDurationSeconds: &duration})
			if err != nil {
				t.Fatal(err)
			}
			boundary, cancelBoundary := context.WithTimeout(ctx, 10*time.Second)
			defer cancelBoundary()
			for {
				var blocked bool
				if err := control.QueryRow(boundary, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND wait_event='advisory')`).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case <-boundary.Done():
					t.Fatal("publication did not reach its transaction boundary")
				}
			}
			checkpoint, err := db.GetRun(ctx, run.ID)
			if err != nil || !checkpoint.TraversalDone || checkpoint.Pages != 1 {
				t.Fatalf("completed traversal was not checkpointed: %+v %v", checkpoint, err)
			}
			if shutdown {
				stop, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := manager.Close(stop)
				cancel()
				if err != nil {
					t.Fatal("shutdown did not cancel publication promptly:", err)
				}
				held, err := db.GetRun(ctx, run.ID)
				if err != nil || held.Status != model.StatusQueued || held.PauseReason != "" || held.PauseRequested || !held.TraversalDone || !bytes.Equal(held.Cursor, checkpoint.Cursor) {
					t.Fatalf("publication interruption lost its resumable checkpoint: %+v %v", held, err)
				}
				if live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID}); err != nil || live.Total != 0 {
					t.Fatalf("interrupted transaction published a partial catalogue: %+v %v", live, err)
				}
				release()
				manager = New(db, registry, nil, 1)
				start(manager)
			} else {
				// The former 15-second cleanup deadline must not bound a full
				// catalogue's atomic publication, including waits for writers.
				timer := time.NewTimer(16 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-manager.Done():
					t.Fatal("slow publication terminated the collection manager")
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				release()
			}
			completion, cancelCompletion := context.WithTimeout(ctx, 10*time.Second)
			defer cancelCompletion()
			for {
				finished, err := db.GetRun(completion, run.ID)
				if err != nil {
					t.Fatal(err)
				}
				if finished.Status == model.StatusSucceeded {
					if finished.Pages != 1 || finished.Records != 1 || !bytes.Equal(finished.Cursor, checkpoint.Cursor) || requests.Load() != 1 {
						t.Fatalf("finalization refetched or changed committed traversal: %+v requests=%d", finished, requests.Load())
					}
					break
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case <-completion.Done():
					t.Fatalf("publication did not complete: %+v", finished)
				}
			}
			live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil || live.Total != 1 || live.Items[0].SourceID != "original" {
				t.Fatalf("completed publication lost the release: %+v %v", live, err)
			}
			raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || raw.Total != 1 {
				t.Fatalf("finalization lost the source archive: %+v %v", raw, err)
			}
			archived, _, err := db.RawPage(ctx, raw.Items[0].PageID)
			if !errors.Is(err, model.ErrNotFound) || len(archived) != 0 || raw.Items[0].PayloadRetained {
				t.Fatalf("finalization retained an unwanted source payload: %v", err)
			}
		})
	}
}

func TestTorznabUnidentifiedRepeatPreservesCheckpointAcrossResume(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("t") == "caps" {
			_, _ = fmt.Fprint(w, `<caps><limits max="2" default="2"/><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			return
		}
		step := searches.Add(1)
		// Updating a volatile counter must not hide an ignored offset.
		_, _ = fmt.Fprintf(w, `<rss xmlns:t="http://torznab.com/schemas/2015/feed"><channel><item><title>Unidentified release</title><t:attr name="size" value="1024"/><t:attr name="seeders" value="%d"/></item></channel></rss>`, step)
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := model.Provider{
		Version: 1, ID: "unidentified-repeat", Name: "Unidentified repeat", Adapter: "torznab", URL: server.URL,
		Enabled: true, PageSize: 2, RequestInterval: "1ms",
		Auth: model.Auth{Type: "none"}, HTTP: model.HTTPConfig{Method: "GET"},
	}
	seedCollectionBaseline(t, ctx, db, provider)
	saveScheduledProvider(t, registry, provider, "")
	manager := New(db, registry, nil, 1)
	budget := 1
	run, err := manager.Enqueue(ctx, model.StartRun{
		ProviderID: provider.ID, Mode: model.ModeIncremental, MaxPages: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	paused := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusPaused)
	if paused.Pages != 1 || paused.Errors != 1 || searches.Load() != 1 {
		t.Fatalf("invalid record did not retain a resumable first-page observation: %+v", paused)
	}
	// A new manager reconstructs its connector from the persisted checkpoint.
	manager = New(db, registry, nil, 1)
	if _, err := manager.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	failed := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusFailed)
	if failed.Pages != paused.Pages || !bytes.Equal(failed.Cursor, paused.Cursor) || searches.Load() != 2 {
		t.Fatalf("repeated unidentified page advanced or lost its checkpoint: %+v", failed)
	}
	rows, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || rows.Total != 0 {
		t.Fatalf("pagination fallback fabricated native catalogue identities: %+v %v", rows, err)
	}
}
