package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

type windowLedger struct {
	scopes  map[string]map[string]bool
	ids     map[string]bool
	pending map[string]bool
	records []model.Record
	pages   []model.Page
}

func newWindowLedger() *windowLedger {
	return &windowLedger{scopes: make(map[string]map[string]bool), ids: make(map[string]bool), pending: make(map[string]bool)}
}

func (l *windowLedger) environment(t *testing.T) Environment {
	t.Helper()
	return Environment{
		ScopeCounts: func(context.Context) (map[string]int64, error) {
			counts := make(map[string]int64)
			for scope, ids := range l.scopes {
				counts[scope] = int64(len(ids))
			}
			return counts, nil
		},
		ObservedIDs: func(_ context.Context, ids []string) (map[string]bool, error) {
			if len(ids) > 100 {
				t.Errorf("unbounded membership lookup: %d", len(ids))
			}
			found := make(map[string]bool, len(ids))
			for _, id := range ids {
				found[id] = l.ids[id] && !l.pending[id]
			}
			return found, nil
		},
		FilterComplete: func(_ context.Context, scope string, fingerprints []string) (bool, error) {
			selected := make(map[string]bool, len(fingerprints))
			for _, fingerprint := range fingerprints {
				selected[fingerprint] = true
			}
			total := int64(-1)
			ids := make(map[string]bool)
			for _, page := range l.pages {
				fingerprint, _ := page.Metadata["fingerprint_scope"].(string)
				if page.Error != "" || page.Metadata["traversal_phase"] != "options_list" || !selected[fingerprint] {
					continue
				}
				count, ok := page.Metadata["total"].(int64)
				if !ok || count < 0 || (total >= 0 && count != total) {
					return false, nil
				}
				total = count
				for _, record := range page.Items {
					if record.Error == "" && !record.Ignored && !record.Auxiliary && l.scopes[scope][record.SourceID] && !l.pending[record.SourceID] {
						ids[record.SourceID] = true
					}
				}
			}
			return total >= 0 && int64(len(ids)) == total, nil
		},
		NextRefreshID: func(_ context.Context, after, through int64) (int64, error) {
			var next int64
			for id := range l.pending {
				n, err := strconv.ParseInt(id, 10, 64)
				if err == nil && strconv.FormatInt(n, 10) == id && n > after && n <= through && (next == 0 || n < next) {
					next = n
				}
			}
			return next, nil
		},
	}
}

func (l *windowLedger) commit(page model.Page) {
	l.pages = append(l.pages, page)
	l.records = append(l.records, page.Items...)
	if page.Error != "" {
		return
	}
	for _, scope := range page.RefreshScopes {
		for id := range l.scopes[scope] {
			l.pending[id] = true
		}
		delete(l.scopes, scope)
	}
	if len(page.RefreshScopes) > 0 {
		l.ids = make(map[string]bool)
		for _, ids := range l.scopes {
			for id := range ids {
				l.ids[id] = true
			}
		}
	}
	for _, record := range page.Items {
		if record.Error != "" || record.Auxiliary || record.SourceID == "" || (record.Ignored && (record.CoverageScopes == nil || len(record.CoverageScopes) > 0)) {
			continue
		}
		if record.Ignored {
			delete(l.ids, record.SourceID)
		} else {
			l.ids[record.SourceID] = true
		}
		if record.CoverageScopes == nil {
			continue
		}
		delete(l.pending, record.SourceID)
		for _, ids := range l.scopes {
			delete(ids, record.SourceID)
		}
		for _, scope := range record.CoverageScopes {
			if l.scopes[scope] == nil {
				l.scopes[scope] = make(map[string]bool)
			}
			l.scopes[scope][record.SourceID] = true
		}
	}
}

func windowFixture(endpoint string) model.Provider {
	p := protocolProvider("http_json", endpoint+"/list")
	p.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "limit", Start: 1, CurrentPath: "/page"}
	p.HTTP.ItemsPath = "/data"
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"category_id": "/category", "title": "/name", "size": "/size", "info_hash": "/hash"}}
	p.Traversal = &model.JSONTraversal{WindowPages: 1, TotalPaths: []string{"/total", "/fallback"}, Scopes: []model.JSONScope{{ID: "selected", Query: map[string]any{"category": 1}, Match: map[string]any{"category_id": 1}}}}
	return p
}

func windowItem(id, category int) string {
	return fmt.Sprintf(`{ "id":%d, "category":%d, "name":"item", "size":0, "hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "vendor":1.2300e+2 }`, id, category)
}

func windowList(w http.ResponseWriter, page, total int, items ...string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, "{\n\"page\":%d,\"total\":%d,\"data\":[%s]}\n", page, total, strings.Join(items, ",\n"))
}

// Reopening after every committed page tests resumability at every network
// boundary, including between HEAD and detail and after control-only pages.
func windowRun(t *testing.T, p model.Provider, ledger *windowLedger, requests *atomic.Int64) ([]model.Page, error) {
	return windowRunFrom(t, p, ledger, requests, nil)
}

func windowRunFrom(t *testing.T, p model.Provider, ledger *windowLedger, requests *atomic.Int64, checkpoint json.RawMessage) ([]model.Page, error) {
	t.Helper()
	var pages []model.Page
	for range 120 {
		c, err := New(t.Context(), p, model.ModeIncremental, ledger.environment(t))
		if err != nil {
			t.Fatal(err)
		}
		before := requests.Load()
		page, fetchErr := c.Fetch(t.Context(), checkpoint)
		_ = c.Close()
		networkSteps := requests.Load() - before
		if networkSteps > 1 || (networkSteps == 0 && len(page.Body) != 0) {
			t.Fatalf("Fetch hid or invented responses: requests=%d body=%q", networkSteps, page.Body)
		}
		if page.Metadata["known_pages_managed"] != true || page.RequireUniqueIDs {
			t.Fatalf("overlapping traversal used global known-page or unique-ID stopping: %#v", page)
		}
		pages = append(pages, page)
		ledger.commit(page)
		if fetchErr != nil {
			if string(page.Next) != string(checkpoint) {
				t.Fatal("failed response advanced the committed checkpoint")
			}
			return pages, fetchErr
		}
		if len(page.Next) > 4096 {
			t.Fatalf("checkpoint grew with observed IDs: %d bytes", len(page.Next))
		}
		checkpoint = page.Next
		if page.Done {
			return pages, nil
		}
	}
	t.Fatal("bounded fixture never reached completion or explicit failure")
	return nil, nil
}

func TestWindowPartitionsOverlapAndFreshRootCoverage(t *testing.T) {
	var requests atomic.Int64
	var rootFirstPages atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if bucket := r.URL.Query().Get("bucket"); bucket != "" {
			switch bucket {
			case "1":
				windowList(w, page, 2, windowItem(1, 1), windowItem(2, 1))
			case "2":
				windowList(w, page, 2, windowItem(3, 1), windowItem(4, 1))
			default:
				t.Error("already covered partition was requested")
				windowList(w, page, 2, windowItem(5, 1), windowItem(6, 1))
			}
			return
		}
		if page == 1 {
			if rootFirstPages.Add(1) > 2 {
				windowList(w, page, 7, windowItem(7, 1), windowItem(6, 1))
			} else {
				windowList(w, page, 6, windowItem(6, 1), windowItem(5, 1))
			}
			return
		}
		// A live insert changes the total and shifts one ID onto both pages.
		windowList(w, page, 7, windowItem(5, 1), windowItem(4, 1))
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.WindowPages = 2
	p.Schedule.KnownPages = 1
	end := 3
	p.Traversal.Partitions = []model.JSONPartition{{Parameter: "bucket", Start: 1, End: &end}}
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "overlap", Query: map[string]any{"category": 1, "view": "second"}, Match: map[string]any{"category_id": "1"}})
	ledger := newWindowLedger()
	pages, err := windowRun(t, p, ledger, &requests)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.scopes["selected"]) != 7 || len(ledger.scopes["overlap"]) != 7 || rootFirstPages.Load() != 4 {
		t.Fatalf("harmless growth discarded accepted coverage: %#v, roots=%d", ledger.scopes, rootFirstPages.Load())
	}
	for _, page := range pages {
		if len(page.RefreshScopes) > 0 {
			t.Fatalf("growth alone reset source identities: %v", page.RefreshScopes)
		}
	}
	if pages[0].Done || pages[1].Done || !pages[len(pages)-1].Done {
		t.Fatal("a capped query was mistaken for completion")
	}
	if pages[0].Metadata["fingerprint_scope"] != pages[1].Metadata["fingerprint_scope"] || pages[1].Metadata["fingerprint_scope"] == pages[2].Metadata["fingerprint_scope"] {
		t.Fatal("fingerprints do not separate complete queries from pages of one query")
	}
	for _, record := range ledger.records {
		id, _ := strconv.Atoi(record.SourceID)
		if string(record.Raw) != windowItem(id, 1) {
			t.Fatalf("original item bytes changed: %q", record.Raw)
		}
	}
}

func TestWindowOptionsNumericRecoveryAndActualIdentity(t *testing.T) {
	for _, detailID := range []int{7, 8} {
		t.Run(fmt.Sprintf("detail-ID-%d", detailID), func(t *testing.T) {
			var requests atomic.Int64
			var mu sync.Mutex
			var heads []string
			var optionFilters []string
			var baseOptions map[string]bool
			var details int
			const options = `{ "groups":[null,{"required":true,"type":"text"},{"values":null},{"required":false,"values":[{"id":"later"}]},{"required":true,"values":[null,{"label":"heading"},{"id":null},{"id":"first"},{"id":"first"}]}] }`
			detailItem := fmt.Sprintf(`{ "id":%d, "metadata":{"category":1}, "name":"detail", "size":0, "hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "untouched":1e+03 }`, detailID)
			detailBody := "{\n\"result\":" + detailItem + "\n}\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				requests.Add(1)
				switch {
				case r.URL.Path == "/list":
					query := r.URL.Query()
					if query.Get("order") == "id-desc" {
						if query.Get("category") != "" || query.Get("base") != "yes" {
							t.Error("numeric discovery did not use the global base query")
						}
						windowList(w, 1, 9, windowItem(9, 2), windowItem(8, 1))
					} else if query.Get("option") != "" {
						optionFilters = append(optionFilters, query.Get("option"))
						option := query.Get("option")
						if option == "later" && !baseOptions["first"] {
							t.Error("optional filter was visited before the required group")
						}
						if query.Get("order") == "" {
							if baseOptions[option] {
								http.Error(w, "duplicate option consumed another query", http.StatusTooManyRequests)
								return
							}
							baseOptions[option] = true
						}
						if query.Get("option") == "first" {
							windowList(w, 1, 1, windowItem(4, 1))
						} else {
							windowList(w, 1, 1, `{ "id":12, "category":1, "size":"bad" }`)
						}
					} else {
						windowList(w, 1, 5, windowItem(1, 1), windowItem(3, 1))
					}
				case r.URL.Path == "/options/1":
					baseOptions = make(map[string]bool)
					_, _ = io.WriteString(w, options)
				case strings.HasPrefix(r.URL.Path, "/resolve/"):
					if r.Method != http.MethodHead {
						t.Error("resolver request was not HEAD")
					}
					id := strings.TrimPrefix(r.URL.Path, "/resolve/")
					heads = append(heads, id)
					switch id {
					case "2", "9":
						w.WriteHeader(http.StatusNotFound)
					case "5":
						w.WriteHeader(http.StatusGone)
					case "6":
						w.WriteHeader(http.StatusNoContent)
					case "7":
						w.Header().Set("Location", "/torrent/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
						w.WriteHeader(http.StatusFound)
					case "10":
						w.WriteHeader(http.StatusServiceUnavailable)
					default:
						t.Errorf("already committed ID was resolved: %s", id)
						w.WriteHeader(http.StatusNotFound)
					}
				case strings.HasPrefix(r.URL.Path, "/detail/"):
					details++
					_, _ = io.WriteString(w, detailBody)
				default:
					t.Errorf("unexpected request path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.HTTP.Query = map[string]any{"base": "yes"}
			p.Traversal.QueryVariants = []map[string]any{{"order": "reverse"}}
			p.Traversal.Options = &model.JSONOptions{URL: "/options/{category}", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option", PriorityPath: "/required"}
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "id-desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f0-9]{40}$", DetailURL: "/detail/{value}", DetailPath: "/result", Mapping: model.Mapping{ID: "/id", Fields: map[string]string{"category_id": "/metadata/category", "title": "/name", "size": "/size", "info_hash": "/hash"}}}
			ledger := newWindowLedger()
			pages, err := windowRun(t, p, ledger, &requests)
			if detailID == 7 && err != nil {
				t.Fatal(err)
			}
			last := pages[len(pages)-1]
			if detailID == 8 {
				if FailureCode(err) != "http" || last.Done || ledger.ids["7"] || len(ledger.scopes["selected"]) != 4 {
					t.Fatalf("resolver input was fabricated into a source identity or missing coverage was certified: %#v, %v", last, err)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			wantHeads := []string{"2", "5", "6", "7"}
			if detailID == 8 {
				wantHeads = append(wantHeads, "9", "10")
			}
			if !reflect.DeepEqual(heads, wantHeads) || details != 1 {
				t.Fatalf("resumed recovery repeated, skipped, or reordered requests: heads=%v options=%v details=%d", heads, optionFilters, details)
			}
			foundOptions, foundDetail, foundMalformed, foundIgnored := false, false, false, false
			for _, page := range pages {
				if page.Metadata["traversal_phase"] == "options" {
					foundOptions = string(page.Body) == options
				}
				if page.Metadata["traversal_phase"] == "detail" {
					foundDetail = string(page.Body) == detailBody && len(page.Items) == 1 && string(page.Items[0].Raw) == detailItem && page.Items[0].SourceID == strconv.Itoa(detailID)
				}
				for _, record := range page.Items {
					if record.SourceID == "12" {
						foundMalformed = record.Error != "" && len(record.CoverageScopes) == 0
					}
					if record.SourceID == "9" {
						foundIgnored = record.Ignored && len(record.CoverageScopes) == 0
					}
				}
			}
			if !foundOptions || !foundDetail || !foundMalformed || !foundIgnored || ledger.ids["12"] || ledger.ids["9"] {
				t.Fatal("response/item bytes or excluded/malformed record semantics were lost")
			}
			if detailID == 7 && (!ledger.ids["7"] || !ledger.ids["8"] || len(ledger.scopes["selected"]) != 5) {
				t.Fatal("equal-hash independent source IDs were merged")
			}
		})
	}
}

func TestWindowRecoveryFailsClosed(t *testing.T) {
	for _, failure := range []string{"cross-origin", "invalid-token", "authentication", "detail-authentication", "detail-json", "redirect-without-location"} {
		t.Run(failure, func(t *testing.T) {
			var requests, external atomic.Int64
			outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { external.Add(1) }))
			defer outside.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.URL.Path == "/list":
					if r.URL.Query().Get("order") == "desc" {
						windowList(w, 1, 2, windowItem(2, 2))
					} else {
						windowList(w, 1, 2, windowItem(1, 1))
					}
				case r.Method == http.MethodHead:
					switch failure {
					case "cross-origin":
						w.Header().Set("Location", outside.URL+"/aaaa")
					case "invalid-token":
						w.Header().Set("Location", "/not-valid")
					case "authentication":
						w.WriteHeader(http.StatusForbidden)
						return
					case "redirect-without-location":
					default:
						w.Header().Set("Location", "/aaaa")
					}
					w.WriteHeader(http.StatusFound)
				default:
					if failure == "detail-authentication" {
						w.WriteHeader(http.StatusUnauthorized)
					}
					_, _ = io.WriteString(w, "private malformed body")
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			pages, err := windowRun(t, p, newWindowLedger(), &requests)
			want := "parse"
			if strings.Contains(failure, "authentication") {
				want = "authentication"
			}
			if FailureCode(err) != want || external.Load() != 0 || pages[len(pages)-1].Done {
				t.Fatalf("recovery did not fail closed: %v external=%d", err, external.Load())
			}
			if strings.HasPrefix(failure, "detail-") && string(pages[len(pages)-1].Body) != "private malformed body" {
				t.Fatal("failed detail response was not retained")
			}
		})
	}
}

func TestWindowTotalsAndVisibilityCannotCertifyMissingRecords(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		totals  string
		minimum int
		item    bool
		want    string
	}{
		{"malformed-present", `"total":null,"fallback":1`, 0, true, "parse"},
		{"excess-observed", `"total":0`, 0, true, "stalled"},
		{"invisible", `"total":0`, 1, false, "stalled"},
		{"fallback-present", `"fallback":1`, 0, true, ""},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var requests atomic.Int64
			body := `{"page":1,` + fixture.totals + `,"data":[`
			if fixture.item {
				body += windowItem(1, 1)
			}
			body += `]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.MinimumTotal = fixture.minimum
			pages, err := windowRun(t, p, newWindowLedger(), &requests)
			if fixture.want == "" {
				if err != nil || !pages[len(pages)-1].Done {
					t.Fatalf("missing primary total did not use fallback: %v", err)
				}
			} else if FailureCode(err) != fixture.want || pages[len(pages)-1].Done {
				t.Fatalf("invalid coverage was certified: %v", err)
			}
		})
	}
}

func TestWindowRejectsUnsafeTraversalConfiguration(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		change func(*model.Provider)
	}{
		{"unknown-total-mode", func(p *model.Provider) { p.Traversal.TotalMode = "approximate" }},
		{"scope-paging", func(p *model.Provider) { p.Traversal.Scopes[0].Query["page"] = 2 }},
		{"scope-auth", func(p *model.Provider) { p.Traversal.Scopes[0].Query["token"] = "not-allowed" }},
		{"unmapped-match", func(p *model.Provider) { p.Traversal.Scopes[0].Match["missing"] = 1 }},
		{"cross-origin-options", func(p *model.Provider) {
			p.Traversal.Options = &model.JSONOptions{URL: "https://outside.invalid/options", QueryParam: "option"}
		}},
		{"unknown-template", func(p *model.Provider) {
			p.Traversal.Options = &model.JSONOptions{URL: "/options/{missing}", QueryParam: "option"}
		}},
		{"unbounded-partition", func(p *model.Provider) {
			p.Traversal.Partitions = []model.JSONPartition{{Parameter: "bucket", Start: 1}}
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			p := windowFixture("http://127.0.0.1:1")
			fixture.change(&p)
			if _, err := New(t.Context(), p, model.ModeFull, newWindowLedger().environment(t)); err == nil {
				t.Fatal("unsafe traversal configuration was accepted")
			}
		})
	}
	if _, err := New(t.Context(), windowFixture("http://127.0.0.1:1"), model.ModeFull, Environment{}); err == nil {
		t.Fatal("traversal without durable coverage callbacks was accepted")
	}
}

func TestWindowKnownIDBatchResumesWithoutNetworkBytes(t *testing.T) {
	var requests atomic.Int64
	var mu sync.Mutex
	var resolved []string
	knownItems := make([]string, 100)
	for i := range knownItems {
		knownItems[i] = windowItem(i+1, 1)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		requests.Add(1)
		switch {
		case r.URL.Path == "/list":
			if r.URL.Query().Get("order") == "desc" {
				windowList(w, 1, 103, windowItem(103, 2))
			} else {
				windowList(w, 1, 101, knownItems...)
			}
		case r.Method == http.MethodHead:
			resolved = append(resolved, r.URL.Path)
			w.Header().Set("Location", "/aaaa")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = io.WriteString(w, windowItem(101, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.PageSize = 100
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	pages, err := windowRun(t, p, ledger, &requests)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	controlBatch := false
	for _, page := range pages {
		if page.Metadata["traversal_phase"] == "resolve" && page.Metadata["http_status"] == nil && len(page.Body) == 0 {
			controlBatch = true
		}
	}
	if !controlBatch || !reflect.DeepEqual(resolved, []string{"/resolve/101"}) || len(ledger.scopes["selected"]) != 101 {
		t.Fatalf("known-ID batch was not resumed without re-fetching committed IDs: control=%t resolved=%v", controlBatch, resolved)
	}
}

func TestWindowDiscoveryRejectsNoncanonicalIDs(t *testing.T) {
	for _, items := range []string{
		`{"id":"02","category":2}`,
		`{"id":2e0,"category":2}`,
		`{"category":2}`,
	} {
		t.Run(items, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method == http.MethodHead {
					t.Error("numeric recovery accepted an invalid source ID")
				}
				if r.URL.Query().Get("order") == "desc" {
					_, _ = fmt.Fprintf(w, `{"page":1,"total":3,"data":[%s]}`, items)
				} else {
					windowList(w, 1, 2, windowItem(1, 1))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			pages, err := windowRun(t, p, newWindowLedger(), &requests)
			if FailureCode(err) != "parse" || pages[len(pages)-1].Metadata["traversal_phase"] != "discover" {
				t.Fatalf("noncanonical discovery identity accepted: %v", err)
			}
		})
	}
}

func TestWindowMutableScopesReconcileFromLegacyBoundary(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy-last-scope-%t", legacy), func(t *testing.T) {
			var requests atomic.Int64
			var firstRoots atomic.Int64
			var mutable atomic.Bool
			mutable.Store(legacy)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				category, _ := strconv.Atoi(r.URL.Query().Get("category"))
				bucket := r.URL.Query().Get("bucket")
				if category == 1 && bucket == "" && firstRoots.Add(1) == 2 {
					mutable.Store(true)
				}
				var ids []int
				switch category {
				case 1:
					ids = []int{1, 2, 3, 4}
					if mutable.Load() {
						ids = []int{7, 1, 2} // added 7, moved 3, deleted 4
					}
				case 2:
					ids = []int{5, 6}
					if mutable.Load() {
						ids = []int{3, 5, 6}
					}
				case 3:
					if bucket != "" {
						t.Error("an unaffected completed scope was re-enumerated")
					}
					ids = []int{9}
				default:
					t.Errorf("unexpected scope %d", category)
				}
				total := len(ids)
				if bucket != "" {
					if len(ids) > 2 {
						ids = ids[2:]
					}
					total = len(ids)
				} else if len(ids) > 2 {
					ids = ids[:2]
				}
				items := make([]string, len(ids))
				for i, id := range ids {
					items[i] = windowItem(id, category)
				}
				windowList(w, 1, total, items...)
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.PageSize = 2
			end := 1
			p.Traversal.Partitions = []model.JSONPartition{{Parameter: "bucket", Start: 1, End: &end}}
			p.Traversal.Scopes = []model.JSONScope{
				{ID: "one", Query: map[string]any{"category": 1}, Match: map[string]any{"category_id": 1}},
				{ID: "two", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}},
				{ID: "three", Query: map[string]any{"category": 3}, Match: map[string]any{"category_id": 3}},
			}
			ledger := newWindowLedger()
			var checkpoint json.RawMessage
			if legacy {
				for category, ids := range map[int][]int{1: {1, 2, 3, 4}, 2: {5, 6}, 3: {9}} {
					for _, id := range ids {
						ledger.commit(model.Page{Items: []model.Record{{SourceID: strconv.Itoa(id), CoverageScopes: []string{p.Traversal.Scopes[category-1].ID}, Raw: []byte(windowItem(id, category))}}})
					}
				}
				checkpoint = json.RawMessage(`{"version":1,"phase":"reconcile","scope":3,"page":1,"ends":[1],"totals":[4,2,1]}`)
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]map[string]bool{
				"one":   {"1": true, "2": true, "7": true},
				"two":   {"3": true, "5": true, "6": true},
				"three": {"9": true},
			}
			if !reflect.DeepEqual(ledger.scopes, want) || ledger.ids["4"] || !pages[len(pages)-1].Done {
				t.Fatalf("mutable catalogue certified stale identities: %+v", ledger.scopes)
			}
			refreshes := 0
			fingerprints := make(map[string]bool)
			for _, page := range pages {
				if len(page.RefreshScopes) > 0 {
					refreshes++
					if !reflect.DeepEqual(page.RefreshScopes, []string{"one"}) {
						t.Fatalf("refresh erased unaffected or harmless-growth coverage: %v", page.RefreshScopes)
					}
				}
				if scope, ok := page.Metadata["fingerprint_scope"].(string); ok {
					key := scope + string(page.Body)
					if fingerprints[key] {
						t.Fatal("fresh enumeration collided with a committed successful-page fingerprint")
					}
					fingerprints[key] = true
				}
			}
			if refreshes != 1 {
				t.Fatalf("stable source required %d refresh rounds", refreshes)
			}
			foundDeletedOriginal := false
			for _, record := range ledger.records {
				if record.SourceID == "4" && string(record.Raw) == windowItem(4, 1) {
					foundDeletedOriginal = true
				}
			}
			if !foundDeletedOriginal {
				t.Fatal("derived refresh erased the original deleted-item observation")
			}
		})
	}
}

func TestWindowLegacyCheckpointRefreshesRootTotals(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("category") != "1" {
			t.Errorf("legacy end checkpoint did not restart root checks: %s", r.URL.RawQuery)
		}
		windowList(w, 1, 5487, windowItem(1, 1))
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	for i := 2; i <= 12; i++ {
		p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: fmt.Sprintf("subcat_%d", i), Query: map[string]any{"category": i}, Match: map[string]any{"category_id": i}})
	}
	c, err := New(t.Context(), p, model.ModeFull, newWindowLedger().environment(t))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkpoint := json.RawMessage(`{"version":1,"phase":"reconcile","scope":12,"page":1,"ends":[],"totals":[5486,14343,885,3853,5181,53247,29846,1322,3284,22,289,2429]}`)
	page, err := c.Fetch(t.Context(), checkpoint)
	var next windowCursor
	if err != nil || json.Unmarshal(page.Next, &next) != nil || next.Scope != 1 || next.Totals[0] != 5487 || requests.Load() != 1 || page.Done {
		t.Fatalf("legacy checkpoint remained trapped at frozen totals: page=%+v state=%+v error=%v", page, next, err)
	}
}

func TestWindowStaleExcessDoesNotDisableMissingIDRecovery(t *testing.T) {
	var requests atomic.Int64
	var mu sync.Mutex
	var heads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/list":
			if r.URL.Query().Get("order") == "desc" {
				windowList(w, 1, 5, windowItem(5, 2))
			} else {
				windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1))
			}
		case r.Method == http.MethodHead:
			mu.Lock()
			heads = append(heads, r.URL.Path)
			mu.Unlock()
			if r.URL.Path == "/resolve/3" {
				w.WriteHeader(http.StatusNotFound)
			} else if r.URL.Path == "/resolve/4" {
				w.Header().Set("Location", "/aaaa")
				w.WriteHeader(http.StatusFound)
			} else {
				t.Errorf("known or unnecessary identity resolved: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			_, _ = io.WriteString(w, windowItem(4, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	for _, id := range []string{"1", "2", "3", "6"} {
		ledger.commit(model.Page{Items: []model.Record{{SourceID: id, CoverageScopes: []string{"selected"}}}})
	}
	pages, err := windowRunFrom(t, p, ledger, &requests, json.RawMessage(`{"version":1,"phase":"reconcile","scope":1,"page":1,"ends":[],"totals":[3]}`))
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !pages[len(pages)-1].Done || ledger.ids["3"] || !ledger.ids["4"] || !reflect.DeepEqual(heads, []string{"/resolve/3", "/resolve/4"}) {
		t.Fatalf("stale excess prevented truthful missing-ID recovery: coverage=%v heads=%v err=%v", ledger.scopes, heads, err)
	}
}

func TestWindowExhaustedReconciliationRequiresExplicitFailedResume(t *testing.T) {
	var requests atomic.Int64
	var corrected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		total := 0
		if corrected.Load() {
			total = 1
		}
		windowList(w, 1, total, windowItem(1, 1))
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	ledger := newWindowLedger()
	pages, err := windowRun(t, p, ledger, &requests)
	last := pages[len(pages)-1]
	refreshes := 0
	for _, page := range pages {
		if len(page.RefreshScopes) > 0 {
			refreshes++
		}
	}
	if FailureCode(err) != "stalled" || refreshes != 3 || last.Metadata["coverage_incomplete"] != true {
		t.Fatalf("inconsistent source did not stop after bounded fresh rounds: refreshes=%d page=%+v err=%v", refreshes, last, err)
	}
	c, err := New(t.Context(), p, model.ModeFull, ledger.environment(t))
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	repeated, err := c.Fetch(t.Context(), last.Next)
	_ = c.Close()
	if FailureCode(err) != "stalled" || requests.Load() != before || string(repeated.Next) != string(last.Next) {
		t.Fatalf("ordinary reconstruction silently renewed exhausted retries: %+v %v", repeated, err)
	}
	corrected.Store(true)
	env := ledger.environment(t)
	env.CoverageAttempt = "explicit-failed-resume-42"
	c, err = New(t.Context(), p, model.ModeFull, env)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fresh, err := c.Fetch(t.Context(), last.Next)
	if err != nil || len(fresh.Body) == 0 || requests.Load() != before+1 {
		t.Fatalf("explicit failed Resume did not recheck source totals: %+v %v", fresh, err)
	}
	ledger.commit(fresh)
	resumed, err := windowRunFrom(t, p, ledger, &requests, fresh.Next)
	done := resumed[len(resumed)-1]
	if err != nil || !done.Done {
		t.Fatalf("corrected source did not complete on explicit retry: %+v %v", done, err)
	}
	var state windowCursor
	if json.Unmarshal(done.Next, &state) != nil || state.Generation != 1 || state.Round != 0 || state.Attempt != env.CoverageAttempt {
		t.Fatalf("retry generation was not durably renewed: %+v", state)
	}
	for _, page := range pages {
		if page.Metadata["fingerprint_scope"] == fresh.Metadata["fingerprint_scope"] {
			t.Fatal("explicit retry recycled an older successful-page fingerprint")
		}
	}
}

func TestWindowAdditiveCatchupPreservesDeficitProgress(t *testing.T) {
	var requests, selectedRoots atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("category") == "2" {
			windowList(w, 1, 1, windowItem(9, 2))
			return
		}
		id := int(selectedRoots.Add(1))
		if id > 3 {
			id = 3
		}
		windowList(w, 1, 3, windowItem(id, 1))
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "complete", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
	ledger := newWindowLedger()
	ledger.commit(model.Page{Items: []model.Record{{SourceID: "1", CoverageScopes: []string{"selected"}}, {SourceID: "9", CoverageScopes: []string{"complete"}}}})
	pages, err := windowRunFrom(t, p, ledger, &requests, json.RawMessage(`{"version":1,"phase":"reconcile","scope":2,"page":1,"ends":[],"totals":[3,1]}`))
	if err != nil || !pages[len(pages)-1].Done || len(ledger.scopes["selected"]) != 3 || !ledger.ids["1"] || !ledger.ids["9"] {
		t.Fatalf("additive catch-up discarded accepted progress: scopes=%v err=%v", ledger.scopes, err)
	}
	catchups := 0
	for _, page := range pages {
		if len(page.RefreshScopes) > 0 {
			t.Fatalf("deficit-only catch-up reset identities: %v", page.RefreshScopes)
		}
		if scopes, ok := page.Metadata["coverage_catchup_scopes"].([]string); ok {
			catchups++
			if !reflect.DeepEqual(scopes, []string{"selected"}) {
				t.Fatalf("additive catch-up enumerated completed scopes: %v", scopes)
			}
		}
	}
	if catchups != 1 {
		t.Fatalf("bounded additive source required %d catch-ups", catchups)
	}
}

func TestWindowExcessRefreshPreservesOtherDeficitsForRecovery(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("order") == "desc" {
			windowList(w, 1, 5, windowItem(5, 2))
			return
		}
		if r.Method == http.MethodHead {
			t.Errorf("already accepted or unnecessary identity revalidated: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Query().Get("category") {
		case "1":
			windowList(w, 1, 1, windowItem(1, 1))
		case "2":
			windowList(w, 1, 3, windowItem(3, 2)) // ID4 is never repeated.
		case "3":
			windowList(w, 1, 1, windowItem(9, 3))
		default:
			t.Errorf("unexpected recovery path: %s", r.URL)
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.Scopes = []model.JSONScope{
		{ID: "excess", Query: map[string]any{"category": 1}, Match: map[string]any{"category_id": 1}},
		{ID: "deficit", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}},
		{ID: "complete", Query: map[string]any{"category": 3}, Match: map[string]any{"category_id": 3}},
	}
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	ledger.commit(model.Page{Items: []model.Record{
		{SourceID: "1", CoverageScopes: []string{"excess"}}, {SourceID: "2", CoverageScopes: []string{"excess"}},
		{SourceID: "3", CoverageScopes: []string{"deficit"}}, {SourceID: "4", CoverageScopes: []string{"deficit"}},
		{SourceID: "9", CoverageScopes: []string{"complete"}},
	}})
	pages, err := windowRunFrom(t, p, ledger, &requests, json.RawMessage(`{"version":1,"phase":"reconcile","scope":3,"page":1,"ends":[],"totals":[1,3,1]}`))
	if err != nil || !pages[len(pages)-1].Done || ledger.ids["2"] || !ledger.ids["4"] || len(ledger.scopes["deficit"]) != 3 {
		t.Fatalf("excess repair discarded unrelated deficit progress: %v %v", ledger.scopes, err)
	}
	for _, page := range pages {
		if len(page.RefreshScopes) > 0 && !reflect.DeepEqual(page.RefreshScopes, []string{"excess"}) {
			t.Fatalf("excess repair reset other scopes: %v", page.RefreshScopes)
		}
	}
}

func TestWindowValidMoveOutsideEmitsNegativeCoverage(t *testing.T) {
	p := windowFixture("http://127.0.0.1:1")
	c := &windowJSONConnector{provider: p}
	ledger := newWindowLedger()
	ledger.commit(model.Page{Items: []model.Record{c.record(json.RawMessage(windowItem(1, 1)), p.Mapping), c.record(json.RawMessage(windowItem(2, 1)), p.Mapping)}})
	outside := c.record(json.RawMessage(windowItem(1, 2)), p.Mapping)
	ledger.commit(model.Page{Items: []model.Record{outside}})
	if ledger.ids["1"] || !ledger.ids["2"] || len(ledger.scopes["selected"]) != 1 {
		t.Fatalf("valid out-of-scope identity still satisfied coverage: %+v %+v", outside, ledger.scopes)
	}
	ledger.commit(model.Page{Items: []model.Record{c.record(json.RawMessage(windowItem(1, 1)), p.Mapping)}})
	if !ledger.ids["1"] || len(ledger.scopes["selected"]) != 2 || string(outside.Raw) != windowItem(1, 2) {
		t.Fatal("move-back lost identity, simultaneous source occurrence, or raw evidence")
	}
}

func TestWindowUnorderedDiscoveryRecoversBeyondObservedIDs(t *testing.T) {
	var requests atomic.Int64
	var mu sync.Mutex
	var heads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/list":
			if r.URL.Query().Get("sortBy") == "id" {
				windowList(w, 1, 8, windowItem(2, 1), windowItem(5, 2), windowItem(4, 1))
			} else {
				windowList(w, 1, 4, windowItem(1, 1))
			}
		case r.Method == http.MethodHead:
			mu.Lock()
			heads = append(heads, r.URL.Path)
			mu.Unlock()
			if r.URL.Path == "/resolve/8" {
				w.Header().Set("Location", "/aaaa")
				w.WriteHeader(http.StatusFound)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case r.URL.Path == "/detail/aaaa":
			_, _ = io.WriteString(w, windowItem(8, 1))
		default:
			t.Errorf("unexpected recovery request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.PageSize = 3
	p.HTTP.Query = map[string]any{"sortBy": "createdAt"}
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"sortBy": "id", "sortOrder": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	ledger.commit(model.Page{Items: []model.Record{{SourceID: "1", CoverageScopes: []string{"selected"}}}})
	checkpoint := json.RawMessage(`{"version":2,"phase":"discover","scope":1,"page":1,"ends":[],"totals":[4],"recovery":true,"round":3}`)
	pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
	if err != nil || !pages[len(pages)-1].Done || len(ledger.scopes["selected"]) != 4 || !ledger.ids["8"] {
		t.Fatalf("unordered discovery or an apparent maximum truncated recovery: coverage=%v err=%v", ledger.scopes, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(heads, []string{"/resolve/3", "/resolve/5", "/resolve/6", "/resolve/7", "/resolve/8"}) {
		t.Fatalf("range extension repeated old holes or skipped unseen IDs: %v", heads)
	}
	for _, page := range pages {
		if len(page.RefreshScopes) > 0 {
			t.Fatal("numeric range extension discarded accepted coverage")
		}
	}
}

func TestWindowRecoveryNumericDomainBoundary(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("provider first ID is bounded by the platform int")
	}
	for _, exists := range []bool{true, false} {
		t.Run(fmt.Sprintf("last-ID-exists-%t", exists), func(t *testing.T) {
			var requests atomic.Int64
			var mu sync.Mutex
			var heads []string
			maximum := int64(math.MaxInt64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.URL.Path == "/list":
					windowList(w, 1, 2, windowItem(1, 1))
				case r.Method == http.MethodHead:
					mu.Lock()
					heads = append(heads, r.URL.Path)
					mu.Unlock()
					if exists {
						w.Header().Set("Location", "/aaaa")
						w.WriteHeader(http.StatusFound)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				default:
					_, _ = io.WriteString(w, windowItem(int(maximum), 1))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: int(maximum), DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			ledger := newWindowLedger()
			pages, err := windowRun(t, p, ledger, &requests)
			last := pages[len(pages)-1]
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(heads, []string{"/resolve/" + strconv.FormatInt(maximum, 10)}) {
				t.Fatalf("numeric boundary skipped, repeated or wrapped: %v", heads)
			}
			if exists {
				if err != nil || !last.Done || !ledger.ids[strconv.FormatInt(maximum, 10)] {
					t.Fatalf("last representable source ID was lost: %+v %v", last, err)
				}
			} else if FailureCode(err) != "stalled" || last.Done || last.Metadata["coverage_incomplete"] != true {
				t.Fatalf("domain exhaustion certified missing coverage: %+v %v", last, err)
			}
		})
	}
}

func TestWindowRecoveryPreservesUnscannedTailAfterRootGrowth(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy-reconcile-%t", legacy), func(t *testing.T) {
			var requests atomic.Int64
			var recovered atomic.Bool
			recovered.Store(legacy)
			var mu sync.Mutex
			var heads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.URL.Path == "/list":
					if r.URL.Query().Get("order") == "desc" {
						windowList(w, 1, 6, windowItem(6, 2))
					} else {
						total := 2
						if recovered.Load() {
							total = 3
						}
						windowList(w, 1, total, windowItem(1, 1))
					}
				case r.Method == http.MethodHead:
					mu.Lock()
					heads = append(heads, r.URL.Path)
					mu.Unlock()
					switch r.URL.Path {
					case "/resolve/2":
						w.Header().Set("Location", "/aaaa")
						w.WriteHeader(http.StatusFound)
					case "/resolve/3":
						w.WriteHeader(http.StatusNotFound)
					case "/resolve/4":
						w.Header().Set("Location", "/bbbb")
						w.WriteHeader(http.StatusFound)
					default:
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				case r.URL.Path == "/detail/aaaa":
					recovered.Store(true)
					_, _ = io.WriteString(w, windowItem(2, 1))
				case r.URL.Path == "/detail/bbbb":
					_, _ = io.WriteString(w, windowItem(4, 1))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			ledger := newWindowLedger()
			ledger.commit(model.Page{Items: []model.Record{{SourceID: "1", CoverageScopes: []string{"selected"}}}})
			checkpoint := json.RawMessage(`{"version":2,"phase":"discover","scope":1,"page":1,"ends":[],"totals":[2],"recovery":true}`)
			want := []string{"/resolve/2", "/resolve/3", "/resolve/4"}
			if legacy {
				ledger.commit(model.Page{Items: []model.Record{{SourceID: "2", CoverageScopes: []string{"selected"}}}})
				checkpoint = json.RawMessage(`{"version":2,"phase":"reconcile","page":1,"ends":[],"totals":[2],"recovery":true,"next_id":3,"max_id":6,"grew":true}`)
				want = want[1:]
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || !pages[len(pages)-1].Done || !ledger.ids["4"] || len(ledger.scopes["selected"]) != 3 || !reflect.DeepEqual(heads, want) {
				t.Fatalf("root growth skipped the unscanned tail: ids=%v heads=%v err=%v", ledger.ids, heads, err)
			}
		})
	}
}

func TestWindowScopeRefreshKeepsNumericFrontier(t *testing.T) {
	for _, scenario := range []string{"refresh", "failed-resume", "forward-progress", "failed-detail", "alias", "targeted-completion"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int64
			var detailFailed atomic.Bool
			var mu sync.Mutex
			var heads []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.URL.Path == "/list":
					switch {
					case r.URL.Query().Get("order") == "desc":
						windowList(w, 1, 12, windowItem(12, 3))
					case r.URL.Query().Get("category") == "1":
						windowList(w, 1, 2, windowItem(1, 1))
					default:
						windowList(w, 1, 2, windowItem(2, 2))
					}
				case r.Method == http.MethodHead:
					mu.Lock()
					heads = append(heads, r.URL.Path)
					mu.Unlock()
					switch r.URL.Path {
					case "/resolve/5":
						w.Header().Set("Location", "/aaaa")
						w.WriteHeader(http.StatusFound)
					case "/resolve/9":
						if scenario == "alias" {
							w.Header().Set("Location", "/aaaa")
							w.WriteHeader(http.StatusFound)
						} else {
							w.WriteHeader(http.StatusNotFound)
						}
					case "/resolve/10":
						w.WriteHeader(http.StatusNotFound)
					case "/resolve/11":
						w.Header().Set("Location", "/bbbb")
						w.WriteHeader(http.StatusFound)
					default:
						w.WriteHeader(http.StatusServiceUnavailable)
					}
				case r.URL.Path == "/detail/aaaa":
					if scenario == "failed-detail" && !detailFailed.Swap(true) {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = io.WriteString(w, windowItem(5, 1))
				case r.URL.Path == "/detail/bbbb":
					_, _ = io.WriteString(w, windowItem(11, 2))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.Scopes = []model.JSONScope{
				{ID: "changed", Query: map[string]any{"category": 1}, Match: map[string]any{"category_id": 1}},
				{ID: "stable", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}},
			}
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			ledger := newWindowLedger()
			for _, id := range []string{"1", "5", "9"} {
				ledger.commit(model.Page{Items: []model.Record{{SourceID: id, CoverageScopes: []string{"changed"}}}})
			}
			ledger.commit(model.Page{Items: []model.Record{{SourceID: "2", CoverageScopes: []string{"stable"}}}})
			checkpoint := json.RawMessage(`{"version":3,"phase":"check","scope":2,"page":1,"ends":[],"totals":[2,2],"recovery":true,"next_id":10,"max_id":10}`)
			want := []string{"/resolve/5", "/resolve/9", "/resolve/11"}
			if scenario == "targeted-completion" {
				ledger.commit(model.Page{Items: []model.Record{{SourceID: "11", CoverageScopes: []string{"stable"}}}})
				want = []string{"/resolve/5"}
			}
			if scenario == "forward-progress" {
				checkpoint = json.RawMessage(`{"version":3,"phase":"resolve","scope":2,"page":1,"ends":[],"totals":[2,2],"recovery":true,"next_id":10,"max_id":10,"round":3}`)
				want = append([]string{"/resolve/10"}, want...)
			}
			if scenario == "failed-resume" {
				env := ledger.environment(t)
				env.CoverageAttempt = "retry-preserving-frontier"
				c, err := New(t.Context(), p, model.ModeFull, env)
				if err != nil {
					t.Fatal(err)
				}
				page, err := c.Fetch(t.Context(), checkpoint)
				_ = c.Close()
				if err != nil {
					t.Fatal(err)
				}
				ledger.commit(page)
				checkpoint = page.Next
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			if scenario == "failed-detail" {
				last := pages[len(pages)-1]
				if FailureCode(err) != "http" || last.Done {
					t.Fatalf("failed detail was accepted as recovery evidence: %+v %v", last, err)
				}
				pages, err = windowRunFrom(t, p, ledger, &requests, last.Next)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil || !pages[len(pages)-1].Done || !reflect.DeepEqual(heads, want) {
				t.Fatalf("scope refresh rewound unrelated probes or skipped affected IDs: heads=%v want=%v err=%v", heads, want, err)
			}
			if !ledger.ids["5"] || !ledger.ids["11"] || ledger.ids["9"] || len(ledger.ids) != 4 {
				t.Fatalf("targeted refresh lost or invented identities: %v", ledger.ids)
			}
		})
	}
}

func TestWindowResumedNumericScanCatchesUpOnlyDeficientScopes(t *testing.T) {
	for _, path := range []string{"next-page", "options", "pending-detail"} {
		t.Run(path, func(t *testing.T) {
			var requests, heads, details, completeRoots, optionCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.Method == http.MethodHead:
					heads.Add(1)
					w.WriteHeader(http.StatusServiceUnavailable)
				case r.URL.Path == "/detail/aaaa":
					details.Add(1)
					_, _ = io.WriteString(w, windowItem(5, 3))
				case r.URL.Path == "/options/1":
					optionCalls.Add(1)
					_, _ = io.WriteString(w, `{"groups":[{"values":[{"id":"hidden"}]}]}`)
				case r.URL.Query().Get("category") == "2":
					completeRoots.Add(1)
					windowList(w, 1, 1, windowItem(9, 2))
				case r.URL.Query().Get("year") != "":
					t.Error("catch-up repeated year partitions before trying metadata options")
					w.WriteHeader(http.StatusServiceUnavailable)
				case r.URL.Query().Get("option") == "hidden":
					windowList(w, 1, 1, windowItem(3, 1))
				case r.URL.Query().Get("page") == "2":
					windowList(w, 2, 3, windowItem(3, 1))
				default:
					windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.PageSize, p.Traversal.WindowPages = 2, 3
			p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "complete", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			if path == "options" {
				end := 2020
				p.Traversal.WindowPages = 1
				p.Traversal.Partitions = []model.JSONPartition{{Parameter: "year", Start: 2020, End: &end}}
				p.Traversal.Options = &model.JSONOptions{URL: "/options/{category}", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
			}
			ledger := newWindowLedger()
			ledger.commit(model.Page{Items: []model.Record{
				{SourceID: "1", CoverageScopes: []string{"selected"}},
				{SourceID: "2", CoverageScopes: []string{"selected"}},
				{SourceID: "9", CoverageScopes: []string{"complete"}},
			}})
			state := windowCursor{Version: 2, Phase: "resolve", Scope: 2, Page: 1, Ends: []int{}, Totals: []int64{3, 1}, Recovery: true, NextID: 5, MaxID: 1000, Round: 3}
			if path == "options" {
				state.Ends = []int{2020}
			}
			if path == "pending-detail" {
				state.Phase, state.Pending = "detail", "aaaa"
			}
			checkpoint, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			if err != nil || !pages[len(pages)-1].Done || heads.Load() != 0 || completeRoots.Load() != 2 {
				t.Fatalf("numeric fallback bypassed targeted catch-up: heads=%d complete-roots=%d err=%v", heads.Load(), completeRoots.Load(), err)
			}
			if len(ledger.ids) != 4 || !ledger.ids["1"] || !ledger.ids["2"] || !ledger.ids["3"] || !ledger.ids["9"] || ledger.ids["5"] {
				t.Fatalf("catch-up discarded prior coverage or invented an identity: %v", ledger.ids)
			}
			if (path == "options" && optionCalls.Load() != 1) || (path == "pending-detail" && details.Load() != 1) {
				t.Fatalf("option lookup or already-resolved detail was lost: options=%d details=%d", optionCalls.Load(), details.Load())
			}
			for _, page := range pages {
				if len(page.RefreshScopes) != 0 {
					t.Fatalf("additive recovery retired committed scope identities: %v", page.RefreshScopes)
				}
			}
		})
	}
}

func TestWindowUnchangedDeficitDoesNotRepeatCatchupAtEachNumericRange(t *testing.T) {
	var requests, roots atomic.Int64
	var mu sync.Mutex
	var heads []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/list":
			if r.URL.Query().Get("order") == "desc" {
				windowList(w, 1, 4, windowItem(4, 2))
			} else {
				if roots.Add(1) > 4 {
					t.Error("unchanged deficit renewed an ineffective catch-up")
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				windowList(w, 1, 3, windowItem(1, 1), windowItem(2, 1))
			}
		case r.Method == http.MethodHead:
			mu.Lock()
			heads = append(heads, r.URL.Path)
			mu.Unlock()
			if r.URL.Path == "/resolve/5" {
				w.Header().Set("Location", "/aaaa")
				w.WriteHeader(http.StatusFound)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			_, _ = io.WriteString(w, windowItem(5, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.PageSize = 2
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	ledger.commit(model.Page{Items: []model.Record{{SourceID: "1", CoverageScopes: []string{"selected"}}, {SourceID: "2", CoverageScopes: []string{"selected"}}}})
	pages, err := windowRunFrom(t, p, ledger, &requests, json.RawMessage(`{"version":3,"phase":"check","scope":1,"page":1,"ends":[],"totals":[3]}`))
	mu.Lock()
	defer mu.Unlock()
	if err != nil || !pages[len(pages)-1].Done || !ledger.ids["5"] || !reflect.DeepEqual(heads, []string{"/resolve/3", "/resolve/4", "/resolve/5"}) {
		t.Fatalf("catch-up starved or rewound numeric progress: heads=%v roots=%d err=%v", heads, roots.Load(), err)
	}
}

func TestWindowCompleteOptionSkipsRedundantSorts(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/options" {
			_, _ = io.WriteString(w, `{"groups":[{"values":[{"id":"first"},{"id":"empty"},{"id":"last"}]}]}`)
			return
		}
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		if query.Get("option") != "" && query.Get("order") != "" {
			http.Error(w, "completed filter was queried again", http.StatusTooManyRequests)
			return
		}
		switch query.Get("option") {
		case "first":
			if page == 1 {
				windowList(w, page, 3, windowItem(1, 1), windowItem(2, 1))
			} else {
				windowList(w, page, 3, windowItem(3, 1))
			}
		case "empty":
			windowList(w, page, 0)
		case "last":
			windowList(w, page, 2, windowItem(4, 1), windowItem(5, 1))
		default:
			windowList(w, page, 5, windowItem(1, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.WindowPages = 2
	p.Traversal.QueryVariants = []map[string]any{{"order": "reverse"}, {"order": "other"}}
	p.Traversal.Options = &model.JSONOptions{URL: "/options", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
	ledger := newWindowLedger()
	_, err := windowRun(t, p, ledger, &requests)
	if err != nil || len(ledger.scopes["selected"]) != 5 {
		t.Fatalf("exhausted filters prevented collection of the remaining identities: ids=%v err=%v", ledger.ids, err)
	}
}

func TestWindowIncompleteOptionRetainsUsefulSorts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		total  int
		first  []string
		second []string
	}{
		{"capped-union", 3, []string{windowItem(1, 1), windowItem(2, 1)}, []string{windowItem(2, 1), windowItem(3, 1)}},
		{"duplicate-identities", 2, []string{windowItem(1, 1), windowItem(1, 1)}, []string{windowItem(2, 1), windowItem(2, 1)}},
		{"rejected-identity", 2, []string{windowItem(1, 1), `{"id":2,"category":1,"size":"invalid"}`}, []string{windowItem(1, 1), windowItem(2, 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == "/options" {
					_, _ = io.WriteString(w, `{"groups":[{"values":[{"id":"first"},{"id":"last"}]}]}`)
					return
				}
				query := r.URL.Query()
				switch query.Get("option") {
				case "first":
					switch query.Get("order") {
					case "":
						windowList(w, 1, tc.total, tc.first...)
					case "reverse":
						windowList(w, 1, tc.total, tc.second...)
					default:
						http.Error(w, "already covered filter exceeded its request budget", http.StatusTooManyRequests)
					}
				case "last":
					windowList(w, 1, 1, windowItem(20, 1))
				default:
					windowList(w, 1, tc.total+2, windowItem(10, 1))
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.QueryVariants = []map[string]any{{"order": "reverse"}, {"order": "waste"}}
			p.Traversal.Options = &model.JSONOptions{URL: "/options", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
			ledger := newWindowLedger()
			_, err := windowRun(t, p, ledger, &requests)
			if err != nil || len(ledger.scopes["selected"]) != tc.total+2 || !ledger.ids["20"] {
				t.Fatalf("filter completeness lost identities or wasted the next query: ids=%v err=%v", ledger.ids, err)
			}
		})
	}
}

func TestWindowLegacyOptionResumeUsesArchivedCoverage(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("option") == "first" {
			http.Error(w, "resumed a completed filter", http.StatusTooManyRequests)
			return
		}
		if r.URL.Query().Get("option") == "last" {
			windowList(w, 1, 2, windowItem(4, 1), windowItem(5, 1))
		} else {
			windowList(w, 1, 5, windowItem(1, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.WindowPages = 2
	p.Traversal.QueryVariants = []map[string]any{{"order": "reverse"}, {"order": "other"}}
	p.Traversal.Options = &model.JSONOptions{URL: "/options", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
	ledger := newWindowLedger()
	env := ledger.environment(t)
	source, err := New(t.Context(), p, model.ModeFull, env)
	if err != nil {
		t.Fatal(err)
	}
	collector := source.(*windowJSONConnector)
	state := collector.initialState()
	state.Phase, state.Options, state.Totals = "options_list", []string{"first", "last"}, []int64{5}
	state.Recovery, state.NextID, state.MaxID = true, 1053, 1052
	state.Generation, state.Round, state.CatchupRound = 2, 1, 2
	state.Variant = -1
	request, err := collector.request(state)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := collector.fingerprint(state, request)
	for _, ids := range [][]int{{1, 2}, {3}} {
		page := model.Page{Metadata: map[string]any{"traversal_phase": "options_list", "fingerprint_scope": fingerprint, "total": int64(3)}}
		for _, id := range ids {
			page.Items = append(page.Items, collector.record(json.RawMessage(windowItem(id, 1)), p.Mapping))
		}
		ledger.commit(page)
	}
	_ = source.Close()
	// A pre-fix v3 worker was already midway through another redundant sort.
	state.Variant, state.Page = 1, 2
	checkpoint, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	proof := env.FilterComplete
	fail := true
	env.FilterComplete = func(ctx context.Context, scope string, fingerprints []string) (bool, error) {
		if fail {
			return false, fmt.Errorf("coverage storage unavailable")
		}
		return proof(ctx, scope, fingerprints)
	}
	resumed, err := New(t.Context(), p, model.ModeFull, env)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	failed, err := resumed.Fetch(t.Context(), checkpoint)
	if err == nil || string(failed.Next) != string(checkpoint) || requests.Load() != 0 {
		t.Fatalf("unavailable coverage advanced the checkpoint or used the source: page=%+v requests=%d err=%v", failed, requests.Load(), err)
	}
	fail = false
	control, err := resumed.Fetch(t.Context(), checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := resumed.Fetch(t.Context(), checkpoint)
	if err != nil || string(repeated.Next) != string(control.Next) || requests.Load() != 0 {
		t.Fatalf("uncommitted filter completion was not safely repeatable: requests=%d err=%v", requests.Load(), err)
	}
	ledger.commit(control)
	pages, err := windowRunFrom(t, p, ledger, &requests, control.Next)
	if err != nil || len(ledger.scopes["selected"]) != 5 {
		t.Fatalf("legacy resume lost remaining identities: ids=%v err=%v", ledger.ids, err)
	}
	var finished windowCursor
	if err := json.Unmarshal(pages[len(pages)-1].Next, &finished); err != nil || finished.NextID != 1053 || finished.MaxID != 1052 {
		t.Fatalf("filter completion changed the numeric frontier: %+v %v", finished, err)
	}
}

func TestWindowAtLeastFreezesTargetsAndRetainsSameHashIDs(t *testing.T) {
	for _, changedTotal := range []int{6, 2} {
		t.Run(fmt.Sprintf("later-total-%d", changedTotal), func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				query := r.URL.Query()
				page, _ := strconv.Atoi(query.Get("page"))
				if r.URL.Path != "/list" || query.Get("bucket") != "" || query.Get("order") != "" {
					http.Error(w, "covered scope requested another view", http.StatusTooManyRequests)
					return
				}
				switch {
				case query.Get("view") == "overlap" && page == 1:
					windowList(w, page, 3, windowItem(1, 1), windowItem(2, 1))
				case query.Get("view") == "" && page == 1:
					windowList(w, page, 3, windowItem(1, 1), windowItem(2, 1))
				case query.Get("view") == "" && page == 2:
					windowList(w, page, changedTotal, windowItem(3, 1), windowItem(4, 1))
				default:
					http.Error(w, "covered root requested another page", http.StatusTooManyRequests)
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.PageSize, p.Traversal.WindowPages = 2, 3
			p.Traversal.TotalMode, p.Traversal.MinimumTotal = "at_least", 6
			p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "overlap", Query: map[string]any{"category": 1, "view": "overlap"}, Match: map[string]any{"category_id": 1}})
			end := 2
			p.Traversal.Partitions = []model.JSONPartition{{Parameter: "bucket", Start: 1, End: &end}}
			p.Traversal.QueryVariants = []map[string]any{{"order": "reverse"}}
			p.Traversal.Options = &model.JSONOptions{URL: "/options", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
			ledger := newWindowLedger()
			pages, err := windowRun(t, p, ledger, &requests)
			if err != nil || !pages[len(pages)-1].Done || requests.Load() != 3 {
				t.Fatalf("frozen target caused re-enumeration or skipped a scope's first root: requests=%d err=%v", requests.Load(), err)
			}
			want := map[string]bool{"1": true, "2": true, "3": true, "4": true}
			if !reflect.DeepEqual(ledger.ids, want) || !reflect.DeepEqual(ledger.scopes["selected"], want) || !reflect.DeepEqual(ledger.scopes["overlap"], want) {
				t.Fatalf("surplus native IDs sharing one hash were lost: ids=%v scopes=%v", ledger.ids, ledger.scopes)
			}
			for _, page := range pages {
				if len(page.RefreshScopes) != 0 {
					t.Fatalf("a changing total retired accepted identities: %v", page.RefreshScopes)
				}
			}
			expected := map[string]int64{"selected": 3, "overlap": 3}
			if !reflect.DeepEqual(pages[len(pages)-1].Metadata["coverage_expected"], expected) {
				t.Fatalf("first root totals were replaced: %v", pages[len(pages)-1].Metadata)
			}
		})
	}
}

func TestWindowAtLeastCannotCertifyMissingNativeIDs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		items   []string
		minimum int
		move    bool
	}{
		{name: "duplicate-ID", items: []string{windowItem(1, 1), windowItem(1, 1)}},
		{name: "invalid-ID", items: []string{windowItem(1, 1), `{"id":2,"category":1,"size":"invalid"}`}},
		{name: "latest-membership", items: []string{windowItem(1, 1), windowItem(2, 1)}, move: true},
		{name: "visibility-minimum", items: []string{windowItem(1, 1), windowItem(2, 1)}, minimum: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Query().Get("category") == "2" {
					windowList(w, 1, 1, windowItem(2, 2))
				} else {
					windowList(w, 1, 2, tc.items...)
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.TotalMode, p.Traversal.MinimumTotal = "at_least", tc.minimum
			if tc.move {
				p.Traversal.Scopes = append(p.Traversal.Scopes, model.JSONScope{ID: "other", Query: map[string]any{"category": 2}, Match: map[string]any{"category_id": 2}})
			}
			ledger := newWindowLedger()
			pages, err := windowRun(t, p, ledger, &requests)
			last := pages[len(pages)-1]
			if FailureCode(err) != "stalled" || last.Done || last.Metadata["coverage_incomplete"] != true || requests.Load() != int64(len(p.Traversal.Scopes)) {
				t.Fatalf("missing coverage was certified or re-enumerated: requests=%d page=%+v err=%v", requests.Load(), last, err)
			}
			if tc.move && (ledger.scopes["selected"]["2"] || !ledger.scopes["other"]["2"]) {
				t.Fatalf("latest native membership was not applied: %v", ledger.scopes)
			}
			for _, page := range pages {
				if len(page.RefreshScopes) != 0 {
					t.Fatalf("incomplete traversal retired accepted evidence: %v", page.RefreshScopes)
				}
			}
		})
	}
}

func TestWindowAtLeastResumesTargetsWithCompleteRootProof(t *testing.T) {
	for _, tc := range []struct {
		name   string
		total  int
		items  []string
		done   bool
		target int64
	}{
		{"growth", 6, []string{windowItem(2, 1), windowItem(3, 1)}, true, 3},
		{"inconsistent-contraction", 1, []string{windowItem(1, 1), windowItem(3, 1)}, true, 3},
		{"complete-contraction", 2, []string{windowItem(1, 1), windowItem(2, 1)}, true, 2},
		{"incomplete-response", 3, []string{windowItem(1, 1), windowItem(2, 1)}, false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				windowList(w, 1, tc.total, tc.items...)
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.TotalMode = "at_least"
			ledger := newWindowLedger()
			ledger.commit(model.Page{Items: []model.Record{
				{SourceID: "1", CoverageScopes: []string{"selected"}},
				{SourceID: "2", CoverageScopes: []string{"selected"}},
			}})
			checkpoint := json.RawMessage(`{"version":3,"phase":"root","page":1,"ends":[],"totals":[3]}`)
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			last := pages[len(pages)-1]
			if last.Done != tc.done || (tc.done && err != nil) || (!tc.done && FailureCode(err) != "stalled") || requests.Load() != 1 {
				t.Fatalf("resumed root changed its original completion target: requests=%d page=%+v err=%v", requests.Load(), last, err)
			}
			if !reflect.DeepEqual(last.Metadata["coverage_expected"], map[string]int64{"selected": tc.target}) || !ledger.ids["1"] || !ledger.ids["2"] {
				t.Fatalf("resumed root replaced its target or retired prior IDs: ids=%v coverage=%v", ledger.ids, last.Metadata)
			}
		})
	}
}

func TestWindowAtLeastResumePreservesNumericFrontier(t *testing.T) {
	for _, phase := range []string{"root", "reconcile", "resolve", "detail"} {
		t.Run(phase, func(t *testing.T) {
			var requests, roots, heads, details atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch {
				case r.URL.Path == "/list" && r.URL.Query().Get("order") == "desc":
					windowList(w, 1, 50, windowItem(6, 2))
				case r.URL.Path == "/list":
					if roots.Add(1) > 2 || phase == "detail" {
						http.Error(w, "small root was refreshed without a deficit", http.StatusTooManyRequests)
						return
					}
					windowList(w, 1, 2, windowItem(1, 1))
				case r.Method == http.MethodHead:
					heads.Add(1)
					if r.URL.Path != "/resolve/5" {
						http.Error(w, "numeric tail skipped or revisited", http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Location", "/aaaa")
					w.WriteHeader(http.StatusFound)
				case r.URL.Path == "/detail/aaaa":
					details.Add(1)
					_, _ = io.WriteString(w, windowItem(5, 1))
				default:
					http.Error(w, "unexpected recovery request", http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.TotalMode = "at_least"
			p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
			ledger := newWindowLedger()
			ledger.commit(model.Page{Items: []model.Record{{SourceID: "1", CoverageScopes: []string{"selected"}}}})
			state := windowCursor{Version: 3, Phase: phase, Page: 1, Ends: []int{}, Totals: []int64{2}, Recovery: true, NextID: 5, MaxID: 9, RecheckAfter: 8}
			if phase == "root" {
				state.Totals[0] = -1
			}
			if phase == "detail" {
				state.Pending = "aaaa"
			}
			checkpoint, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			pages, err := windowRunFrom(t, p, ledger, &requests, checkpoint)
			wantHeads := int64(1)
			if phase == "detail" {
				wantHeads = 0
			}
			if err != nil || !pages[len(pages)-1].Done || heads.Load() != wantHeads || details.Load() != 1 || !reflect.DeepEqual(ledger.ids, map[string]bool{"1": true, "5": true}) {
				t.Fatalf("resumed numeric tail lost coverage or repeated work: ids=%v heads=%d details=%d err=%v", ledger.ids, heads.Load(), details.Load(), err)
			}
		})
	}
}

func TestWindowAtLeastNumericExhaustionCannotCertifyDeficit(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("provider first ID is bounded by the platform int")
	}
	var requests, roots, heads atomic.Int64
	maximum := int64(math.MaxInt64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.Method == http.MethodHead:
			heads.Add(1)
			if r.URL.Path != "/resolve/"+strconv.FormatInt(maximum, 10) {
				t.Errorf("numeric range wrapped: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Query().Get("order") == "desc":
			// A lower live count cannot replace the original target of two.
			windowList(w, 1, 1, windowItem(1, 1))
		default:
			roots.Add(1)
			windowList(w, 1, 2, windowItem(1, 1))
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.TotalMode = "at_least"
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: int(maximum), DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	pages, err := windowRun(t, p, newWindowLedger(), &requests)
	last := pages[len(pages)-1]
	if FailureCode(err) != "stalled" || last.Done || last.Metadata["coverage_incomplete"] != true || roots.Load() != 2 || heads.Load() != 1 {
		t.Fatalf("numeric exhaustion replaced the target or certified missing IDs: roots=%d heads=%d page=%+v err=%v", roots.Load(), heads.Load(), last, err)
	}
}

func TestWindowArchivedBaseOptionCanBeSkipped(t *testing.T) {
	for _, mode := range []string{"strict", "at_least"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Query().Get("option") != "remaining" {
					http.Error(w, "repeated an archived complete base option", http.StatusTooManyRequests)
					return
				}
				windowList(w, 1, 1, windowItem(3, 1))
			}))
			defer server.Close()
			p := windowFixture(server.URL)
			p.Traversal.TotalMode = mode
			p.Traversal.Options = &model.JSONOptions{URL: "/options", GroupsPath: "/groups", ValuesPath: "/values", ValuePath: "/id", QueryParam: "option"}
			ledger := newWindowLedger()
			source, err := New(t.Context(), p, model.ModeFull, ledger.environment(t))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			collector := source.(*windowJSONConnector)
			state := collector.initialState()
			state.Phase, state.Variant, state.Options, state.Totals = "options_list", -1, []string{"archived", "remaining"}, []int64{3}
			request, err := collector.request(state)
			if err != nil {
				t.Fatal(err)
			}
			ledger.commit(model.Page{
				Metadata: map[string]any{"traversal_phase": "options_list", "fingerprint_scope": collector.fingerprint(state, request), "total": int64(2)},
				Items: []model.Record{
					collector.record(json.RawMessage(windowItem(1, 1)), p.Mapping),
					collector.record(json.RawMessage(windowItem(2, 1)), p.Mapping),
				},
			})
			checkpoint, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			control, err := source.Fetch(t.Context(), checkpoint)
			if err != nil || control.Done || requests.Load() != 0 {
				t.Fatalf("archive proof did not skip the completed base query: page=%+v err=%v", control, err)
			}
			ledger.commit(control)
			remaining, err := source.Fetch(t.Context(), control.Next)
			ledger.commit(remaining)
			if err != nil || requests.Load() != 1 || !reflect.DeepEqual(ledger.ids, map[string]bool{"1": true, "2": true, "3": true}) {
				t.Fatalf("archive pruning lost the remaining option: ids=%v requests=%d err=%v", ledger.ids, requests.Load(), err)
			}
		})
	}
}

func TestWindowAtLeastRecoveryExtendsPastDiscoveryMaximum(t *testing.T) {
	var requests, roots, discoveries, heads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/list" && r.URL.Query().Get("order") == "desc":
			discoveries.Add(1)
			windowList(w, 1, 1, windowItem(3, 2))
		case r.URL.Path == "/list":
			roots.Add(1)
			windowList(w, 1, 2, windowItem(1, 1))
		case r.Method == http.MethodHead:
			id := heads.Add(1) + 1
			if r.URL.Path != "/resolve/"+strconv.FormatInt(id, 10) {
				http.Error(w, "numeric range skipped or repeated an ID", http.StatusServiceUnavailable)
			} else if id == 4 {
				w.Header().Set("Location", "/aaaa")
				w.WriteHeader(http.StatusFound)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case r.URL.Path == "/detail/aaaa":
			_, _ = io.WriteString(w, windowItem(4, 1))
		default:
			http.Error(w, "unexpected recovery request", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	p := windowFixture(server.URL)
	p.Traversal.TotalMode = "at_least"
	p.Traversal.IDRecovery = &model.JSONIDRecovery{First: 1, DiscoveryQuery: map[string]any{"order": "desc"}, ResolveURL: "/resolve/{id}", ResolvePattern: "^[a-f]{4}$", DetailURL: "/detail/{value}", Mapping: p.Mapping}
	ledger := newWindowLedger()
	pages, err := windowRun(t, p, ledger, &requests)
	if err != nil || !pages[len(pages)-1].Done || roots.Load() != 2 || discoveries.Load() != 2 || heads.Load() != 3 || !reflect.DeepEqual(ledger.ids, map[string]bool{"1": true, "4": true}) {
		t.Fatalf("finite range exhaustion refreshed roots or lost later native IDs: ids=%v roots=%d discoveries=%d heads=%d err=%v", ledger.ids, roots.Load(), discoveries.Load(), heads.Load(), err)
	}
}
