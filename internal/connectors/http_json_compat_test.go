package connectors

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestJSONMetadataNFOPreservesBinaryTextAndOtherProjections(t *testing.T) {
	for _, test := range []struct {
		name string
		nfo  string
	}{
		{"text with literal escape", "  NFO π \\u0000\r\n"},
		{"binary text", "\x00 NFO π \\u0000\r\n\x00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := map[string]any{"nfoContent": test.nfo, "externalId": json.Number("9007199254740993")}
			raw, err := json.Marshal(map[string]any{"id": "1", "metadata": original})
			if err != nil {
				t.Fatal(err)
			}
			_, fields, err := FieldsFromJSON(raw, model.Mapping{ID: "/id", Fields: map[string]string{
				"metadata": "/metadata", "original": "/metadata",
			}}, "")
			if err != nil {
				t.Fatal(err)
			}
			metadata, ok := fields["metadata"].(map[string]any)
			if !ok || metadata["externalId"] != original["externalId"] {
				t.Fatalf("NFO normalization changed unrelated metadata: %+v", fields)
			}
			if strings.IndexByte(test.nfo, 0) < 0 {
				if metadata["nfoContent"] != test.nfo {
					t.Fatalf("ordinary NFO text was transformed: %+v", metadata)
				}
			} else {
				encoded, ok := metadata["nfoContent"].(map[string]any)
				if !ok || encoded["encoding"] != "base64" {
					t.Fatalf("binary NFO has no explicit reversible encoding: %+v", metadata)
				}
				data, ok := encoded["data"].(string)
				if !ok {
					t.Fatalf("encoded NFO is not a string: %+v", encoded)
				}
				decoded, err := base64.StdEncoding.DecodeString(data)
				if err != nil || string(decoded) != test.nfo {
					t.Fatalf("binary NFO changed after decoding: %q %v", decoded, err)
				}
			}
			if !reflect.DeepEqual(fields["original"], original) {
				t.Fatalf("NFO normalization mutated another mapping of the source: %+v", fields["original"])
			}
		})
	}
}

func TestHTTPJSONNextLinkRetainsFiltersAndPageSize(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	encodedHash := hex.EncodeToString([]byte(strings.ToUpper(hash)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/api/torrents/filter" || !reflect.DeepEqual(query["categories[]"], []string{"1", "2", "6", "7", "8", "9"}) || query.Get("sortField") != "created_at" || query.Get("sortDirection") != "asc" || query.Get("perPage") != "100" {
			t.Errorf("continuation changed the selected listing: %s", r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch query.Get("page") {
		case "1":
			fmt.Fprintf(w, `{"data":[{"id":"9007199254740993","attributes":{"name":"First","info_hash":%q}}],"meta":{"current_page":1},"links":{"next":"/api/torrents/filter?page=2"}}`, encodedHash)
		case "2":
			fmt.Fprintf(w, `{"data":[{"id":"9007199254740994","attributes":{"name":"Second","info_hash":%q}}],"meta":{"current_page":2},"links":{"next":null}}`, hash)
		default:
			t.Errorf("unexpected page: %s", r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	var p model.Provider
	decoder := json.NewDecoder(strings.NewReader(unit3dTemplate()))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&p); err != nil {
		t.Fatal(err)
	}
	p.URL = server.URL + "/api/torrents/filter"
	p.Auth = model.Auth{Type: "none"}
	p.RequestInterval = "1ms"
	p.PageSize = 100
	p.HTTP.Query["categories[]"] = []any{json.Number("1"), json.Number("2"), json.Number("6"), json.Number("7"), json.Number("8"), json.Number("9")}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	first, err := connector.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 1 {
		t.Fatalf("first short page must continue to its explicit link: page=%+v err=%v", first, err)
	}
	second, err := connector.Fetch(t.Context(), first.Next)
	if err != nil || !second.Done || len(second.Items) != 1 {
		t.Fatalf("second page must finish at null next link: page=%+v err=%v", second, err)
	}
	for i, page := range []model.Page{first, second} {
		record := page.Items[0]
		wantID := []string{"9007199254740993", "9007199254740994"}[i]
		if record.SourceID != wantID || record.Error != "" || record.Ignored || record.Fields["info_hash"] != hash {
			t.Errorf("distinct native identity or normalized shared hash lost: %+v", record)
		}
	}
}

func TestHTTPJSONIndexedNextLinks(t *testing.T) {
	categories := []string{"1", "2", "6", "7", "8", "9"}
	continuation := func(page, copies int) url.Values {
		query := url.Values{
			"page": {strconv.Itoa(page)}, "perPage": {"25"},
			"sortField": {"name"}, "sortDirection": {"desc"},
			"opaque": {"A+/=&"}, "other[0]": {"keep"},
			"categories": {"bare"}, "categories[label]": {"named"}, "categories[0][label]": {"nested"},
		}
		for i := range copies * len(categories) {
			query[fmt.Sprintf("categories[%d]", i)] = []string{categories[i%len(categories)]}
		}
		return query
	}
	for _, test := range []struct {
		name     string
		preserve bool
		start    int
	}{
		{name: "successive indexed links", preserve: true, start: 1},
		{name: "accumulated checkpoint", preserve: true, start: 3},
		{name: "authoritative opt-out", start: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step, err := strconv.Atoi(r.URL.Query().Get("page"))
				if err != nil || step < test.start || step > 4 {
					t.Errorf("invalid continuation page: %s", r.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				want := continuation(step, 1)
				if step == 1 {
					want = url.Values{"page": {"1"}}
				}
				if step == 1 || test.preserve {
					for i := range categories {
						want.Del(fmt.Sprintf("categories[%d]", i))
					}
					want["categories[]"] = categories
					want.Set("sortField", "created_at")
					want.Set("sortDirection", "asc")
					want.Set("perPage", "100")
				}
				if !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("page %d query: got %v, want %v", step, r.URL.Query(), want)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var next any
				if step < 4 {
					next = "/api/torrents/filter?" + continuation(step+1, 1).Encode()
				}
				encodedNext, _ := json.Marshal(next)
				fmt.Fprintf(w, `{"data":[{"id":%d}],"current_page":%d,"next":%s}`, step, step, encodedNext)
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL+"/api/torrents/filter")
			p.PageSize = 100
			p.HTTP.ItemsPath = "/data"
			p.HTTP.Query = map[string]any{"categories[]": []any{1, 2, 6, 7, 8, 9}, "sortField": "created_at", "sortDirection": "asc"}
			p.Pagination = model.Pagination{Type: "page", In: "query", PageParam: "page", SizeParam: "perPage", Start: 1, CurrentPath: "/current_page", NextPath: "/next"}
			p.Mapping = model.Mapping{ID: "/id"}
			p.Options = map[string]any{"preserve_query_on_next": test.preserve}
			connector := protocolOpen(t, p, model.ModeFull, Environment{})
			var checkpoint json.RawMessage
			if test.start > 1 {
				var err error
				checkpoint, err = json.Marshal(jsonCursor{Version: 1, Page: test.start, Offset: test.start - 1, URL: server.URL + "/api/torrents/filter?" + continuation(test.start, 2).Encode()})
				if err != nil {
					t.Fatal(err)
				}
			}
			for step := test.start; step <= 4; step++ {
				page, err := connector.Fetch(t.Context(), checkpoint)
				if err != nil || page.Done != (step == 4) || len(page.Items) != 1 {
					t.Fatalf("page %d: page=%+v err=%v", step, page, err)
				}
				if page.Items[0].SourceID != strconv.Itoa(step) {
					t.Fatalf("page %d returned the wrong record: %+v", step, page.Items[0])
				}
				checkpoint = page.Next
			}
		})
	}
}

func TestHTTPJSONRejectsInvalidASCIIHexInfoHashes(t *testing.T) {
	for name, hash := range map[string]string{
		"invalid outer encoding": strings.Repeat("zz", 40),
		"decoded non-hash":       hex.EncodeToString([]byte(strings.Repeat("g", 40))),
		"decoded non-ASCII":      strings.Repeat("ff", 40),
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"id":"51","info_hash":%q}`, hash))
			id, fields, err := FieldsFromJSON(raw, model.Mapping{ID: "/id", Fields: map[string]string{"info_hash": "/info_hash"}}, "")
			if err == nil || id != "51" {
				t.Fatalf("invalid encoded hash accepted or identity discarded: id=%q fields=%v err=%v", id, fields, err)
			}
			if _, exists := fields["info_hash"]; exists {
				t.Fatalf("invalid encoded value published as a hash: %v", fields)
			}
		})
	}
}

func TestHTTPJSONLocalCategoriesPreserveRawAndContinuation(t *testing.T) {
	const included = `{"id":11,"category":1,"vendor":{"keep":"original"}}`
	const excluded = `{"id":12,"category":5,"vendor":{"keep":"excluded"}}`
	const later = `{"id":13,"category":2}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprintf(w, `{"data":[%s,%s],"next":"?page=2"}`, included, excluded)
		case "2":
			fmt.Fprintf(w, `{"data":[%s],"next":null}`, later)
		default:
			t.Errorf("unexpected page: %s", r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("http_json", server.URL)
	p.HTTP.ItemsPath = "/data"
	p.Pagination = model.Pagination{Type: "page", Start: 1, NextPath: "/next"}
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"categories": "/category"}}
	p.Options = map[string]any{"local_categories": []any{1, 2}}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	first, err := connector.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 2 {
		t.Fatalf("filtering truncated the source page or continuation: page=%+v err=%v", first, err)
	}
	if first.Items[0].SourceID != "11" || first.Items[0].Ignored || first.Items[0].Error != "" || first.Items[1].SourceID != "12" || !first.Items[1].Ignored || first.Items[1].Error != "" {
		t.Fatalf("local category selection did not distinguish source identities: %+v", first.Items)
	}
	if string(first.Items[0].Raw) != included || string(first.Items[1].Raw) != excluded {
		t.Fatalf("local filtering changed original records: %+v", first.Items)
	}
	second, err := connector.Fetch(t.Context(), first.Next)
	if err != nil || !second.Done || len(second.Items) != 1 || second.Items[0].SourceID != "13" || second.Items[0].Ignored || second.Items[0].Error != "" || string(second.Items[0].Raw) != later {
		t.Fatalf("later selected category was lost: page=%+v err=%v", second, err)
	}
}

func TestHTTPJSONNullItemsOnlyWithExplicitCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		enabled bool
		wantErr bool
	}{
		{name: "explicit null EOF", body: `{"data":null}`, enabled: true},
		{name: "null requires opt-in", body: `{"data":null}`, wantErr: true},
		{name: "missing is not null", body: `{}`, enabled: true, wantErr: true},
		{name: "object is not null", body: `{"data":{}}`, enabled: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/data"
			p.Pagination = model.Pagination{Type: "page", Start: 1}
			if tc.enabled {
				p.Options = map[string]any{"null_items_as_empty": true}
			}
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
			if string(page.Body) != tc.body {
				t.Fatalf("original null or malformed response lost: %q", page.Body)
			}
			if tc.wantErr {
				if err == nil || page.Error == "" || page.Done {
					t.Fatalf("invalid response certified completion: page=%+v err=%v", page, err)
				}
				return
			}
			if err != nil || !page.Done || len(page.Items) != 0 || !json.Valid(page.Next) {
				t.Fatalf("explicit null EOF not checkpointed: page=%+v err=%v", page, err)
			}
		})
	}
}

func TestHTTPJSONTotalGrowthRequiresExplicitAscendingFeedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		allowGrowth           bool
		firstTotal, nextTotal int
		nextItems             string
		wantError             bool
	}{
		{name: "strict rejects growth", firstTotal: 3, nextTotal: 4, nextItems: `[{"id":3},{"id":4}]`, wantError: true},
		{name: "growth follows latest total", allowGrowth: true, firstTotal: 3, nextTotal: 4, nextItems: `[{"id":3},{"id":4}]`},
		{name: "decrease remains unsafe", allowGrowth: true, firstTotal: 4, nextTotal: 3, nextItems: `[{"id":3}]`, wantError: true},
		{name: "empty page cannot satisfy growth", allowGrowth: true, firstTotal: 3, nextTotal: 4, nextItems: `[]`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("offset") {
				case "0":
					fmt.Fprintf(w, `{"results":[{"id":1},{"id":2}],"start":0,"total":%d}`, tc.firstTotal)
				case "2":
					fmt.Fprintf(w, `{"results":%s,"start":2,"total":%d}`, tc.nextItems, tc.nextTotal)
				default:
					http.Error(w, "unexpected offset", http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/results"
			p.Pagination = model.Pagination{Type: "offset", CurrentPath: "/start", TotalPath: "/total"}
			p.Options = map[string]any{"allow_total_growth": tc.allowGrowth}
			firstReader := protocolOpen(t, p, model.ModeFull, Environment{})
			first, err := firstReader.Fetch(t.Context(), nil)
			if err != nil || first.Done {
				t.Fatalf("initial page did not checkpoint: page=%+v err=%v", first, err)
			}
			_ = firstReader.Close()
			resumed := protocolOpen(t, p, model.ModeFull, Environment{})
			last, err := resumed.Fetch(t.Context(), first.Next)
			if tc.wantError {
				if err == nil || last.Done || string(last.Next) != string(first.Next) {
					t.Fatalf("unsafe page committed: page=%+v err=%v", last, err)
				}
				return
			}
			if err != nil || !last.Done || len(last.Items) != 2 || last.Items[0].SourceID != "3" || last.Items[1].SourceID != "4" {
				t.Fatalf("newly appended release lost after resume: page=%+v err=%v", last, err)
			}
		})
	}
}

func TestHTTPJSONIncrementalTotalDecreasePreservesPaginationChecks(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        model.RunMode
		allowGrowth bool
		current     int
		total       int
		items       string
		wantError   bool
	}{
		{name: "opted-in incremental accepts decrease", mode: model.ModeIncremental, allowGrowth: true, current: 2, total: 3, items: `[{"id":2}]`},
		{name: "strict incremental rejects decrease", mode: model.ModeIncremental, current: 2, total: 3, items: `[{"id":2}]`, wantError: true},
		{name: "preview still rejects decrease", mode: model.ModePreview, allowGrowth: true, current: 2, total: 3, items: `[{"id":2}]`, wantError: true},
		{name: "incremental rejects wrong position", mode: model.ModeIncremental, allowGrowth: true, current: 3, total: 3, items: `[{"id":2}]`, wantError: true},
		{name: "incremental rejects records beyond total", mode: model.ModeIncremental, allowGrowth: true, current: 2, total: 2, items: `[{"id":2}]`, wantError: true},
		{name: "incremental rejects premature empty page", mode: model.ModeIncremental, allowGrowth: true, current: 2, total: 3, items: `[]`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("offset") {
				case "0":
					fmt.Fprint(w, `{"results":[{"id":4},{"id":3}],"start":0,"total":4}`)
				case "2":
					fmt.Fprintf(w, `{"results":%s,"start":%d,"total":%d}`, tc.items, tc.current, tc.total)
				default:
					http.Error(w, "unexpected offset", http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			p := protocolProvider("http_json", server.URL)
			p.HTTP.ItemsPath = "/results"
			p.Pagination = model.Pagination{Type: "offset", CurrentPath: "/start", TotalPath: "/total"}
			p.Options = map[string]any{"allow_total_growth": tc.allowGrowth}
			firstReader := protocolOpen(t, p, tc.mode, Environment{})
			first, err := firstReader.Fetch(t.Context(), nil)
			if err != nil || first.Done {
				t.Fatalf("initial page did not checkpoint: page=%+v err=%v", first, err)
			}
			_ = firstReader.Close()
			resumed := protocolOpen(t, p, tc.mode, Environment{})
			last, err := resumed.Fetch(t.Context(), first.Next)
			if tc.wantError {
				if FailureCode(err) != "stalled" || last.Done || string(last.Next) != string(first.Next) {
					t.Fatalf("invalid pagination committed after resume: page=%+v err=%v", last, err)
				}
				return
			}
			if err != nil || !last.Done || len(last.Items) != 1 || last.Items[0].SourceID != "2" {
				t.Fatalf("variable total blocked a valid incremental page: page=%+v err=%v", last, err)
			}
		})
	}
}

func TestHTTPJSONRejectsInvalidUTF8WithoutReplacingSourceIdentity(t *testing.T) {
	body := "{\"items\":[{\"id\":\"release-\xff\"},{\"id\":\"release-\xfe\"}]}"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("http_json", server.URL)
	p.HTTP.ItemsPath = "/items"
	p.Pagination.Type = "none"
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if FailureCode(err) != "parse" || page.Done || len(page.Next) != 0 || string(page.Body) != body || len(page.Items) != 0 {
		t.Fatalf("malformed identities were replaced or traversal advanced: page=%#v err=%v", page, err)
	}
}

func TestHTTPJSONIncrementalQuerySurvivesNextURLsWithoutChangingFull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != "published" || query.Get("sort_by") != "added_date" ||
			query.Get("limit") != "2" || !reflect.DeepEqual(query["categories[]"], []string{"1", "2"}) ||
			query.Has("categories[0]") {
			http.Error(w, "continuation changed catalogue filters", http.StatusBadRequest)
			return
		}
		offset, _ := strconv.Atoi(query.Get("offset"))
		first, second := offset+1, offset+2
		if query.Get("order") == "desc" {
			first, second = 4-offset, 3-offset
		}
		next := "null"
		if offset == 0 {
			next = `"/list?offset=2&order=asc&state=hidden&limit=1&categories%5B0%5D=999"`
		}
		_, _ = fmt.Fprintf(w, `{"results":[{"id":%d},{"id":%d}],"start":%d,"total":4,"next":%s}`, first, second, offset, next)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("http_json", server.URL+"/list")
	p.HTTP.ItemsPath = "/results"
	p.HTTP.Query = map[string]any{"order": "asc", "sort_by": "added_date", "state": "published", "categories[]": []any{1, 2}}
	p.HTTP.IncrementalQuery = map[string]any{"order": "desc"}
	p.Pagination = model.Pagination{Type: "offset", CurrentPath: "/start", TotalPath: "/total", NextPath: "/next"}
	p.Options = map[string]any{"allow_total_growth": true}
	p.Schedule.KnownPages = 2
	reader := protocolOpen(t, p, model.ModeIncremental, Environment{})
	first, err := reader.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 2 || first.Items[0].SourceID != "4" || first.Items[1].SourceID != "3" {
		t.Fatalf("incremental did not begin with newest native identities: %+v %v", first, err)
	}
	_ = reader.Close()
	resumed := protocolOpen(t, p, model.ModeIncremental, Environment{})
	last, err := resumed.Fetch(t.Context(), first.Next)
	if err != nil || !last.Done || len(last.Items) != 2 || last.Items[0].SourceID != "2" || last.Items[1].SourceID != "1" {
		t.Fatalf("continuation restored the Full ordering or changed filters: %+v %v", last, err)
	}
	full := protocolOpen(t, p, model.ModeFull, Environment{})
	page, err := full.Fetch(t.Context(), nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].SourceID != "1" || page.Items[1].SourceID != "2" {
		t.Fatalf("opening Incremental mutated the source's Full ordering: %+v %v", page, err)
	}
}
