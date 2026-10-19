package connectors

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func htmlProvider(endpoint string) model.Provider {
	p := protocolProvider("html", endpoint)
	p.Pagination = model.Pagination{Type: "next", Start: 1}
	p.HTML = &model.HTMLConfig{
		PageSelector: "#catalog", RowsSelector: "article[data-id]", EmptySelector: ".empty", NextSelector: "a[rel=next]",
		ID: model.HTMLField{Attribute: "data-id"},
		Fields: map[string]model.HTMLField{
			"title":     {Selector: ".title", Required: true},
			"info_hash": {Selector: "a.magnet", Attribute: "href", Format: "info_hash"},
			"size":      {Selector: ".size", Format: "size"},
		},
	}
	return p
}

func htmlListing(content string) string {
	return `<html><main id="catalog">` + content + `</main></html>`
}

func htmlRow(id, title string) string {
	return `<article data-id="` + id + `"><span class="title">` + title + `</span></article>`
}

func htmlResponse(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func TestHTMLResumesNextLinksWithoutRetainingCredentialsOrLoadingResources(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.SetCookie(w, &http.Cookie{Name: "consent", Value: "1", Path: "/"})
		if r.URL.Path != "/catalog/list" || r.URL.Query().Get("apikey") != "fixture-secret-value" {
			t.Error("HTML request escaped its listing or lost authentication")
		}
		if r.URL.Query().Get("page") == "2" {
			htmlResponse(w, htmlListing(`<p class="empty">No results</p>`))
			return
		}
		htmlResponse(w, htmlListing(`<article data-id="17"><span class="title">Café &amp; science</span><a class="magnet" href="magnet:?xt=urn:btih:`+strings.Repeat("a", 40)+`&amp;tr=https%3A%2F%2Fannounce.invalid%2Ffixture-secret-value">Magnet</a><span class="size">1,5 GiB</span></article><a rel="next" href="?page=2&amp;apikey=fixture-secret-value">Next</a><img src="/must-not-load"><script>fetch('/must-not-run')</script>`))
	}))
	t.Cleanup(server.Close)
	p := htmlProvider(server.URL + "/catalog/list")
	p.Auth = model.Auth{Type: "query", Name: "apikey", SecretRef: "example_key"}
	first := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets})
	page, err := first.Fetch(t.Context(), nil)
	if err != nil || page.Done || len(page.Items) != 1 || page.Items[0].SourceID != "17" || page.Items[0].Fields["title"] != "Café & science" || page.Items[0].Fields["size"] != int64(1610612736) {
		t.Fatalf("HTML listing lost its interpreted fields: %+v %v", page, err)
	}
	encoded, _ := json.Marshal(page.Items[0].Fields)
	if strings.Contains(string(encoded), "announce") || strings.Contains(string(page.Next), "apikey") || strings.Contains(string(page.Next), "fixture-secret") {
		t.Fatal("HTML mapping retained magnet trackers or request credentials")
	}
	_ = first.Close()
	resumed := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets})
	last, err := resumed.Fetch(t.Context(), page.Next)
	if err != nil || !last.Done || len(last.Items) != 0 || requests.Load() != 2 {
		t.Fatalf("confirmed empty page did not finish after resume: %+v %v", last, err)
	}
	if _, err := resumed.Fetch(t.Context(), last.Next); err != nil || requests.Load() != 2 {
		t.Fatal("completed HTML checkpoint made another request")
	}
}

func TestHTMLRejectsUnconfirmedOrUnsafeResponsesWithoutAdvancing(t *testing.T) {
	valid := htmlRow("1", "Example film")
	for _, test := range []struct{ name, body, reason string }{
		{"login", `<form>Sign in</form>`, "html_listing_missing"},
		{"changed markup", htmlListing(`<section class="changed">Unrecognized rows</section>`), "html_empty_unconfirmed"},
		{"contradictory empty marker", htmlListing(valid + `<p class="empty">Empty</p>`), "html_empty_unconfirmed"},
		{"missing title", htmlListing(`<article data-id="1"></article>`), "html_field_invalid"},
		{"missing identity", htmlListing(`<article data-id=""><span class="title">Example</span></article>`), "html_identity_invalid"},
		{"duplicate identity", htmlListing(valid + valid), "html_identity_invalid"},
		{"invalid size", htmlListing(valid + `<article data-id="2"><span class="title">Other</span><span class="size">unknown</span></article>`), "html_field_invalid"},
		{"external next", htmlListing(valid + `<a rel="next" href="https://outside.invalid/page/2">Next</a>`), "invalid_continuation"},
		{"unsafe next scheme", htmlListing(valid + `<a rel="next" href="javascript:alert(1)">Next</a>`), "invalid_continuation"},
		{"credential next", htmlListing(valid + `<a rel="next" href="?page=2&amp;passkey=private-value">Next</a>`), "invalid_continuation"},
		{"session next", htmlListing(valid + `<a rel="next" href="?page=2&amp;session_id=private-value">Next</a>`), "invalid_continuation"},
		{"repeated next", htmlListing(valid + `<a rel="next" href="#">Next</a>`), "continuation_repeated"},
		{"ambiguous next", htmlListing(valid + `<a rel="next" href="?page=2">Next</a><a rel="next" href="?page=3">Next</a>`), "invalid_continuation"},
		{"empty continuation", htmlListing(`<p class="empty">Empty</p><a rel="next" href="?page=2">Next</a>`), "unexpected_continuation"},
		{"external base", `<base href="https://outside.invalid/">` + htmlListing(valid), "html_listing_missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				htmlResponse(w, test.body)
			}))
			t.Cleanup(server.Close)
			checkpoint := json.RawMessage(`{"version":1,"page":3,"offset":0,"count":2}`)
			page, err := protocolOpen(t, htmlProvider(server.URL), model.ModeFull, Environment{}).Fetch(t.Context(), checkpoint)
			if err == nil || page.Done || string(page.Next) != string(checkpoint) || page.Metadata["failure_reason"] != test.reason || page.Error != FailureReasonMessage(test.reason) || requests.Load() != 1 {
				t.Fatalf("HTML failure advanced or lost safe diagnostics: reason=%v error=%v done=%v requests=%d", page.Metadata["failure_reason"], err, page.Done, requests.Load())
			}
		})
	}
}

func TestHTMLRedirectBaseAndDetailFieldsShareTheSourceClient(t *testing.T) {
	var requests, boundaries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer fixture-secret-value" {
			t.Error("detail request did not use source authentication")
		}
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/catalog/list", http.StatusFound)
		case "/catalog/list":
			htmlResponse(w, `<base href="/catalog/">`+htmlListing(`<article data-id="1"><span class="title">Listing title</span><a class="details" href="../items/1">Details</a></article><a rel="next" href="last">Next</a>`))
		case "/items/1":
			htmlResponse(w, `<main id="details"><span class="title">Detail title</span><a class="magnet" href="magnet:?xt=urn:btih:`+strings.Repeat("b", 40)+`">Magnet</a></main>`)
		case "/catalog/last":
			htmlResponse(w, htmlListing(`<p class="empty">Empty</p>`))
		default:
			t.Error("HTML link resolved against the wrong document")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	p := htmlProvider(server.URL + "/start")
	p.Auth = model.Auth{Type: "bearer", SecretRef: "key"}
	p.HTML.Details = &model.HTMLDetails{URL: model.HTMLField{Selector: "a.details", Attribute: "href"}, PageSelector: "#details", Fields: map[string]model.HTMLField{"info_hash": {Selector: "a.magnet", Attribute: "href", Format: "info_hash", Required: true}, "title": {Selector: ".title"}}}
	source := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets, Checkpoint: func(_ context.Context, page model.Page) error {
		if page.Next != nil || page.Done || page.Metadata["auxiliary_response"] != true {
			t.Error("intermediate HTML response advanced a data checkpoint")
		}
		boundaries = append(boundaries, page.Metadata["html_phase"].(string))
		return nil
	}})
	page, err := source.Fetch(t.Context(), nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Fields["title"] != "Listing title" || page.Items[0].Fields["info_hash"] != strings.Repeat("b", 40) || page.Metadata["detail_response"] != true || !slices.Equal(boundaries, []string{"listing"}) {
		t.Fatalf("detail enrichment changed listing fields or checkpoints: %+v %v", page, err)
	}
	last, err := source.Fetch(t.Context(), page.Next)
	if err != nil || !last.Done || !slices.Equal(requests, []string{"/start", "/catalog/list", "/items/1", "/catalog/last"}) {
		t.Fatalf("HTML redirect/base resolution failed: %v %v", requests, err)
	}
}

func TestHTMLDetailFailureAndCheckpointFailureNeverCommitPartialListings(t *testing.T) {
	for _, boundaryFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpoint_failure_%v", boundaryFailure), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == "/detail" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				htmlResponse(w, htmlListing(`<article data-id="1"><span class="title">Example</span><a class="details" href="/detail">Details</a></article>`))
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.HTML.Details = &model.HTMLDetails{URL: model.HTMLField{Selector: ".details", Attribute: "href"}, PageSelector: "#detail", Fields: map[string]model.HTMLField{"info_hash": {Selector: ".hash", Required: true}}}
			env := Environment{}
			if boundaryFailure {
				env.Checkpoint = func(context.Context, model.Page) error { return errors.New("private diagnostic must not escape") }
			}
			page, err := protocolOpen(t, p, model.ModeFull, env).Fetch(t.Context(), nil)
			wantRequests := int32(2)
			if boundaryFailure {
				wantRequests = 1
			} else if FailureCode(err) != "authentication" {
				t.Error("detail authentication failure was obscured")
			}
			if err == nil || page.Next != nil || page.Done || len(page.Items) != 0 || strings.Contains(page.Error, "private") || requests.Load() != wantRequests {
				t.Fatalf("failed detail work advanced a listing: error=%v next=%s requests=%d", err, page.Next, requests.Load())
			}
		})
	}
}

func TestHTMLFormPaginationAndChecks(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.PostForm.Get("category") != "all" || r.PostForm.Get("size") != "2" || r.URL.Query().Get("sort") != "newest" {
			t.Error("HTML form query/body handling diverged")
		}
		page := r.PostForm.Get("page")
		content := htmlRow(page, "Example "+page)
		htmlResponse(w, htmlListing(content)+`<span id="current">`+page+`</span><span id="total">2</span>`)
	}))
	t.Cleanup(server.Close)
	p := htmlProvider(server.URL)
	p.HTTP = model.HTTPConfig{Method: http.MethodPost, Body: map[string]any{"category": "all"}, Query: map[string]any{"sort": "oldest"}, IncrementalQuery: map[string]any{"sort": "newest"}}
	p.Pagination = model.Pagination{Type: "page", In: "body", PageParam: "page", SizeParam: "size", Start: 1}
	p.HTML.NextSelector = ""
	p.HTML.Current, p.HTML.Total = &model.HTMLField{Selector: "#current"}, &model.HTMLField{Selector: "#total"}
	source := protocolOpen(t, p, model.ModeIncremental, Environment{})
	first, err := source.Fetch(t.Context(), nil)
	if err != nil || first.Done {
		t.Fatalf("HTML form first page failed: %+v %v", first, err)
	}
	last, err := source.Fetch(t.Context(), first.Next)
	if err != nil || !last.Done || requests.Load() != 2 || last.Items[0].SourceID != "2" {
		t.Fatalf("HTML form totals did not complete: %+v %v", last, err)
	}
}

func TestHTMLTorrentMetadataRetainsOnlyTheExactInfoHash(t *testing.T) {
	const info = "d6:lengthi100e4:name11:example.txt12:piece lengthi16384e6:pieces20:abcdefghijklmnopqrste"
	const torrent = "d8:announce36:https://announce.invalid/private-key4:info" + info + "e"
	expected := sha1.Sum([]byte(info))
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid_%v", invalid), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/file" {
					if r.URL.Query().Get("passkey") != "metadata-secret" {
						t.Error("torrent metadata request lost its private query")
					}
					w.Header().Set("Content-Type", "application/x-bittorrent")
					if invalid {
						_, _ = w.Write([]byte(`<html>Temporary sign-in page</html>`))
					} else {
						_, _ = w.Write([]byte(torrent))
					}
					return
				}
				htmlResponse(w, htmlListing(`<article data-id="1"><span class="title">Example</span><a class="file" href="/file?passkey=metadata-secret">File</a></article>`))
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.HTML.Fields["info_hash"] = model.HTMLField{Selector: "a.file", Attribute: "href", Format: "torrent", Required: true}
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
			if invalid {
				if err == nil || page.Next != nil || page.Done || page.Metadata["failure_reason"] != "html_torrent_invalid" {
					t.Fatalf("HTML download page was accepted as torrent metadata: %v", err)
				}
				return
			}
			if err != nil || !page.Done || len(page.Items) != 1 || page.Items[0].Fields["info_hash"] != hex.EncodeToString(expected[:]) {
				t.Fatalf("torrent-file extraction did not hash exact info bytes: %+v %v", page.Items, err)
			}
			fields, _ := json.Marshal(page.Items[0].Fields)
			if strings.Contains(string(fields), "announce") || strings.Contains(string(fields), "secret") || strings.Contains(string(page.Next), "secret") || strings.Contains(string(fields), "example.txt") {
				t.Fatal("torrent metadata retained an announce, credential or filename")
			}
		})
	}
}

func TestHTMLSizesDatesAndSelectorPatterns(t *testing.T) {
	for _, test := range []struct {
		value string
		bytes int64
	}{
		{"0", 0}, {"1,024", 1024}, {"1.5 GiB", 1610612736}, {"1,5 GB", 1500000000}, {"9007199254740993", 9007199254740993}, {"1 KiB", 1024}, {"1 kB", 1000},
		{"1,5 Go", 1500000000}, {"1,5\u00a0Gio", 1610612736}, {"42 octets", 42},
	} {
		got, err := htmlBytes(test.value)
		if err != nil || got != test.bytes {
			t.Errorf("size %q: got %d %v, want %d", test.value, got, err, test.bytes)
		}
	}
	for _, value := range []string{"-1 GB", "1e9", "9 EiB", "1.5", "NaN GiB"} {
		if _, err := htmlBytes(value); err == nil {
			t.Errorf("accepted invalid size %q", value)
		}
	}
	field, err := compileHTMLField(model.HTMLField{Format: "date", DateLayout: "02/01/2006 15:04", Timezone: "Europe/Paris"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := field.convert("19/10/2026 16:00", nil, "")
	if err != nil || got != "2026-10-19T14:00:00Z" {
		t.Fatalf("date did not use the configured timezone: %v %v", got, err)
	}
}

func TestHTMLPaginationChecksRejectContradictions(t *testing.T) {
	for _, test := range []struct{ name, rows, total, current, next, reason string }{
		{"bad current", htmlRow("1", "Example"), "1", "2", "", "position_mismatch"},
		{"bad total", htmlRow("1", "Example"), "unknown", "1", "", "invalid_total"},
		{"excess records", htmlRow("1", "Example"), "0", "1", "", "records_exceed_total"},
		{"early last page", htmlRow("1", "Example"), "2", "1", "", "premature_end"},
		{"early empty page", `<p class="empty">Empty</p>`, "2", "1", "", "premature_end"},
		{"continuation at total", htmlRow("1", "Example"), "1", "1", `<a rel="next" href="?page=2">Next</a>`, "unexpected_continuation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				htmlResponse(w, htmlListing(test.rows)+`<span id="total">`+test.total+`</span><span id="current">`+test.current+`</span>`+test.next)
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.HTML.Total, p.HTML.Current = &model.HTMLField{Selector: "#total"}, &model.HTMLField{Selector: "#current"}
			page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
			if err == nil || page.Next != nil || page.Done || page.Metadata["failure_reason"] != test.reason {
				t.Fatalf("HTML pagination contradiction was accepted: reason=%v error=%v", page.Metadata["failure_reason"], err)
			}
		})
	}
}

func TestHTMLFieldListsCharsetsAndNativePatterns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		_, _ = w.Write([]byte(htmlListing(`<article data-id="item-123"><span class="title">Caf` + string([]byte{0xe9}) + ` <script>must not be interpreted</script>review</span><a class="cat" data-category="10">First</a><a class="cat" data-category="20">Second</a><span class="ignored-count">99</span></article><a rel="next" disabled href="?page=2">Next</a>`)))
	}))
	t.Cleanup(server.Close)
	p := htmlProvider(server.URL)
	p.HTML.ID.Pattern = `item-([0-9]+)`
	p.HTML.Fields["categories"] = model.HTMLField{Selector: ".cat", Attribute: "data-category", Format: "integer", All: true}
	p.HTML.Fields["leechers"] = model.HTMLField{Selector: ".ignored-count", Required: true}
	p.Options = map[string]any{"local_categories": []any{json.Number("20")}}
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if err != nil || !page.Done || len(page.Items) != 1 || page.Items[0].SourceID != "123" || page.Items[0].Ignored || page.Items[0].Fields["title"] != "Café review" {
		t.Fatalf("HTML charset, native pattern or local filtering failed: %+v %v", page.Items, err)
	}
	categories, _ := page.Items[0].Fields["categories"].([]int64)
	if !slices.Equal(categories, []int64{10, 20}) || page.Items[0].Fields["leechers"] != nil {
		t.Fatal("HTML category arrays or retired-counter policy diverged")
	}
}

func TestHTMLRejectsBadContinuationBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		htmlResponse(w, htmlListing(htmlRow("1", "Example")))
	}))
	t.Cleanup(server.Close)
	for _, checkpoint := range []string{`{"version":9}`, `{"version":1,"page":-1}`, `{"version":1,"url":"https://outside.invalid/catalog"}`, `{"version":1,"url":"/catalog?passkey=private"}`} {
		page, err := protocolOpen(t, htmlProvider(server.URL), model.ModeFull, Environment{}).Fetch(t.Context(), json.RawMessage(checkpoint))
		if err == nil || requests.Load() != 0 || string(page.Next) != checkpoint {
			t.Fatal("invalid HTML checkpoint was followed or rewritten")
		}
	}
}

func TestHTMLOffsetPaginationHonorsTheStartingPositionAndTotal(t *testing.T) {
	for _, start := range []int{0, 10} {
		t.Run(fmt.Sprint(start), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				if err != nil || r.URL.Query().Get("limit") != "2" {
					t.Error("HTML offset request omitted its position or size")
				}
				var rows string
				for id := offset; id < offset+2 && id < start+3; id++ {
					rows += htmlRow(strconv.Itoa(id), "Example")
				}
				htmlResponse(w, htmlListing(rows)+fmt.Sprintf(`<span id="current">%d</span><span id="total">%d</span>`, offset, start+3))
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.Pagination = model.Pagination{Type: "offset", OffsetParam: "offset", SizeParam: "limit", Start: start}
			p.HTML.NextSelector = ""
			p.HTML.Total, p.HTML.Current = &model.HTMLField{Selector: "#total"}, &model.HTMLField{Selector: "#current"}
			source := protocolOpen(t, p, model.ModeFull, Environment{})
			first, err := source.Fetch(t.Context(), nil)
			if err != nil || first.Done || len(first.Items) != 2 {
				t.Fatalf("HTML offset first page failed: %v", err)
			}
			last, err := source.Fetch(t.Context(), first.Next)
			if err != nil || !last.Done || len(last.Items) != 1 || requests.Load() != 2 {
				t.Fatalf("HTML offset total did not finish: %v", err)
			}
		})
	}
}

func TestHTMLMappedTextCannotGrowWithoutABound(t *testing.T) {
	var rows string
	for id := range 6 {
		rows += htmlRow(strconv.Itoa(id), strings.Repeat("x", 60000))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { htmlResponse(w, htmlListing(rows)) }))
	t.Cleanup(server.Close)
	p := htmlProvider(server.URL)
	p.HTML.Fields = map[string]model.HTMLField{"title": {Selector: ".title"}}
	for id := range model.MaxHTMLFields - 1 {
		p.HTML.Fields[fmt.Sprintf("field_%d", id)] = model.HTMLField{Selector: ".title"}
	}
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if err == nil || page.Next != nil || page.Done || page.Metadata["failure_reason"] != "html_fields_limit" {
		t.Fatal("repeated mappings could inflate a small HTML response without a bound")
	}
}

func TestHTMLTorrentV2HybridAndLargeIntegerMetadataKeepExactIdentities(t *testing.T) {
	const tree = "d7:exampled0:d6:lengthi0eeee"
	const version2 = "d9:file tree" + tree + "12:meta versioni2e4:name7:example12:piece lengthi16384ee"
	const hybrid = "d9:file tree" + tree + "6:lengthi0e12:meta versioni2e4:name7:example12:piece lengthi16384e6:pieces0:e"
	for _, info := range []string{version2, hybrid} {
		torrent := "d5:extrai" + strings.Repeat("9", 100) + "e4:info" + info + "12:piece layersdee"
		hash, err := htmlTorrentHash([]byte(torrent))
		if err != nil {
			t.Fatalf("valid exact metainfo was rejected: %v", err)
		}
		if info == hybrid {
			want := sha1.Sum([]byte(info))
			if hash != hex.EncodeToString(want[:]) {
				t.Fatal("hybrid torrent did not retain its compatible v1 identity")
			}
		} else {
			want := sha256.Sum256([]byte(info))
			if hash != hex.EncodeToString(want[:]) {
				t.Fatal("v2-only metainfo did not retain its exact SHA-256 identity")
			}
		}
	}
}
