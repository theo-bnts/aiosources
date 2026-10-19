package jobs

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestMultipleListingsPublishTogetherAndKeepLaterIncrementalRecords(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	var updated atomic.Bool
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		page := "1"
		if r.URL.Query().Get("page") == "2" {
			page = "2"
		}
		id, next := r.URL.Path+page, ""
		if page == "1" {
			next = `<a rel="next" href="?page=2">Next</a>`
		}
		if updated.Load() && r.URL.Path == "/audio" && page == "1" {
			id = "new-audio"
		}
		fmt.Fprintf(w, `<main id="catalog"><article data-id=%q><span class="title">Example release</span></article></main>%s`, id, next)
	}))
	t.Cleanup(upstream.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := model.Provider{Version: 1, ID: "multiple_listings", Name: "Example archive", Adapter: "html", URL: upstream.URL + "/video", Enabled: true,
		Auth: model.Auth{Type: "none"}, RequestInterval: "1ms", PageSize: 1,
		HTTP: model.HTTPConfig{Method: http.MethodGet, ListingPaths: []string{"/audio"}}, Pagination: model.Pagination{Type: "next", Start: 1},
		HTML:     &model.HTMLConfig{PageSelector: "#catalog", RowsSelector: "article", NextSelector: "a[rel=next]", ID: model.HTMLField{Attribute: "data-id"}, Fields: map[string]model.HTMLField{"title": {Selector: ".title", Required: true}}},
		Schedule: model.Schedule{KnownPages: 1}}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	budget := 1
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull, MaxPages: &budget})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 4 {
		status := model.StatusPaused
		if attempt == 3 {
			status = model.StatusSucceeded
		}
		collectScheduledAttempt(t, ctx, manager, db, run.ID, status)
		live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
		if err != nil || attempt < 3 && live.Total != 0 || attempt == 3 && live.Total != 4 {
			t.Fatal("partial listings were published or a completed listing lost", live.Total, err)
		}
		if attempt < 3 {
			if _, err = manager.Resume(ctx, run.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if requests.Load() != 4 {
		t.Fatal("resuming a budgeted import re-fetched a committed listing")
	}
	updated.Store(true)
	delta, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeIncremental})
	if err != nil || delta.Mode != model.ModeIncremental {
		t.Fatal("successful multi-listing import lost its baseline", err)
	}
	collectScheduledAttempt(t, ctx, manager, db, delta.ID, model.StatusSucceeded)
	live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 5 || requests.Load() != 7 {
		t.Fatal("known first listing hid later new records or Incremental removed older publications", live.Total, requests.Load(), err)
	}
}
