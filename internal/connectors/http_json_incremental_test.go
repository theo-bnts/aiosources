package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func orderedFixture(endpoint string) model.Provider {
	p := windowFixture(endpoint)
	p.Schedule.KnownPages = 1
	p.Traversal.WindowPages = 3
	p.Traversal.IDRecovery = &model.JSONIDRecovery{
		First: 1, DiscoveryQuery: map[string]any{"sortBy": "id", "sortOrder": "desc"},
		ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f0-9]{40}$", DetailURL: "/detail/{value}", Mapping: p.Mapping,
	}
	return p
}

func baselineEnvironment(t *testing.T, ledger *windowLedger, prior map[string]bool) Environment {
	env := ledger.environment(t)
	env.KnownIDs = func(_ context.Context, ids []string) (map[string]bool, error) {
		known := make(map[string]bool, len(ids))
		for _, id := range ids {
			known[id] = prior[id]
		}
		return known, nil
	}
	return env
}

func TestOrderedIncrementalCollectsNewNativeIDWithKnownHash(t *testing.T) {
	var requested []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list" || r.URL.Query().Get("category") != "1" || r.URL.Query().Get("sortBy") != "id" || r.URL.Query().Get("sortOrder") != "desc" {
			t.Errorf("incremental left its ordered selected scope: %s", r.URL)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		requested = append(requested, page)
		switch page {
		case 1:
			windowList(w, page, 100, windowItem(249961, 1), windowItem(249960, 1))
		case 2:
			windowList(w, page, 100, windowItem(249627, 1), windowItem(249600, 1))
		default:
			t.Error("incremental crossed the prior-run boundary")
			windowList(w, page, 100)
		}
	}))
	defer server.Close()
	p := orderedFixture(server.URL)
	ledger := newWindowLedger()
	// A re-observation written in this run cannot satisfy the old-run boundary.
	ledger.ids["249961"], ledger.ids["249960"] = true, true
	prior := map[string]bool{"249627": true, "249600": true}
	var cursor json.RawMessage
	for i := range 2 {
		c, err := New(t.Context(), p, model.ModeIncremental, baselineEnvironment(t, ledger, prior))
		if err != nil {
			t.Fatal(err)
		}
		page, err := c.Fetch(t.Context(), cursor)
		_ = c.Close()
		if err != nil {
			t.Fatal(err)
		}
		ledger.commit(page)
		cursor = page.Next
		if page.Done != (i == 1) {
			t.Fatalf("wrong native boundary: %#v", page)
		}
	}
	if !reflect.DeepEqual(requested, []int{1, 2}) || !ledger.ids["249961"] || !ledger.ids["249627"] {
		t.Fatalf("same-hash native identities collapsed: pages=%v ids=%v", requested, ledger.ids)
	}
	if ledger.records[0].SourceID != "249961" || ledger.records[2].SourceID != "249627" || ledger.records[0].Fields["info_hash"] != ledger.records[2].Fields["info_hash"] {
		t.Fatal("fixture did not retain both native identities sharing the hash")
	}
}

func TestOrderedIncrementalDoesNotUseCurrentRunScopeAsBaseline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total := 2
		if r.URL.Query().Get("view") == "second" {
			total = 100
		}
		windowList(w, 1, total, windowItem(10, 1), windowItem(9, 1))
	}))
	defer server.Close()
	p := orderedFixture(server.URL)
	p.Traversal.WindowPages = 1
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "overlap", Query: map[string]any{"category": 1, "view": "second"}, Match: map[string]any{"category_id": 1}})
	ledger := newWindowLedger()
	c, err := New(t.Context(), p, model.ModeIncremental, baselineEnvironment(t, ledger, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first, err := c.Fetch(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ledger.commit(first)
	second, err := c.Fetch(t.Context(), first.Next)
	if FailureCode(err) != "stalled" || second.Done || string(second.Next) != string(first.Next) {
		t.Fatalf("current-run rows falsely completed overlapping scope: page=%#v err=%v", second, err)
	}
}

func TestOrderedIncrementalFailsClosedAtUnprovenWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ids   []int
		total int
	}{
		{"unordered", []int{4, 5}, 100},
		{"window_cap", []int{5, 4}, 100},
		{"short_before_total", []int{5}, 100},
		{"empty_before_total", nil, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var items []string
				for _, id := range tc.ids {
					items = append(items, windowItem(id, 1))
				}
				windowList(w, 1, tc.total, items...)
			}))
			defer server.Close()
			p := orderedFixture(server.URL)
			p.Traversal.WindowPages = 1
			c, err := New(t.Context(), p, model.ModeIncremental, baselineEnvironment(t, newWindowLedger(), nil))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			page, err := c.Fetch(t.Context(), nil)
			if FailureCode(err) != "stalled" || page.Done || len(page.Body) == 0 || len(page.Next) != 0 {
				t.Fatalf("unproven listing completed or lost response: page=%#v err=%v", page, err)
			}
		})
	}
}

func TestOrderedIncrementalShortPageCannotCertifyLaterNominalOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 1 {
			windowList(w, page, 3, windowItem(3, 1))
		} else {
			// Nominal page offset 2 plus this one record would falsely reach
			// total 3 even though source ID 2 has never been observed.
			windowList(w, page, 3, windowItem(1, 1))
		}
	}))
	t.Cleanup(server.Close)
	p := orderedFixture(server.URL)
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprintf("known_boundary=%t", known), func(t *testing.T) {
			env := baselineEnvironment(t, newWindowLedger(), map[string]bool{"3": known})
			page, err := fetchConnectorPage(t, p, model.ModeIncremental, env, nil)
			if known {
				if err != nil || !page.Done || page.Metadata["incremental_stop"] != "prior_run_boundary" {
					t.Fatalf("proved known-page boundary was rejected: page=%#v err=%v", page, err)
				}
			} else if FailureCode(err) != "stalled" || page.Done || len(page.Next) != 0 || len(page.Items) != 1 || len(page.Body) == 0 {
				t.Fatalf("short page allowed an unproved nominal offset: page=%#v err=%v", page, err)
			}
		})
	}
}

func TestOrderedIncrementalAllowsOnlyCommittedDescendingOverlap(t *testing.T) {
	for _, tc := range []struct {
		name string
		next []int
		pass bool
	}{
		{"committed_prefix", []int{9, 8}, true},
		{"unseen_prefix", []int{11, 8}, false},
		{"no_descent", []int{10, 9}, false},
		{"unordered_page", []int{8, 9}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				switch page {
				case 1:
					windowList(w, page, 100, windowItem(10, 1), windowItem(9, 1))
				case 2:
					windowList(w, page, 100, windowItem(tc.next[0], 1), windowItem(tc.next[1], 1))
				case 3:
					windowList(w, page, 100, windowItem(7, 1), windowItem(6, 1))
				default:
					t.Errorf("unexpected page: %d", page)
				}
			}))
			defer server.Close()
			p := orderedFixture(server.URL)
			ledger := newWindowLedger()
			env := baselineEnvironment(t, ledger, map[string]bool{"7": true, "6": true})
			first, err := fetchConnectorPage(t, p, model.ModeIncremental, env, nil)
			if err != nil {
				t.Fatal(err)
			}
			ledger.commit(first)
			second, err := fetchConnectorPage(t, p, model.ModeIncremental, env, first.Next)
			if !tc.pass {
				if FailureCode(err) != "stalled" || second.Done || string(second.Next) != string(first.Next) || len(second.Body) == 0 {
					t.Fatalf("invalid overlap advanced the frontier: page=%#v error=%v", second, err)
				}
				return
			}
			if err != nil || second.Done {
				t.Fatalf("ordinary insertion overlap was stalled or mistaken for old-run boundary: page=%#v error=%v", second, err)
			}
			ledger.commit(second)
			third, err := fetchConnectorPage(t, p, model.ModeIncremental, env, second.Next)
			if err != nil || !third.Done || third.Metadata["incremental_stop"] != "prior_run_boundary" {
				t.Fatalf("old-run native boundary was lost after overlap: page=%#v error=%v", third, err)
			}
		})
	}
}

func TestOrderedIncrementalEnforcesFirstTotalsVisibilityMinimum(t *testing.T) {
	for _, tc := range []struct {
		name    string
		totals  []int
		minimum int
		pass    bool
	}{
		{"hidden_empty", []int{0}, 1, false},
		{"multi_scope_below_minimum", []int{3, 4}, 8, false},
		{"multi_scope_meets_minimum", []int{3, 4}, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				scope, _ := strconv.Atoi(r.URL.Query().Get("scope"))
				if tc.totals[scope] == 0 {
					windowList(w, 1, 0)
				} else {
					windowList(w, 1, tc.totals[scope], windowItem(2, 1), windowItem(1, 1))
				}
			}))
			defer server.Close()
			p := orderedFixture(server.URL)
			p.Traversal.MinimumTotal = tc.minimum
			p.Traversal.Scopes = nil
			for index := range tc.totals {
				p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "scope" + strconv.Itoa(index), Query: map[string]any{"scope": index}, Match: map[string]any{"category_id": 1}})
			}
			env := baselineEnvironment(t, newWindowLedger(), map[string]bool{"1": true, "2": true})
			var cursor json.RawMessage
			for index := range tc.totals {
				page, err := fetchConnectorPage(t, p, model.ModeIncremental, env, cursor)
				final := index == len(tc.totals)-1
				if final && !tc.pass {
					if FailureCode(err) != "stalled" || page.Done || page.Metadata["coverage_incomplete"] != true || string(page.Next) != string(cursor) {
						t.Fatalf("restricted source advertised success: page=%#v error=%v", page, err)
					}
				} else if err != nil || page.Done != final {
					t.Fatalf("first totals did not accumulate across constructor restart: page=%#v error=%v", page, err)
				}
				cursor = page.Next
			}
		})
	}
}

func publicationItem(id int, date string) string {
	return fmt.Sprintf(`{"id":%d,"category":1,"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","date":%q}`, id, date)
}

func TestOrderedIncrementalUsesPublicationOrderAcrossResumes(t *testing.T) {
	listings := [][]string{
		{publicationItem(249981, "2026-09-25T12:32:30Z"), publicationItem(249745, "2026-09-25T12:30:30Z")},
		{publicationItem(249745, "2026-09-25T12:30:30Z"), publicationItem(250297, "2026-09-25T12:12:09Z")},
		{publicationItem(249477, "2026-09-25T12:12:09Z"), publicationItem(250101, "2026-09-25T09:15:36Z")},
		{publicationItem(249859, "2026-09-25T08:00:00Z"), publicationItem(249858, "2026-09-25T07:00:00Z")},
	}
	var requested []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		requested = append(requested, page)
		if page < 1 || page > len(listings) {
			t.Errorf("crossed the known publication boundary: %d", page)
			http.Error(w, "unexpected page", http.StatusBadRequest)
			return
		}
		windowList(w, page, 100, listings[page-1]...)
	}))
	defer server.Close()
	p := orderedFixture(server.URL)
	p.Traversal.WindowPages = len(listings)
	p.Traversal.IncrementalOrder = "published_at"
	p.Traversal.IDRecovery.DiscoveryQuery["sortBy"] = "createdAt"
	p.Mapping.Fields["published_at"] = "/date"
	prior := map[string]bool{"250101": true, "249859": true, "249858": true}
	ledger := newWindowLedger()
	var cursor json.RawMessage
	for i := range listings {
		c, err := New(t.Context(), p, model.ModeIncremental, baselineEnvironment(t, ledger, prior))
		if err != nil {
			t.Fatal(err)
		}
		page, err := c.Fetch(t.Context(), cursor)
		_ = c.Close()
		if err != nil || page.Done != (i == len(listings)-1) {
			t.Fatalf("chronological publication boundary lost at page %d: done=%v error=%v", i+1, page.Done, err)
		}
		ledger.commit(page)
		cursor = page.Next
	}
	wantIDs := map[string]bool{"249981": true, "249745": true, "250297": true, "249477": true, "250101": true, "249859": true, "249858": true}
	if !reflect.DeepEqual(requested, []int{1, 2, 3, 4}) || !reflect.DeepEqual(ledger.ids, wantIDs) {
		t.Fatalf("reordered native IDs, timestamp ties or same-hash identities were lost: pages=%v ids=%v", requested, ledger.ids)
	}
}

func TestPublicationIncrementalUsesIdentityBoundaryDespiteDateReordering(t *testing.T) {
	listings := [][]string{
		{publicationItem(10, "2026-09-25T10:00:00Z"), publicationItem(9, "2026-09-25T11:00:00Z")},
		{publicationItem(8, "2026-09-25T14:00:00Z"), publicationItem(7, "2026-09-25T08:00:00Z")},
		{publicationItem(6, "2026-09-25T13:00:00Z"), publicationItem(5, "2026-09-25T07:00:00Z")},
		{publicationItem(4, "2026-09-25T04:00:00Z"), publicationItem(3, "2026-09-25T05:00:00Z")},
	}
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_resume=%t", resume), func(t *testing.T) {
			var requested []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				requested = append(requested, page)
				if page < 1 || page > len(listings) {
					t.Errorf("missed the identity boundary: page %d", page)
					http.Error(w, "unexpected page", http.StatusBadRequest)
					return
				}
				windowList(w, page, 100, listings[page-1]...)
			}))
			defer server.Close()
			p := orderedFixture(server.URL)
			p.Schedule.KnownPages = 2
			p.Traversal.WindowPages = len(listings)
			p.Traversal.IncrementalOrder = "published_at"
			p.Traversal.IDRecovery.DiscoveryQuery["sortBy"] = "createdAt"
			p.Mapping.Fields["published_at"] = "/date"
			prior := map[string]bool{"10": true, "9": true, "7": true, "6": true, "5": true, "4": true, "3": true}
			ledger := newWindowLedger()
			env := baselineEnvironment(t, ledger, prior)
			first := 1
			var cursor json.RawMessage
			if resume {
				first = 2
				ledger.ids["10"], ledger.ids["9"] = true, true
				cursor = json.RawMessage(`{"version":1,"phase":"incremental","scope":0,"page":2,"known_pages":1,"last_id":9,"last_published_at":"2026-09-25T11:00:00Z","minimum_remaining":0}`)
			}
			for pageNumber := first; pageNumber <= len(listings); pageNumber++ {
				// Reconstruct on every page: the boundary and migration must
				// survive process restarts, not just one connector's memory.
				page, err := fetchConnectorPage(t, p, model.ModeIncremental, env, cursor)
				if err != nil || page.Done != (pageNumber == len(listings)) {
					t.Fatalf("date reordering lost the identity boundary at page %d: done=%v error=%v", pageNumber, page.Done, err)
				}
				ledger.commit(page)
				cursor = page.Next
			}
			wantRequests := []int{1, 2, 3, 4}
			if resume {
				wantRequests = wantRequests[1:]
			}
			wantIDs := map[string]bool{"10": true, "9": true, "8": true, "7": true, "6": true, "5": true, "4": true, "3": true}
			if !reflect.DeepEqual(requested, wantRequests) || !reflect.DeepEqual(ledger.ids, wantIDs) {
				t.Fatalf("late publication, native identity or resume position lost: pages=%v ids=%v", requested, ledger.ids)
			}
		})
	}
}

func TestPublicationIncrementalRejectsInvalidRecords(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []string
	}{
		{"missing_date", []string{publicationItem(4, ""), publicationItem(3, "2026-09-25T11:00:00Z")}},
		{"duplicate_identity", []string{publicationItem(4, "2026-09-25T12:00:00Z"), publicationItem(4, "2026-09-25T11:00:00Z")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				windowList(w, 1, 100, tc.items...)
			}))
			defer server.Close()
			p := orderedFixture(server.URL)
			p.Traversal.IncrementalOrder = "published_at"
			p.Mapping.Fields["published_at"] = "/date"
			page, err := fetchConnectorPage(t, p, model.ModeIncremental, baselineEnvironment(t, newWindowLedger(), nil), nil)
			if err == nil || page.Done || len(page.Next) != 0 {
				t.Fatalf("invalid records advanced the checkpoint: page=%#v error=%v", page, err)
			}
		})
	}
}

func TestOrderedIncrementalKnownPageStreakResetsAndIsPerScope(t *testing.T) {
	firstScope := [][]int{{12, 11}, {10, 9}, {8, 7}, {6, 5}}
	secondScope := [][]int{{4, 3}, {2, 1}}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		scope := r.URL.Query().Get("view")
		requests = append(requests, fmt.Sprintf("%s:%d", scope, page))
		if r.URL.Path != "/list" || r.URL.Query().Get("sortBy") != "createdAt" {
			t.Errorf("incremental left the publication listing: %s", r.URL)
		}
		listing := firstScope
		if scope == "second" {
			listing = secondScope
		}
		if page < 1 || page > len(listing) {
			t.Errorf("crossed the per-scope known-page boundary: %s page %d", scope, page)
			http.Error(w, "unexpected page", http.StatusBadRequest)
			return
		}
		var items []string
		for _, id := range listing[page-1] {
			items = append(items, publicationItem(id, fmt.Sprintf("2026-09-25T%02d:00:00Z", id)))
		}
		windowList(w, page, 100, items...)
	}))
	defer server.Close()
	p := metadataFixture(server.URL)
	p.Schedule.KnownPages = 2
	p.Traversal.WindowPages = 4
	p.Traversal.IncrementalOrder = "published_at"
	p.Traversal.IDRecovery.DiscoveryQuery["sortBy"] = "createdAt"
	p.Mapping.Fields["published_at"] = "/date"
	p.Traversal.Scopes[0].Query["view"] = "first"
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "other", Query: map[string]any{"category": 1, "view": "second"}, Match: map[string]any{"category_id": 1}})
	prior := make(map[string]bool)
	for id := 1; id <= 12; id++ {
		prior[strconv.Itoa(id)] = id != 9
	}
	ledger := newWindowLedger()
	env := baselineEnvironment(t, ledger, prior)
	var cursor json.RawMessage
	for step := range 6 {
		page, err := fetchConnectorPage(t, p, model.ModeIncremental, env, cursor)
		if err != nil || page.Done != (step == 5) {
			t.Fatalf("known-page streak lost across reset/resume: step=%d page=%#v error=%v", step, page, err)
		}
		ledger.commit(page)
		cursor = page.Next
	}
	want := []string{"first:1", "first:2", "first:3", "first:4", "second:1", "second:2"}
	if !reflect.DeepEqual(requests, want) || !ledger.ids["9"] {
		t.Fatalf("incremental failed X=2 reset or scope isolation: requests=%v ids=%v", requests, ledger.ids)
	}
}
