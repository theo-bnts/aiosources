package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAdditionalListingsPreserveIndependentIncrementalBoundaries(t *testing.T) {
	var requests []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path + r.URL.RawQuery
		mu.Lock()
		requests = append(requests, key)
		mu.Unlock()
		id, next := "known-video", "/video?page=2"
		if r.URL.Path == "/audio" {
			id, next = "new-audio", "/audio?page=2"
			if r.URL.Query().Get("page") == "2" {
				id, next = "known-audio", "/audio?page=3"
			}
		}
		htmlResponse(w, htmlListing(htmlRow(id, "Example release")+`<a rel="next" href="`+next+`">Next</a>`))
	}))
	t.Cleanup(upstream.Close)
	p := htmlProvider(upstream.URL + "/video")
	p.HTTP.ListingPaths = []string{"/audio"}
	p.Schedule.KnownPages = 1
	env := Environment{KnownIDs: func(_ context.Context, ids []string) (map[string]bool, error) {
		known := make(map[string]bool, len(ids))
		for _, id := range ids {
			known[id] = strings.HasPrefix(id, "known-")
		}
		return known, nil
	}}
	var cursor json.RawMessage
	for i, want := range []string{"known-video", "new-audio", "known-audio"} {
		// Reopening after each commit also verifies recovery at a listing boundary.
		connector := protocolOpen(t, p, model.ModeIncremental, env)
		page, err := connector.Fetch(t.Context(), cursor)
		if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != want || page.Done != (i == 2) || page.Metadata["known_pages_managed"] != true {
			t.Fatalf("listing %d skipped unseen records or used another boundary: %+v %v", i, page, err)
		}
		if i == 1 && bytes.Equal(cursor, page.Next) {
			t.Fatal("pagination did not advance within the second listing")
		}
		cursor = page.Next
		_ = connector.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"/video", "/audio", "/audiopage=2"}) {
		t.Fatal("a completed listing was revisited or later listing skipped", requests)
	}
}

func TestAdditionalJSONListingsRetryOnlyTheFailedListing(t *testing.T) {
	var calls atomic.Int32
	var invalid atomic.Bool
	invalid.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/second" && invalid.Load() {
			_, _ = w.Write([]byte(`{"wrong_shape":true}`))
			return
		}
		fmt.Fprintf(w, `[{"id":%q,"title":"Example release"}]`, r.URL.Path)
	}))
	t.Cleanup(upstream.Close)
	p := protocolProvider("json", upstream.URL+"/first")
	p.HTTP.ListingPaths = []string{"/second"}
	p.Pagination.Type = "none"
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title"}}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	first, err := connector.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 1 || first.Metadata["listing_index"] != 0 {
		t.Fatal("first listing failed", err)
	}
	failed, err := connector.Fetch(t.Context(), first.Next)
	if err == nil || failed.Done || !bytes.Equal(failed.Next, first.Next) {
		t.Fatal("failed listing advanced its checkpoint")
	}
	_ = connector.Close()
	invalid.Store(false)
	resumed := protocolOpen(t, p, model.ModeFull, Environment{})
	last, err := resumed.Fetch(t.Context(), failed.Next)
	if err != nil || !last.Done || len(last.Items) != 1 || last.Items[0].SourceID != "/second" || calls.Load() != 3 || last.Metadata["fingerprint_scope"] == first.Metadata["fingerprint_scope"] {
		t.Fatal("resume lost the current listing or repeated the first one", err)
	}
	again, err := resumed.Fetch(t.Context(), last.Next)
	if err != nil || !again.Done || len(again.Items) != 0 || calls.Load() != 3 {
		t.Fatal("completed checkpoint made another request", err)
	}
}

func TestAdditionalListingsRejectUnsafePathsAndInvalidRecoveryState(t *testing.T) {
	p := htmlProvider("https://archive.example/video")
	for _, paths := range [][]string{{"https://other.example/audio"}, {"//other.example/audio"}, {"audio"}, {"/audio#fragment"}, {"/audio?apikey=secret"}, {"/video"}, {"/audio", "/audio"}, {"/audio?gate=secret"}} {
		p.HTTP.ListingPaths, p.Auth = paths, model.Auth{Type: "query", Name: "gate", SecretRef: "example_key"}
		if Validate(p) == nil {
			t.Fatal("unsafe additional listing accepted", paths)
		}
	}
	p.HTTP.ListingPaths = []string{"/audio"}
	for _, adapter := range []string{"torznab", "json"} {
		incompatible := p
		incompatible.Adapter = adapter
		if adapter == "json" {
			incompatible.HTTP.Catalog = true
		}
		if Validate(incompatible) == nil {
			t.Fatal("incompatible source accepted additional listings", adapter)
		}
	}
	connector := protocolOpen(t, p, model.ModeFull, Environment{Secrets: protocolSecrets})
	for _, raw := range []string{`{"version":2}`, `{"version":1,"listing":-1}`, `{"version":1,"listing":2}`, `{"version":1,"listing":0,"done":true}`, `{"version":1,"known_pages":1}`} {
		page, err := connector.Fetch(t.Context(), json.RawMessage(raw))
		if FailureCode(err) != "configuration" || page.Done || string(page.Next) != raw {
			t.Fatal("invalid checkpoint accepted or modified", err)
		}
	}
}

func TestAdditionalListingsDoNotAdvanceWhenBaselineLookupFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlResponse(w, htmlListing(htmlRow("one", "Example release")+`<a rel="next" href="?page=2">Next</a>`))
	}))
	t.Cleanup(upstream.Close)
	p := htmlProvider(upstream.URL + "/first")
	p.HTTP.ListingPaths, p.Schedule.KnownPages = []string{"/second"}, 1
	lookupErr := errors.New("baseline unavailable")
	connector := protocolOpen(t, p, model.ModeIncremental, Environment{KnownIDs: func(context.Context, []string) (map[string]bool, error) { return nil, lookupErr }})
	page, err := connector.Fetch(t.Context(), nil)
	if !errors.Is(err, lookupErr) || page.Done || len(page.Next) != 0 {
		t.Fatal("baseline failure advanced or completed a listing", err)
	}
}

func TestAdditionalJSONListingsKeepRelativeContinuationOnTheirOwnPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page, next := "1", `"?page=2"`
		if r.URL.Query().Get("page") == "2" {
			page, next = "2", "null"
		}
		fmt.Fprintf(w, `{"data":[{"id":%q,"title":"Example release"}],"next":%s}`, r.URL.Path+page, next)
	}))
	t.Cleanup(upstream.Close)
	p := protocolProvider("json", upstream.URL+"/video")
	p.HTTP.ListingPaths, p.HTTP.ItemsPath = []string{"/audio"}, "/data"
	p.Pagination = model.Pagination{Type: "page", Start: 1, NextPath: "/next"}
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title"}}
	var cursor json.RawMessage
	for index, want := range []string{"/video1", "/video2", "/audio1", "/audio2"} {
		connector := protocolOpen(t, p, model.ModeFull, Environment{})
		page, err := connector.Fetch(t.Context(), cursor)
		if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != want || page.Done != (index == 3) {
			t.Fatalf("relative continuation escaped its current listing: %+v %v", page, err)
		}
		cursor = page.Next
		_ = connector.Close()
	}
}
