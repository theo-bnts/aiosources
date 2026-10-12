package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
)

func TestPreviewRetainsParsedFieldsWithoutLiveWrites(t *testing.T) {
	ctx, _, db := lifecycleDatabase(t)
	originals := []string{
		`{ "id": "zero", "title":"Zero", "size":0, "seeders":0, "peers":null, "unknown":{"exact":1e2,"nil":null} }`,
		`{"id":"invalid","title":"Keep these bytes","size":"not-a-number","unknown":[0,null,false]}`,
		`{"id":"third","title":"Third"}`, `{"id":"fourth","title":"Fourth"}`,
		`{"id":"fifth","title":"Fifth"}`, `{"id":"sixth","title":"Sixth"}`,
	}
	bodies := make([]string, 3)
	for i := range bodies {
		bodies[i] = "{\n \"items\": [" + strings.Join(originals[2*i:2*i+2], ",\n") + "], \"total\": 8, \"unknown_page\": null\n}\n"
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-test-token" {
			http.Error(w, "synthetic authentication required", http.StatusUnauthorized)
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 || offset%2 != 0 || offset/2 >= len(bodies) {
			http.Error(w, "unexpected preview request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[offset/2]))
	}))
	t.Cleanup(server.Close)
	p := lifecycleProvider("preview-source", server.URL)
	p.Auth = model.Auth{Type: "bearer", SecretRef: "synthetic-token"}
	registry, doc := lifecycleRegistry(t, p)
	secrets, err := vault.New(ctx, db, bytes.Repeat([]byte{0x37}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.Put(ctx, "synthetic-token", "synthetic-test-token"); err != nil {
		t.Fatal(err)
	}
	seedLive(t, ctx, db, doc.Provider, lifecycleRecord("existing", "Existing live record"))
	before := liveRows(t, ctx, db, p.ID)
	manager := lifecycleManager(t, ctx, db, registry, secrets.Resolve, 4)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, ctx, db, manager, run.ID, model.StatusSucceeded)
	if requests.Load() != 3 || run.Pages != 3 || run.Records != 6 || run.Errors != 1 {
		t.Fatalf("preview crossed its boundary: requests=%d pages=%d records=%d errors=%d", requests.Load(), run.Pages, run.Records, run.Errors)
	}
	if after := liveRows(t, ctx, db, p.ID); !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed live rows: before=%v after=%v", before, after)
	}
	raw := rawRows(t, ctx, db, run.ID)
	if len(raw) != len(originals) {
		t.Fatalf("retained %d raw records, want %d", len(raw), len(originals))
	}
	pageIDs := map[int64]bool{}
	for _, item := range raw {
		if item.PayloadRetained || len(item.Raw) != 0 {
			t.Errorf("preview persisted a raw item payload for %q", item.SourceID)
		}
		body, _, err := db.RawPage(ctx, item.PageID)
		if !errors.Is(err, model.ErrNotFound) || len(body) != 0 {
			t.Errorf("preview persisted its source response: %v", err)
		}
		pageIDs[item.PageID] = true
		switch item.SourceID {
		case "zero":
			if item.Fields["size"] != json.Number("0") {
				t.Errorf("present zero lost or output projection applied to storage: %v", item.Fields)
			}
			if _, present := item.Fields["seeders"]; present {
				t.Error("Preview retained a transient seeder count")
			}
			if _, ok := item.Fields["peers"]; ok {
				t.Errorf("null became a canonical value: %v", item.Fields)
			}
		case "invalid":
			if item.Error == "" {
				t.Error("normalization failure was not retained with its original record")
			}
		}
	}
	if len(pageIDs) != 3 {
		t.Fatalf("retained %d distinct responses, want 3", len(pageIDs))
	}
}

func TestFullRunPublishesOnlyAfterResumedCompletion(t *testing.T) {
	ctx, _, db := lifecycleDatabase(t)
	secondEntered, releaseSecond := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"items":[{"id":"a","title":"A"},{"id":"b","title":"B"}],"total":4}`))
		case "2":
			enterOnce.Do(func() { close(secondEntered) })
			select {
			case <-releaseSecond:
				_, _ = w.Write([]byte(`{"items":[{"id":"c","title":"C"},{"id":"d","title":"D"}],"total":4}`))
			case <-r.Context().Done():
			}
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseSecond) }) })
	p := lifecycleProvider("full-source", server.URL)
	registry, doc := lifecycleRegistry(t, p)
	seedLive(t, ctx, db, doc.Provider, lifecycleRecord("old", "Old"))
	manager := lifecycleManager(t, ctx, db, registry, nil, 4)
	budget := 1
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull, MaxPages: &budget})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, ctx, db, manager, run.ID, model.StatusPaused)
	assertLiveIDs(t, ctx, db, p.ID, "old")
	if requests.Load() != 1 || run.Pages != 1 || run.TraversalDone {
		t.Fatalf("full budget was treated as completion: requests=%d run=%+v", requests.Load(), run)
	}
	if _, err := manager.Resume(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, secondEntered, "resumed second request")
	assertLiveIDs(t, ctx, db, p.ID, "old")

	// A concurrent consumer may see either complete generation, never a mixed
	// or empty set while FinishRun replaces the provider's live dataset.
	observeCtx, stopObserver := context.WithCancel(ctx)
	observed, observation := make(chan struct{}), make(chan error, 1)
	go func() {
		first := true
		for {
			rows, err := db.ListTorrents(observeCtx, model.ListOptions{ProviderID: p.ID, Limit: 100})
			if err != nil {
				if observeCtx.Err() != nil {
					observation <- nil
				} else {
					observation <- err
				}
				return
			}
			ids := torrentIDs(rows.Items)
			if !slices.Equal(ids, []string{"old"}) && !slices.Equal(ids, []string{"a", "b", "c", "d"}) {
				observation <- fmt.Errorf("partial full publication visible: %v", ids)
				return
			}
			if first {
				close(observed)
				first = false
			}
		}
	}()
	t.Cleanup(stopObserver)
	awaitSignal(t, ctx, observed, "publication observer")
	releaseOnce.Do(func() { close(releaseSecond) })
	run = awaitRun(t, ctx, db, manager, run.ID, model.StatusSucceeded)
	stopObserver()
	select {
	case err := <-observation:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("publication observer did not stop")
	}
	assertLiveIDs(t, ctx, db, p.ID, "a", "b", "c", "d")
	if requests.Load() != 2 || run.Pages != 2 || run.Records != 4 || !run.TraversalDone {
		t.Fatalf("resume refetched or lost committed work: requests=%d run=%+v", requests.Load(), run)
	}
}

func TestCancellationPreventsFullPublication(t *testing.T) {
	ctx, _, db := lifecycleDatabase(t)
	// This scenario verifies the optional complete processing history.
	useFullStorage(t, ctx, db)
	entered, requestCancelled := make(chan struct{}), make(chan struct{})
	releaseHandler := make(chan struct{})
	var enterOnce, cancelOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			_, _ = w.Write([]byte(`{"items":[{"id":"a"},{"id":"b"}],"total":4}`))
			return
		}
		enterOnce.Do(func() { close(entered) })
		select {
		case <-r.Context().Done():
			cancelOnce.Do(func() { close(requestCancelled) })
		case <-releaseHandler:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(releaseHandler) })
	p := lifecycleProvider("cancel-source", server.URL)
	registry, doc := lifecycleRegistry(t, p)
	seedLive(t, ctx, db, doc.Provider, lifecycleRecord("old", "Old"))
	manager := lifecycleManager(t, ctx, db, registry, nil, 4)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, entered, "second full request")
	committed, err := db.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Pages != 1 {
		t.Fatalf("expected first page to commit before cancellation, got %d", committed.Pages)
	}
	if _, err := manager.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, ctx, db, manager, run.ID, model.StatusCancelled)
	awaitSignal(t, ctx, requestCancelled, "HTTP cancellation")
	assertLiveIDs(t, ctx, db, p.ID, "old")
	if run.Pages != 1 || !bytes.Equal(run.Cursor, committed.Cursor) || run.TraversalDone {
		t.Fatalf("cancellation advanced the checkpoint: before=%+v after=%+v", committed, run)
	}
	if len(rawRows(t, ctx, db, run.ID)) != 2 {
		t.Fatal("cancellation lost the committed first page")
	}

	// Publication also rejects a cancellation arriving after the final page
	// commit but before the worker's successful finish transaction.
	closeLifecycleManager(t, manager)
	late := claimRun(t, ctx, db, doc, model.ModeFull)
	late = savePage(t, ctx, db, late, model.Page{Body: []byte(`{"id":"late"}`), Items: []model.Record{lifecycleRecord("late", "Late")}, Done: true}, "late")
	if _, err := db.RequestCancel(ctx, late.ID); err != nil {
		t.Fatal(err)
	}
	late, err = db.FinishRun(ctx, late.ID, model.StatusSucceeded, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if late.Status != model.StatusCancelled {
		t.Fatalf("late cancellation published: %s", late.Status)
	}
	assertLiveIDs(t, ctx, db, p.ID, "old")
}

func TestRejectedPagesRetainRawWithoutChangingCheckpointOrStaging(t *testing.T) {
	ctx, _, db := lifecycleDatabase(t)
	// This scenario verifies the optional complete processing history.
	useFullStorage(t, ctx, db)
	p := lifecycleProvider("rejected-source", "http://source.example.invalid")
	doc := model.ProviderDocument{Provider: p, Revision: "synthetic-revision"}
	seedLive(t, ctx, db, p, lifecycleRecord("old", "Old"))
	run := claimRun(t, ctx, db, doc, model.ModeFull)
	auxiliary := lifecycleRecord("a", "Enrichment must not replace A")
	auxiliary.Auxiliary = true
	auxiliaryOnly := lifecycleRecord("aux-only", "Enrichment must not become a live identity")
	auxiliaryOnly.Auxiliary = true
	run = savePage(t, ctx, db, run, model.Page{
		Body: []byte("initial response"), Items: []model.Record{lifecycleRecord("a", "A"), auxiliary, auxiliaryOnly},
		Next: json.RawMessage(`{"offset":1}`), RequireUniqueIDs: true,
	}, "first-identities")
	if run.Pages != 1 || run.Records != 1 || run.Errors != 0 {
		t.Fatalf("auxiliary observation changed primary progress: %+v", run)
	}
	checkpoint := bytes.Clone(run.Cursor)
	expectedRaw := 3
	attempts := []struct {
		name        string
		page        model.Page
		fingerprint string
		wantErr     error
	}{
		{"fetch failure", model.Page{Body: []byte("failed response including original bytes"), Items: []model.Record{lifecycleRecord("error-only", "Do not stage")}, Error: "synthetic fetch failure", ResetStaging: true}, "unused-on-error", nil},
		{"repeated fingerprint", model.Page{Body: []byte("repeated response with changing volatile fields"), Items: []model.Record{lifecycleRecord("repeat-only", "Do not stage")}}, "first-identities", model.ErrStalled},
		{"duplicate within reset page", model.Page{Body: []byte("duplicate IDs in new generation"), Items: []model.Record{lifecycleRecord("b", "B1"), lifecycleRecord("b", "B2")}, ResetStaging: true, RequireUniqueIDs: true}, "duplicate-within-page", model.ErrStalled},
		{"duplicate staged ID", model.Page{Body: []byte("duplicate previously staged identity"), Items: []model.Record{lifecycleRecord("a", "Must not overwrite A")}, RequireUniqueIDs: true}, "duplicate-across-pages", model.ErrStalled},
	}
	for index, attempt := range attempts {
		attempt.page.Next = json.RawMessage(`{"offset":999}`)
		attempt.page.Done = true
		beforeRecords, beforeErrors := run.Records, run.Errors
		saved, err := db.SavePage(ctx, run, attempt.page, attempt.fingerprint)
		if !errors.Is(err, attempt.wantErr) {
			t.Fatalf("%s: error %v, want %v", attempt.name, err, attempt.wantErr)
		}
		run = saved
		if run.Pages != 1 || !bytes.Equal(run.Cursor, checkpoint) || run.TraversalDone {
			t.Fatalf("%s advanced successful traversal: %+v", attempt.name, run)
		}
		if run.Records != beforeRecords+len(attempt.page.Items) || run.Errors != beforeErrors+1 {
			t.Fatalf("%s lost rejected-attempt accounting: %+v", attempt.name, run)
		}
		expectedRaw += len(attempt.page.Items)
		raw := rawRows(t, ctx, db, run.ID)
		if len(raw) != expectedRaw {
			t.Fatalf("%s retained %d raw records, want %d", attempt.name, len(raw), expectedRaw)
		}
		for i := range attempt.page.Items {
			body, _, err := db.RawPage(ctx, raw[i].PageID)
			if !errors.Is(err, model.ErrNotFound) || len(body) != 0 || raw[i].PayloadRetained || len(raw[i].Raw) != 0 {
				t.Fatalf("%s retained rejected payloads: %v", attempt.name, err)
			}
		}
		events, err := db.Events(ctx, run.ID, model.ListOptions{Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		errored := 0
		for _, event := range events.Items {
			if event.Kind == "page_error" {
				errored++
			}
		}
		if errored != index+1 {
			t.Fatalf("%s archived %d page errors, want %d", attempt.name, errored, index+1)
		}
		assertLiveIDs(t, ctx, db, p.ID, "old")
	}
	// Neither a failed reset nor a rejected duplicate may erase the previously
	// staged A; auxiliary data must not overwrite it or create live identities.
	run = savePage(t, ctx, db, run, model.Page{Body: []byte("final response"), Items: []model.Record{lifecycleRecord("c", "C")}, Done: true, RequireUniqueIDs: true}, "final-identities")
	if _, err := db.FinishRun(ctx, run.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	assertLiveIDs(t, ctx, db, p.ID, "a", "c")
	for _, row := range liveRows(t, ctx, db, p.ID) {
		if row.SourceID == "a" && row.Fields["title"] != "A" {
			t.Fatalf("rejected or auxiliary data overwrote A: %v", row.Fields)
		}
	}
}

func TestManagerArchivesRepeatedIdentityPageOnlyOnce(t *testing.T) {
	ctx, _, db := lifecycleDatabase(t)
	// This scenario verifies the optional complete processing history.
	useFullStorage(t, ctx, db)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"items":[{"id":"same-a","seeders":%d},{"id":"same-b","seeders":%d}],"total":6}`, count, count)
	}))
	t.Cleanup(server.Close)
	p := lifecycleProvider("repeat-source", server.URL)
	registry, doc := lifecycleRegistry(t, p)
	seedLive(t, ctx, db, doc.Provider, lifecycleRecord("old", "Old"))
	manager := lifecycleManager(t, ctx, db, registry, nil, 4)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, ctx, db, manager, run.ID, model.StatusFailed)
	if requests.Load() != 2 || run.Pages != 1 || run.Records != 4 || run.Errors != 1 {
		t.Fatalf("repeat detection or archive accounting failed: requests=%d run=%+v", requests.Load(), run)
	}
	var checkpoint struct {
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(run.Cursor, &checkpoint); err != nil || checkpoint.Offset != 2 {
		t.Fatalf("repeat advanced successful continuation: %s, %v", run.Cursor, err)
	}
	if raw := rawRows(t, ctx, db, run.ID); len(raw) != 4 {
		t.Fatalf("repeat was lost or archived twice: %d records", len(raw))
	}
	events, err := db.Events(ctx, run.ID, model.ListOptions{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	saved, rejected := 0, 0
	for _, event := range events.Items {
		switch event.Kind {
		case "page_saved":
			saved++
		case "page_error":
			rejected++
		}
	}
	if saved != 1 || rejected != 1 {
		t.Fatalf("repeat produced non-atomic or duplicated archive events: saved=%d rejected=%d", saved, rejected)
	}
	assertLiveIDs(t, ctx, db, p.ID, "old")
}

func TestLiveLocksAndFinalCheckpointSurviveRecovery(t *testing.T) {
	ctx, databaseURL, owner := lifecycleDatabase(t)
	useFullStorage(t, ctx, owner)
	peer := openLifecycleStore(t, ctx, databaseURL)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "final checkpoint must not refetch", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	p := lifecycleProvider("recovery-source", server.URL)
	registry, doc := lifecycleRegistry(t, p)
	seedLive(t, ctx, owner, doc.Provider, lifecycleRecord("old", "Old"))
	completed := claimRun(t, ctx, owner, doc, model.ModeFull)
	completed = savePage(t, ctx, owner, completed, model.Page{Body: []byte("last original page"), Items: []model.Record{lifecycleRecord("new", "New")}, Next: json.RawMessage(`{"version":1,"offset":1,"done":true}`), Done: true}, "final")
	otherProvider := lifecycleProvider("other-locked-source", server.URL)
	other := claimRun(t, ctx, owner, model.ProviderDocument{Provider: otherProvider}, model.ModePreview)

	// Two leases saturate collection slots in a four-connection pool. Reads,
	// cancellation and cross-process recovery must still make progress.
	if _, err := owner.RequestCancel(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.CreateRun(ctx, model.Run{ProviderID: p.ID, Mode: model.ModeFull, Config: doc.Provider}); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("second instance admitted an active duplicate: %v", err)
	}
	if claimed, err := peer.ClaimNext(ctx); err != nil || claimed != nil {
		t.Fatalf("second instance claimed an owned run: %v, %v", claimed, err)
	}
	if err := peer.RecoverInterrupted(ctx, map[string]bool{p.ID: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{completed.ID, other.ID} {
		locked, err := peer.GetRun(ctx, id)
		if err != nil || locked.Status != model.StatusRunning {
			t.Fatalf("recovery stole live locked run %s: %+v, %v", id, locked, err)
		}
	}
	assertLiveIDs(t, ctx, peer, p.ID, "old")

	// Close releases sessions without finishing rows, reproducing an owner
	// interruption after the final SavePage transaction but before publication.
	owner.Close()
	if err := peer.RecoverInterrupted(ctx, map[string]bool{p.ID: true}); err != nil {
		t.Fatal(err)
	}
	recovered, err := peer.GetRun(ctx, completed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.StatusQueued || recovered.PauseReason != "" || recovered.PauseRequested || !recovered.TraversalDone || recovered.Pages != completed.Pages || !bytes.Equal(recovered.Cursor, completed.Cursor) {
		t.Fatalf("recovery lost final traversal evidence: %+v", recovered)
	}
	cancelled, err := peer.GetRun(ctx, other.ID)
	if err != nil || cancelled.Status != model.StatusCancelled {
		t.Fatalf("recovery ignored durable cancellation: %+v, %v", cancelled, err)
	}
	manager := lifecycleManager(t, ctx, peer, registry, nil, 4)
	resumed := awaitRun(t, ctx, peer, manager, completed.ID, model.StatusSucceeded)
	if requests.Load() != 0 || resumed.Pages != completed.Pages || resumed.Records != completed.Records {
		t.Fatalf("completed traversal was refetched: requests=%d run=%+v", requests.Load(), resumed)
	}
	assertLiveIDs(t, ctx, peer, p.ID, "new")
	if len(rawRows(t, ctx, peer, completed.ID)) != 1 {
		t.Fatal("recovery duplicated or lost the final raw record")
	}
}

func TestLegacyImportIsAdditiveWithoutResurrectingFullDeletions(t *testing.T) {
	ctx, databaseURL, db := lifecycleDatabase(t)
	// This scenario verifies the optional complete processing history.
	useFullStorage(t, ctx, db)
	// SQL is used only to model the pre-existing public legacy table, never to
	// inspect or mutate implementation-owned ingest state.
	fixture, err := pgx.Connect(ctx, withoutPoolOptions(t, databaseURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fixture.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	if _, err := fixture.Exec(ctx, `CREATE TABLE public.torrents (provider text, id text, name text, file_size_bytes bigint, seeders integer, unknown jsonb);
INSERT INTO public.torrents VALUES
('legacy-source','historical','Historical',0,0,'{"retained":null}'),
('legacy-source','fresh','Stale legacy title',9,1,'{"older":true}');`); err != nil {
		t.Fatal(err)
	}
	p := lifecycleProvider("legacy-source", "http://source.example.invalid")
	seedLive(t, ctx, db, p, lifecycleRecord("fresh", "Fresh collected title"))
	imported, err := db.ImportLegacy(ctx)
	if err != nil || imported != 1 {
		t.Fatalf("initial import: imported=%d err=%v", imported, err)
	}
	for _, row := range liveRows(t, ctx, db, p.ID) {
		switch row.SourceID {
		case "historical":
			if !row.Historical || row.RawID != nil || row.Fields["title"] != "Historical" || row.Fields["size"] != json.Number("0") {
				t.Fatalf("historical provenance or zero values lost: %+v", row)
			}
			if _, present := row.Fields["seeders"]; present {
				t.Error("Historical import retained a transient seeder count")
			}
			unknown, ok := row.Fields["unknown"].(map[string]any)
			if !ok {
				t.Fatalf("unknown legacy fields lost: %v", row.Fields)
			}
			if value, present := unknown["retained"]; !present || value != nil {
				t.Fatalf("legacy structured null lost: %v", unknown)
			}
		case "fresh":
			if row.Historical || row.RawID == nil || row.Fields["title"] != "Fresh collected title" {
				t.Fatalf("import overwrote collected data: %+v", row)
			}
		}
	}
	assertLiveIDs(t, ctx, db, p.ID, "fresh", "historical")
	if imported, err := db.ImportLegacy(ctx); err != nil || imported != 0 {
		t.Fatalf("repeat import is not idempotent: imported=%d err=%v", imported, err)
	}

	full := claimRun(t, ctx, db, model.ProviderDocument{Provider: p}, model.ModeFull)
	full = savePage(t, ctx, db, full, model.Page{Body: []byte("replacement full response"), Items: []model.Record{lifecycleRecord("replacement", "Replacement")}, Done: true}, "replacement")
	if _, err := db.FinishRun(ctx, full.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	assertLiveIDs(t, ctx, db, p.ID, "replacement")
	if _, err := fixture.Exec(ctx, `INSERT INTO public.torrents VALUES ('legacy-source','new-legacy','New historical row',12,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if imported, err := db.ImportLegacy(ctx); err != nil || imported != 1 {
		t.Fatalf("additive import after full: imported=%d err=%v", imported, err)
	}
	assertLiveIDs(t, ctx, db, p.ID, "new-legacy", "replacement")
	if imported, err := db.ImportLegacy(ctx); err != nil || imported != 0 {
		t.Fatalf("post-publication import resurrected rows: imported=%d err=%v", imported, err)
	}
	var legacyCount int
	if err := fixture.QueryRow(ctx, "SELECT count(*) FROM public.torrents").Scan(&legacyCount); err != nil || legacyCount != 3 {
		t.Fatalf("legacy source was modified: rows=%d err=%v", legacyCount, err)
	}
}

func lifecycleDatabase(t *testing.T) (context.Context, string, *store.Store) {
	t.Helper()
	ctx, databaseURL := testutil.NewDatabase(t)
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			t.Fatal("parse isolated database URL:", err)
		}
		query := parsed.Query()
		query.Set("pool_max_conns", "4")
		parsed.RawQuery = query.Encode()
		databaseURL = parsed.String()
	} else {
		databaseURL += " pool_max_conns=4"
	}
	db := openLifecycleStore(t, ctx, databaseURL)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, databaseURL, db
}

func withoutPoolOptions(t *testing.T, databaseURL string) string {
	t.Helper()
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		query := parsed.Query()
		query.Del("pool_max_conns")
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return strings.TrimSuffix(databaseURL, " pool_max_conns=4")
}

func openLifecycleStore(t *testing.T, ctx context.Context, databaseURL string) *store.Store {
	t.Helper()
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func lifecycleProvider(id, endpoint string) model.Provider {
	return model.Provider{
		Version: 1, ID: id, Name: id, Adapter: "http_json", URL: endpoint, Enabled: true,
		Auth: model.Auth{Type: "none"}, RequestInterval: "1ms", PageSize: 2,
		HTTP:       model.HTTPConfig{Method: "GET", ItemsPath: "/items"},
		Pagination: model.Pagination{Type: "offset", In: "query", OffsetParam: "offset", SizeParam: "limit", TotalPath: "/total"},
		Mapping:    model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title", "size": "/size", "seeders": "/seeders", "peers": "/peers"}},
		Output:     model.Output{Fields: []string{"title"}},
	}
}

func lifecycleRegistry(t *testing.T, p model.Provider) (*providers.Registry, model.ProviderDocument) {
	t.Helper()
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := registry.Save(p.ID, string(raw), "")
	if err != nil {
		t.Fatalf("save synthetic provider: %v (%v)", err, doc.Issues)
	}
	return registry, doc
}

func lifecycleManager(t *testing.T, ctx context.Context, db *store.Store, registry *providers.Registry, resolver model.SecretResolver, parallelism int) *jobs.Manager {
	t.Helper()
	manager := jobs.New(db, registry, resolver, parallelism)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeLifecycleManager(t, manager) })
	return manager
}

func closeLifecycleManager(t *testing.T, manager *jobs.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Error("close manager:", err)
	}
	select {
	case <-manager.Done():
	default:
		t.Error("manager shutdown did not finish")
	}
}

func awaitRun(t *testing.T, parent context.Context, db *store.Store, manager *jobs.Manager, id string, status model.RunStatus) model.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	notices, unsubscribe := manager.Subscribe()
	defer unsubscribe()
	for {
		run, err := db.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == status {
			return run
		}
		if run.Status != model.StatusRunning && run.Status != model.StatusQueued {
			t.Fatalf("run %s reached %s instead of %s: %s", id, run.Status, status, run.Error)
		}
		select {
		case _, ok := <-notices:
			if !ok {
				t.Fatal("manager stopped before run completed")
			}
		case <-ctx.Done():
			t.Fatalf("waiting for %s: last status=%s: %v", status, run.Status, ctx.Err())
		}
	}
}

func awaitSignal(t *testing.T, parent context.Context, signal <-chan struct{}, description string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("waiting for %s: %v", description, ctx.Err())
	}
}

func claimRun(t *testing.T, ctx context.Context, db *store.Store, doc model.ProviderDocument, mode model.RunMode) model.Run {
	t.Helper()
	created, err := db.CreateRun(ctx, model.Run{ProviderID: doc.Provider.ID, Config: doc.Provider, Revision: doc.Revision, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != created.ID {
		t.Fatalf("claim synthetic run: run=%v err=%v", claimed, err)
	}
	return *claimed
}

func lifecycleRecord(id, title string) model.Record {
	fields := map[string]any{"title": title}
	raw, err := json.Marshal(map[string]any{"id": id, "title": title})
	if err != nil {
		panic(err)
	}
	return model.Record{SourceID: id, Raw: raw, ContentType: "application/json", Fields: fields}
}

func savePage(t *testing.T, ctx context.Context, db *store.Store, run model.Run, page model.Page, fingerprint string) model.Run {
	t.Helper()
	saved, err := db.SavePage(ctx, run, page, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func seedLive(t *testing.T, ctx context.Context, db *store.Store, p model.Provider, records ...model.Record) {
	t.Helper()
	run := claimRun(t, ctx, db, model.ProviderDocument{Provider: p}, model.ModeIncremental)
	run = savePage(t, ctx, db, run, model.Page{Body: []byte("synthetic seed response"), Items: records, Done: true}, "seed")
	if _, err := db.FinishRun(ctx, run.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
}

func liveRows(t *testing.T, ctx context.Context, db *store.Store, providerID string) []model.Torrent {
	t.Helper()
	rows, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: providerID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rows.Total != int64(len(rows.Items)) {
		t.Fatalf("live fixture exceeds requested result window: total=%d rows=%d", rows.Total, len(rows.Items))
	}
	return rows.Items
}

func torrentIDs(rows []model.Torrent) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.SourceID
	}
	slices.Sort(ids)
	return ids
}

func assertLiveIDs(t *testing.T, ctx context.Context, db *store.Store, providerID string, expected ...string) {
	t.Helper()
	actual := torrentIDs(liveRows(t, ctx, db, providerID))
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		t.Fatalf("live provider %s: IDs=%v, want %v", providerID, actual, expected)
	}
}

func rawRows(t *testing.T, ctx context.Context, db *store.Store, runID string) []model.RawRecord {
	t.Helper()
	rows, err := db.ListRaw(ctx, model.ListOptions{RunID: runID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if rows.Total != int64(len(rows.Items)) {
		t.Fatalf("raw fixture exceeds requested result window: total=%d rows=%d", rows.Total, len(rows.Items))
	}
	for i := range rows.Items {
		rows.Items[i], err = db.Raw(ctx, rows.Items[i].ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	return rows.Items
}
