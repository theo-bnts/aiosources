package connectors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestWindowAtLeastCompleteRootUsesNativeClassifications(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("category") == "2" {
			windowList(w, 1, 1, windowItem(3, 2))
		} else {
			// The source's search index still counts the reclassified record.
			windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1), windowItem(3, 2))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.PageSize = 3
	p.Traversal.TotalMode, p.Traversal.MinimumTotal = "at_least", 3
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "other", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
	ledger := newWindowLedger()
	pages, err := windowRun(t, p, ledger, &requests)
	if err != nil || !pages[len(pages)-1].Done || requests.Load() != 2 {
		t.Fatalf("complete mixed-classification root did not finish: requests=%d err=%v", requests.Load(), err)
	}
	if !reflect.DeepEqual(ledger.scopes["selected"], map[string]bool{"1": true, "2": true}) || !reflect.DeepEqual(ledger.scopes["other"], map[string]bool{"3": true}) || len(ledger.ids) != 3 {
		t.Fatalf("reclassified record was counted in the wrong scope or discarded: %v", ledger.scopes)
	}
	if !reflect.DeepEqual(pages[len(pages)-1].Metadata["coverage_expected"], map[string]int64{"selected": 2, "other": 1}) || pages[0].Metadata["coverage_previous_target"] != int64(3) || pages[0].Metadata["coverage_target"] != int64(2) || len(pages[0].RefreshScopes) != 0 {
		t.Fatal("complete response did not retain an auditable target adjustment")
	}
}

func TestWindowAtLeastExistingNumericRecoveryRechecksSmallDeficit(t *testing.T) {
	for _, phase := range []string{"resolve", "recheck", "discover", "check"} {
		t.Run(phase, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/list" || r.URL.Query().Get("category") != "1" || r.URL.Query().Get("order") != "" {
					t.Errorf("small deficit caused numeric or unrelated collection: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1), windowItem(3, 2))
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.PageSize = 3
			p.Traversal.TotalMode, p.Traversal.MinimumTotal = "at_least", 3
			p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "other", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			ledger := newWindowLedger()
			ledger.commit(model.Page{Items: []model.Record{
				{SourceID: "1", CoverageScopes: []string{"selected"}},
				{SourceID: "2", CoverageScopes: []string{"selected"}},
				{SourceID: "3", CoverageScopes: []string{"other"}},
			}})
			checkpoint, err := json.Marshal(windowCursor{Version: 3, Phase: phase, Scope: 2, Page: 1, Ends: []int{}, Totals: []int64{3, 1}, Recovery: true, NextID: 3955, MaxID: 4000})
			if err != nil {
				t.Fatal(err)
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			if err != nil || !pages[len(pages)-1].Done || requests.Load() != 1 || len(ledger.ids) != 3 || len(ledger.scopes["selected"]) != 2 || len(ledger.scopes["other"]) != 1 {
				t.Fatalf("existing checkpoint lost progress or kept scanning IDs: scopes=%v requests=%d err=%v", ledger.scopes, requests.Load(), err)
			}
		})
	}
}

func TestWindowAtLeastIncompleteRootCannotLowerTarget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total int
		items []string
	}{
		{"short-response", 3, []string{windowItem(1, 1), windowItem(2, 1)}},
		{"duplicate-native-id", 3, []string{windowItem(1, 1), windowItem(1, 1), windowItem(2, 2)}},
		{"missing-classification", 3, []string{windowItem(1, 1), windowItem(2, 1), strings.Replace(windowItem(3, 2), `"category":2,`, "", 1)}},
		{"unselected-classification", 3, []string{windowItem(1, 1), windowItem(2, 1), windowItem(3, 9)}},
		{"invalid-record", 3, []string{windowItem(1, 1), windowItem(2, 1), strings.Replace(windowItem(3, 2), `"size":0`, `"size":"invalid"`, 1)}},
		{"inconsistent-total", 2, []string{windowItem(1, 1), windowItem(2, 1), windowItem(3, 2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				windowList(w, 1, tc.total, tc.items...)
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.PageSize, p.Traversal.TotalMode = 3, "at_least"
			checkpoint := json.RawMessage(`{"version":3,"phase":"root","page":1,"ends":[],"totals":[3]}`)
			pages, err := windowRunFrom(t, p, newWindowLedger(), &requests, checkpoint)
			if err == nil || pages[len(pages)-1].Done || pages[0].Metadata["coverage_target"] != nil || requests.Load() != 1 {
				t.Fatalf("unsafe response lowered its target or certified completion: %v %v", pages[len(pages)-1], err)
			}
		})
	}
}

func TestWindowStrictDoesNotAdoptCumulativeRootAdjustment(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("category") == "2" {
			windowList(w, 1, 1, windowItem(3, 2))
		} else {
			windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1), windowItem(3, 2))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.PageSize = 3
	p.Traversal.TotalMode = "strict"
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "other", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
	pages, err := windowRun(t, p, newWindowLedger(), &requests)
	if FailureCode(err) != "stalled" || pages[len(pages)-1].Done {
		t.Fatal("strict coverage accepted a mismatched classification total")
	}
	for _, page := range pages {
		if page.Metadata["coverage_target"] != nil {
			t.Fatal("cumulative target reconciliation changed strict coverage")
		}
	}
}
