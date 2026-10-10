package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func metadataFixture(endpoint string) model.Provider {
	p := orderedFixture(endpoint)
	p.Traversal.EnrichFields = []string{"external_ids"}
	fields := make(map[string]string, len(p.Mapping.Fields)+2)
	for name, path := range p.Mapping.Fields {
		fields[name] = path
	}
	fields["external_ids"], fields["attributes"] = "/externalIds", "/attributes"
	p.Traversal.IDRecovery.Mapping = model.Mapping{ID: "/id", Fields: fields}
	return p
}

func metadataEnvironment(t *testing.T, candidates ...model.MetadataCandidate) Environment {
	t.Helper()
	return Environment{MetadataCandidates: func(_ context.Context, after string, limit int) ([]model.MetadataCandidate, error) {
		if limit < 1 || limit > 100 {
			t.Fatalf("unbounded metadata lookup: %d", limit)
		}
		var selected []model.MetadataCandidate
		for _, candidate := range candidates {
			if candidate.SourceID > after {
				selected = append(selected, candidate)
				if len(selected) == limit {
					break
				}
			}
		}
		return selected, nil
	}}
}

func metadataItem(id int, hash, external string) string {
	return fmt.Sprintf(`{ "id":%d, "category":1, "name":"detail", "size":0, "hash":%q, "externalIds":%s, "vendor":1.2300e+2 }`, id, hash, external)
}

func fetchConnectorPage(t *testing.T, p model.Provider, mode model.RunMode, env Environment, cursor json.RawMessage) (model.Page, error) {
	t.Helper()
	c, err := New(t.Context(), p, mode, env)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.Fetch(t.Context(), cursor)
}

func TestJSONMetadataVisitsHistoricalCandidatesAcrossRestartWithoutListing(t *testing.T) {
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	details := []string{metadataItem(1, hashes[0], `[{"kind":"imdb","value":"tt123"}]`), metadataItem(2, hashes[1], `[]`)}
	// These historical published identities no longer match today's selected
	// category. Metadata must still visit them, not just IDs on recent pages.
	for i := range details {
		details[i] = strings.Replace(details[i], `"category":1`, `"category":9`, 1)
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		for i, hash := range hashes {
			if r.Method == http.MethodGet && r.URL.Path == "/detail/"+hash {
				_, _ = fmt.Fprint(w, details[i])
				return
			}
		}
		t.Errorf("metadata made a listing, options or resolver request: %s %s", r.Method, r.URL)
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	p := metadataFixture(server.URL)
	p.Traversal.EnrichFields = append(p.Traversal.EnrichFields, "seeders")
	p.Traversal.IDRecovery.Mapping.Fields["seeders"] = "/seeders"
	env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "1", InfoHash: hashes[0]}, model.MetadataCandidate{SourceID: "2", InfoHash: hashes[1]})
	var cursor json.RawMessage
	for step := range 3 {
		page, err := fetchConnectorPage(t, p, model.ModeMetadata, env, cursor)
		if err != nil || page.Done != (step == 2) || page.Metadata["auxiliary_response"] != true {
			t.Fatalf("lost committed metadata boundary: step=%d page=%#v error=%v", step, page, err)
		}
		if step < 2 {
			if len(page.Items) != 1 || page.Items[0].SourceID != strconv.Itoa(step+1) || page.Items[0].Ignored || page.Items[0].Auxiliary || page.Items[0].CoverageScopes != nil || string(page.Items[0].Raw) != details[step] || string(page.Body) != details[step] {
				t.Fatalf("historical metadata was filtered or changed identity/bytes: %#v", page)
			}
		}
		cursor = page.Next
	}
	// A terminal checkpoint is safe to resume without another catalogue query.
	env.MetadataCandidates = func(context.Context, string, int) ([]model.MetadataCandidate, error) {
		t.Fatal("terminal metadata checkpoint queried candidates")
		return nil, nil
	}
	page, err := fetchConnectorPage(t, p, model.ModeMetadata, env, cursor)
	if err != nil || !page.Done || !reflect.DeepEqual(requests, []string{"/detail/" + hashes[0], "/detail/" + hashes[1]}) {
		t.Fatalf("resume refetched completed native IDs: page=%#v error=%v requests=%v", page, err, requests)
	}
}

func TestJSONDiscoveryModesDoNotFetchSupplementalMetadata(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModePreview, model.ModeIncremental} {
		t.Run(string(mode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/list" {
					t.Errorf("discovery made a supplemental detail request: %s", r.URL)
					http.Error(w, "unexpected detail", http.StatusBadRequest)
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				id := 8 - page*2
				windowList(w, page, 6, windowItem(id, 1), windowItem(id-1, 1))
			}))
			defer server.Close()
			p := metadataFixture(server.URL)
			ledger := newWindowLedger()
			env := baselineEnvironment(t, ledger, map[string]bool{"6": true, "5": true})
			env.MetadataCandidates = func(context.Context, string, int) ([]model.MetadataCandidate, error) {
				t.Fatal("discovery queried metadata candidates")
				return nil, nil
			}
			var cursor json.RawMessage
			done := false
			for range 20 {
				page, err := fetchConnectorPage(t, p, mode, env, cursor)
				if err != nil {
					t.Fatal(err)
				}
				ledger.commit(page)
				cursor = page.Next
				if page.Done {
					done = true
					break
				}
			}
			want := map[string]bool{"6": true, "5": true}
			if mode != model.ModeIncremental {
				want = map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true, "6": true}
			}
			if !done || !reflect.DeepEqual(ledger.ids, want) {
				t.Fatalf("Full obeyed a known boundary or Incremental crossed it: done=%v ids=%v", done, ledger.ids)
			}
		})
	}
}

func TestJSONMetadataSharedHashNeverJoinsNativeIdentities(t *testing.T) {
	hash := strings.Repeat("a", 40)
	detail := metadataItem(249961, hash, `[{"kind":"tmdb_movie","value":"42"}]`)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, detail)
	}))
	defer server.Close()
	p := metadataFixture(server.URL)
	env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "249627", InfoHash: hash}, model.MetadataCandidate{SourceID: "249961", InfoHash: hash})
	first, err := fetchConnectorPage(t, p, model.ModeMetadata, env, nil)
	if err != nil || len(first.Items) != 1 || !first.Items[0].Auxiliary || !first.Items[0].Ignored || first.Items[0].SourceID != "249961" || first.Metadata["detail_skip_reason"] != "native_id_mismatch" || first.Metadata["requested_id"] != "249627" || first.Metadata["returned_id"] != "249961" {
		t.Fatalf("hash collision published another native identity: page=%#v error=%v", first, err)
	}
	second, err := fetchConnectorPage(t, p, model.ModeMetadata, env, first.Next)
	if err != nil || requests != 2 || len(second.Items) != 1 || second.Items[0].SourceID != "249961" || second.Items[0].Auxiliary || second.Items[0].Ignored {
		t.Fatalf("same-hash candidate was deduplicated or mismatch did not advance: page=%#v error=%v calls=%d", second, err, requests)
	}
}

func TestJSONMetadataFailedDetailRetainsResponseAndContinuation(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"quota", http.StatusTooManyRequests, `{"retry":"later"}`, "http"},
		{"authentication", http.StatusUnauthorized, `{"error":"denied"}`, "authentication"},
		{"parse", http.StatusOK, `{"id":`, "parse"},
		{"hash_mismatch", http.StatusOK, metadataItem(9, strings.Repeat("b", 40), `[]`), "parse"},
		{"missing_metadata", http.StatusOK, windowItem(9, 1), "parse"},
		{"null_metadata", http.StatusOK, metadataItem(9, hash, `null`), "parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			details := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				details++
				if details == 1 {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
					return
				}
				_, _ = fmt.Fprint(w, metadataItem(9, hash, `[]`))
			}))
			defer server.Close()
			p := metadataFixture(server.URL)
			env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "9", InfoHash: hash})
			cursor := json.RawMessage(`{"metadata_version":1,"after":"8"}`)
			failed, err := fetchConnectorPage(t, p, model.ModeMetadata, env, cursor)
			if FailureCode(err) != tc.code || details != 1 || failed.Done || string(failed.Body) != tc.body || string(failed.Next) != string(cursor) || failed.Metadata["http_status"] != tc.status || failed.Metadata["auxiliary_response"] != true {
				t.Fatalf("failure lost response or advanced candidate: page=%#v error=%v calls=%d", failed, err, details)
			}
			resumed, err := fetchConnectorPage(t, p, model.ModeMetadata, env, failed.Next)
			if err != nil || len(resumed.Items) != 1 || resumed.Items[0].SourceID != "9" || details != 2 {
				t.Fatalf("failed candidate could not resume: page=%#v error=%v", resumed, err)
			}
			finished, err := fetchConnectorPage(t, p, model.ModeMetadata, env, resumed.Next)
			if err != nil || !finished.Done || details != 2 {
				t.Fatalf("successful candidate was refetched: page=%#v error=%v calls=%d", finished, err, details)
			}
		})
	}
}

func TestJSONMetadataUnavailableDetailAdvancesWithExplicitReason(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := `{"error":"no longer available"}`
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			p := metadataFixture(server.URL)
			env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "1", InfoHash: strings.Repeat("a", 40)})
			page, err := fetchConnectorPage(t, p, model.ModeMetadata, env, nil)
			if err != nil || len(page.Items) != 0 || string(page.Body) != body || page.Metadata["detail_skip_reason"] != "unavailable" || page.Metadata["requested_id"] != "1" || page.Metadata["auxiliary_response"] != true {
				t.Fatalf("unavailable detail lost evidence or changed identity: page=%#v error=%v", page, err)
			}
			finished, err := fetchConnectorPage(t, p, model.ModeMetadata, env, page.Next)
			if err != nil || !finished.Done || requests != 1 {
				t.Fatalf("unavailable candidate did not advance: page=%#v error=%v calls=%d", finished, err, requests)
			}
		})
	}
}

func TestFieldsFromJSONExternalIDsPreserveMappedAndUnknownData(t *testing.T) {
	raw := []byte(`{"id":9007199254740993,"attributes":{"imdbid":["ttold","ttnew","ttold"],"tmdbid":[12],"custom":["keep","both"]},"externalIds":[{"kind":"imdb","value":"ttnew"},{"kind":"imdb","value":"ttnew"},{"kind":"tmdb_movie","value":184467440737095516170},{"kind":"tmdb_tv","value":"34"},{"kind":"tvdb_series","value":9007199254740993},{"kind":"vendor","value":1.2300e+2,"extra":true},{"kind":"tvdb_movie","value":45}]}`)
	mapping := model.Mapping{ID: "/id", Fields: map[string]string{"attributes": "/attributes", "external_ids": "/externalIds"}}
	id, fields, err := FieldsFromJSON(raw, mapping, "")
	if err != nil || id != "9007199254740993" {
		t.Fatalf("numeric identity lost precision: id=%s error=%v", id, err)
	}
	attributes := fields["attributes"].(map[string]any)
	for key, want := range map[string][]string{"imdbid": {"ttold", "ttnew"}, "tmdbid": {"12", "184467440737095516170", "34"}, "tvdbid": {"9007199254740993", "45"}} {
		if !reflect.DeepEqual(attributes[key], want) {
			t.Fatalf("attribute %s lost values or precision: got=%#v want=%#v", key, attributes[key], want)
		}
	}
	if !reflect.DeepEqual(attributes["custom"], []any{"keep", "both"}) {
		t.Fatalf("custom mapped attributes were replaced: %#v", attributes)
	}
	external := fields["external_ids"].([]any)
	unknown := external[5].(map[string]any)
	if len(external) != 7 || external[2].(map[string]any)["value"] != json.Number("184467440737095516170") || unknown["kind"] != "vendor" || unknown["value"] != json.Number("1.2300e+2") || unknown["extra"] != true {
		t.Fatalf("authoritative external IDs were rewritten: %#v", external)
	}
	for _, source := range []string{`{"id":1,"externalIds":[]}`, `{"id":1,"externalIds":[{"kind":"unknown","value":"x"}]}`} {
		_, fields, err := FieldsFromJSON([]byte(source), mapping, "")
		if err != nil || fields["attributes"] != nil || fields["external_ids"] == nil {
			t.Fatalf("unknown or empty IDs invented attributes or lost completeness: fields=%#v error=%v", fields, err)
		}
	}
}

func TestJSONMetadataValidatesMappingsAndEligibilityBeforeIO(t *testing.T) {
	for _, change := range []func(*model.Provider){
		func(p *model.Provider) { p.Traversal.IDRecovery = nil },
		func(p *model.Provider) { delete(p.Mapping.Fields, "info_hash") },
		func(p *model.Provider) { delete(p.Traversal.IDRecovery.Mapping.Fields, "info_hash") },
		func(p *model.Provider) { p.Traversal.EnrichFields = []string{"external_ids", "external_ids"} },
		func(p *model.Provider) { p.Traversal.EnrichFields = []string{"missing"} },
		func(p *model.Provider) { p.Traversal.EnrichFields = []string{"bad field"} },
		func(p *model.Provider) {
			p.Traversal.EnrichFields = nil
			p.Traversal.MetadataAfterIncremental = true
		},
	} {
		p := metadataFixture("http://example.test")
		change(&p)
		if err := Validate(p); err == nil {
			t.Fatalf("invalid metadata configuration accepted: %#v", p.Traversal)
		}
	}
	p := metadataFixture("http://example.test")
	if c, err := New(t.Context(), p, model.ModeMetadata, Environment{}); err == nil {
		_ = c.Close()
		t.Fatal("metadata accepted without published candidates")
	}
	for _, provider := range []model.Provider{orderedFixture("http://example.test"), protocolProvider("http_json", "http://example.test"), protocolProvider("torznab", "http://example.test")} {
		if c, err := New(t.Context(), provider, model.ModeMetadata, metadataEnvironment(t)); err == nil {
			_ = c.Close()
			t.Fatal("unsupported source accepted metadata mode")
		}
	}
}

func TestJSONMetadataSharesPreNetworkPauseAndResumesCandidate(t *testing.T) {
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		for i, hash := range hashes {
			if r.URL.Path == "/detail/"+hash {
				_, _ = fmt.Fprint(w, metadataItem(i+1, hash, `[]`))
				return
			}
		}
		t.Errorf("unexpected request: %s", r.URL)
	}))
	defer server.Close()
	p := metadataFixture(server.URL)
	p.RequestInterval = "1h"
	env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "1", InfoHash: hashes[0]}, model.MetadataCandidate{SourceID: "2", InfoHash: hashes[1]})
	env.BeforeWait = func(context.Context, time.Time) error { return context.Canceled }
	c, err := New(t.Context(), p, model.ModeMetadata, env)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first, err := c.Fetch(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := c.Fetch(t.Context(), first.Next)
	if err == nil || requests != 1 || len(paused.Body) != 0 || paused.Done || string(paused.Next) != string(first.Next) {
		t.Fatalf("metadata escaped the shared pacing/pause boundary: page=%#v error=%v requests=%d", paused, err, requests)
	}
	_ = c.Close()
	resumed, err := fetchConnectorPage(t, p, model.ModeMetadata, env, paused.Next)
	if err != nil || requests != 2 || len(resumed.Items) != 1 || resumed.Items[0].SourceID != "2" {
		t.Fatalf("safe-boundary pause lost native identity: page=%#v error=%v requests=%d", resumed, err, requests)
	}
}

func TestJSONMetadataCandidateLookupFailureDoesNotAdvance(t *testing.T) {
	p := metadataFixture("http://example.test")
	want := errors.New("catalogue unavailable")
	env := Environment{MetadataCandidates: func(context.Context, string, int) ([]model.MetadataCandidate, error) {
		return nil, want
	}}
	cursor := json.RawMessage(`{"metadata_version":1,"after":"8"}`)
	page, err := fetchConnectorPage(t, p, model.ModeMetadata, env, cursor)
	if err == nil || page.Done || string(page.Next) != string(cursor) || len(page.Body) != 0 {
		t.Fatalf("candidate lookup failure advanced progress: page=%#v error=%v", page, err)
	}
}

func TestJSONModesRejectIncompatibleInlineEnrichmentCheckpoint(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModePreview, model.ModeIncremental, model.ModeMetadata} {
		t.Run(string(mode), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				t.Error("incompatible checkpoint restarted source traversal")
				http.Error(w, "unexpected request", http.StatusBadRequest)
			}))
			defer server.Close()
			p := metadataFixture(server.URL)
			env := baselineEnvironment(t, newWindowLedger(), nil)
			env.MetadataCandidates = metadataEnvironment(t).MetadataCandidates
			for _, cursor := range []json.RawMessage{
				json.RawMessage(`{"enrichment_version":1,"child":{"version":1,"phase":"incremental","page":2},"pending":[{"id":"9","hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`),
				json.RawMessage(`{}`),
				json.RawMessage(`null`),
			} {
				page, err := fetchConnectorPage(t, p, mode, env, cursor)
				if err == nil || page.Done || requests != 0 {
					t.Fatalf("incompatible checkpoint was accepted: page=%#v error=%v calls=%d", page, err, requests)
				}
			}
		})
	}
}

func TestJSONMetadataMissingLocatorSkipsWithoutBlockingLaterIdentity(t *testing.T) {
	hash := strings.Repeat("a", 40)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/detail/"+hash {
			t.Errorf("invalid metadata locator reached source: %s", r.URL)
		}
		_, _ = fmt.Fprint(w, metadataItem(2, hash, `[]`))
	}))
	defer server.Close()
	p := metadataFixture(server.URL)
	env := metadataEnvironment(t, model.MetadataCandidate{SourceID: "1"}, model.MetadataCandidate{SourceID: "2", InfoHash: hash})
	skipped, err := fetchConnectorPage(t, p, model.ModeMetadata, env, nil)
	if err != nil || requests != 0 || len(skipped.Items) != 0 || skipped.Metadata["detail_skip_reason"] != "invalid_info_hash" || skipped.Metadata["requested_id"] != "1" {
		t.Fatalf("missing locator blocked sweep or invented detail: page=%#v error=%v calls=%d", skipped, err, requests)
	}
	next, err := fetchConnectorPage(t, p, model.ModeMetadata, env, skipped.Next)
	if err != nil || requests != 1 || len(next.Items) != 1 || next.Items[0].SourceID != "2" {
		t.Fatalf("missing locator prevented later metadata: page=%#v error=%v calls=%d", next, err, requests)
	}
}
