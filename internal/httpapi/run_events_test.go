package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestRunEventPagesShowRecentEventsAndKeepTotalsDuringCollection(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "paged", Config: model.Provider{ID: "paged"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateRun(ctx, model.Run{ProviderID: "other", Config: model.Provider{ID: "other"}, Mode: model.ModePreview})
	if err != nil {
		t.Fatal(err)
	}
	// Each run starts with its queued event. Interleaving another run gives
	// the requested run noncontiguous IDs without changing its own total.
	for sequence := 1; sequence < 100; sequence++ {
		if _, err := db.AddEvent(ctx, run.ID, "page_saved", fmt.Sprintf("event-%d", sequence), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AddEvent(ctx, other.ID, "page_saved", "another collection", nil); err != nil {
			t.Fatal(err)
		}
	}
	srv := &server{options: Options{Store: db}}
	readPage := func(offset int) model.List[model.Event] {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/runs/%s/events?limit=50&offset=%d", run.ID, offset), nil).WithContext(ctx)
		r.SetPathValue("id", run.ID)
		w := httptest.NewRecorder()
		srv.runEvents(w, r)
		var result model.List[model.Event]
		if w.Code != http.StatusOK {
			t.Fatalf("event page: status %d, %s", w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := readPage(0)
	second := readPage(50)
	for pageIndex, result := range []model.List[model.Event]{first, second} {
		if result.Total != 100 || len(result.Items) != 50 {
			t.Fatalf("page %d must contain 50 of this run's 100 events: %+v", pageIndex+1, result)
		}
		for index, event := range result.Items {
			sequence := 99 - pageIndex*50 - index
			if event.RunID != run.ID || (sequence == 0 && event.Kind != "queued") || (sequence > 0 && event.Message != fmt.Sprintf("event-%d", sequence)) {
				t.Fatalf("event %d missing, duplicated or out of order: %+v", sequence, event)
			}
		}
	}
	pastEnd := readPage(100)
	if pastEnd.Total != 100 || len(pastEnd.Items) != 0 {
		t.Fatalf("an exact page boundary must retain its total without phantom rows: %+v", pastEnd)
	}
	if _, err := db.AddEvent(ctx, run.ID, "page_saved", "event-100", nil); err != nil {
		t.Fatal(err)
	}
	last := readPage(100)
	if last.Total != 101 || len(last.Items) != 1 || last.Items[0].Kind != "queued" {
		t.Fatalf("new events must extend the total without losing the earliest event: %+v", last)
	}
	refreshed := readPage(0)
	if refreshed.Total != 101 || refreshed.Items[0].Message != "event-100" || !reflect.DeepEqual(refreshed.Items[1:], first.Items[:49]) {
		t.Fatalf("live updates must put the newest event first and preserve the existing order: %+v", refreshed)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/runs/missing/events?limit=50&offset=0", nil).WithContext(ctx)
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	srv.runEvents(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing collection must not look like an empty page: status %d, %s", w.Code, w.Body.String())
	}
}

func TestEventStreamHeadFinishesWithoutHoldingStreamAdmission(t *testing.T) {
	srv := &server{options: Options{Jobs: jobs.New(nil, nil, nil, 1)}, streams: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodHead, "/api/events", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.events(response, request)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("HEAD kept a live event stream open")
	}
	if response.Code != http.StatusOK || response.Body.Len() != 0 || response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("HEAD did not return empty event-stream headers: status %d", response.Code)
	}
	// A metadata-only request does not compete with real subscriptions.
	srv.streams <- struct{}{}
	response = httptest.NewRecorder()
	srv.events(response, request)
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD was rejected by full streaming capacity: status %d", response.Code)
	}
}

func TestRunEventsPublishOnlyFinitePaginationDiagnostics(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "diagnostics", Config: model.Provider{ID: "diagnostics"}, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []map[string]any{
		{"failure_reason": "total_changed", "expected_total": 3, "actual_total": 4, "http_status": 200, "offset": 1, "payload_retained": false},
		{"failure_reason": "credential-secret", "expected_total": "credential-secret", "actual_total": json.Number("9007199254740993"), "expected_position": -1, "actual_position": 1.5, "http_status": 999, "offset": "credential-secret"},
	} {
		if _, err := db.AddEvent(ctx, run.ID, "page_error", "Response rejected", data); err != nil {
			t.Fatal(err)
		}
	}
	srv := &server{options: Options{Store: db}}
	request := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID+"/events", nil).WithContext(ctx)
	request.SetPathValue("id", run.ID)
	response := httptest.NewRecorder()
	srv.runEvents(response, request)
	var result model.List[model.Event]
	if response.Code != http.StatusOK {
		t.Fatalf("read diagnostics: %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("event history changed: %+v", result)
	}
	want := map[string]any{"failure_reason": "total_changed", "expected_total": float64(3), "actual_total": float64(4), "http_status": float64(200), "offset": float64(1), "payload_retained": false}
	if !reflect.DeepEqual(result.Items[1].Data, want) || len(result.Items[0].Data) != 0 {
		t.Fatalf("public diagnostic boundary lost safe values or leaked unsafe ones: %+v", result.Items)
	}
	// Filtering public diagnostics must not rewrite retained history.
	stored, err := db.Events(ctx, run.ID, model.ListOptions{})
	if err != nil || stored.Items[0].Data["failure_reason"] != "credential-secret" {
		t.Fatalf("public filtering mutated historical data: %+v %v", stored, err)
	}
}
