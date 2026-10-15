package jobs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestSourceFailureMessagesRemainSafeAndReadable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{"unknown response", map[string]any{"failure_reason": "credential-secret"}, "Source response could not be processed"},
		{"known reason", map[string]any{"failure_reason": "position_mismatch"}, "Source returned a different pagination position than requested; checkpoint unchanged; review run events for details"},
		{"coverage", map[string]any{"coverage_incomplete": true}, coverageFailureMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := sourceFailureMessage(model.Page{Error: "credential-secret", Metadata: tc.metadata})
			if message != tc.want || strings.Contains(message, "credential-secret") {
				t.Fatalf("unsafe or inconsistent source diagnostic: %q", message)
			}
		})
	}
}

func TestPaginationDiagnosticsSurviveRetriesAndSuccess(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var corrected atomic.Bool
	var firstRequests, rejectedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Secret", "credential-secret")
		switch r.URL.Query().Get("offset") {
		case "0":
			firstRequests.Add(1)
			_, _ = fmt.Fprint(w, `{"items":[{"id":"first","title":"First"}],"total":2}`)
		case "1":
			rejectedRequests.Add(1)
			total := 3
			if corrected.Load() {
				total = 2
			}
			_, _ = fmt.Fprintf(w, `{"items":[{"id":"second","title":"Second"}],"total":%d,"upstream_error":"credential-secret"}`, total)
		default:
			http.Error(w, "unexpected offset", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	provider := schedulerProvider("pagination-diagnostics", server.URL)
	provider.Schedule = model.Schedule{}
	saveScheduledProvider(t, registry, provider, "")
	manager := New(db, registry, nil, 1)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: provider.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	var checkpoint string
	for attempt := 1; attempt <= 2; attempt++ {
		run = collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusFailed)
		if run.Error == "" || run.Errors != attempt || run.Pages != 1 || run.TraversalDone {
			t.Fatalf("failure lost actionable diagnosis or accepted rejected page: %+v", run)
		}
		if attempt == 1 {
			checkpoint = string(run.Cursor)
		} else if string(run.Cursor) != checkpoint {
			t.Fatalf("rejected retry advanced checkpoint: %s -> %s", checkpoint, run.Cursor)
		}
		if _, err := manager.Resume(ctx, run.ID); err != nil {
			t.Fatal(err)
		}
	}
	corrected.Store(true)
	run = collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
	if run.Error != "" || run.Errors != 2 || run.DistinctRecords != 2 || firstRequests.Load() != 1 || rejectedRequests.Load() != 3 {
		t.Fatalf("success confused historical errors or replayed accepted work: %+v requests=%d/%d", run, firstRequests.Load(), rejectedRequests.Load())
	}
	events, err := db.Events(ctx, run.ID, model.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, event := range events.Items {
		if event.Kind != "page_error" {
			continue
		}
		failures++
		if event.Data["failure_reason"] != "total_changed" || event.Data["failure_code"] != "stalled" || event.Data["expected_total"] != json.Number("2") || event.Data["actual_total"] != json.Number("3") || event.Data["offset"] != json.Number("1") || event.Data["http_status"] != json.Number("200") || event.Data["payload_retained"] != false {
			t.Fatalf("retained failure lost diagnostic context: %+v", event)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "credential-secret") || strings.Contains(string(encoded), server.URL) {
			t.Fatalf("private response data escaped event: %s", encoded)
		}
	}
	if failures != 2 {
		t.Fatalf("success erased historical attempts: %d", failures)
	}
}

func TestBodylessPaginationFailureUsesSafeNumericEvidence(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	run, err := db.CreateRun(ctx, model.Run{ProviderID: "bodyless-diagnostic", Config: model.Provider{ID: "bodyless-diagnostic"}, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, nil, nil, 1)
	page := model.Page{Metadata: map[string]any{"failure_reason": "position_mismatch", "expected_position": 3, "actual_position": 8, "http_status": 200, "upstream_error": "credential-secret"}}
	if err := manager.archiveFailure(ctx, run, page, "credential-secret", "stalled"); err != nil {
		t.Fatal(err)
	}
	events, err := db.Events(ctx, run.ID, model.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events.Items {
		if event.Kind == "source_error" {
			if event.Data["expected_position"] != json.Number("3") || event.Data["actual_position"] != json.Number("8") || event.Data["failure_reason"] != "position_mismatch" || strings.Contains(event.Message, "credential-secret") || event.Data["upstream_error"] != nil {
				t.Fatalf("bodyless rejection lost safe evidence: %+v", event)
			}
			return
		}
	}
	t.Fatal("bodyless rejection was not retained")
}
