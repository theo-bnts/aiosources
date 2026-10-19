package jobs

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestHTMLPreviewFullRecoveryAndIncrementalUseOrdinaryPublication(t *testing.T) {
	ctx, endpoint, db := schedulerDatabase(t)
	var brokenPage, brokenDetail atomic.Bool
	var firstListings, secondListings atomic.Int32
	var updated atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.HasPrefix(r.URL.Path, "/detail/") {
			if brokenDetail.Load() && r.URL.Path == "/detail/two" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`<main id="detail"><span class="hash">` + strings.Repeat("a", 40) + `</span></main>`))
			return
		}
		id, title, next := "one", "Original example", `<a rel="next" href="?page=2">Next</a>`
		if updated.Load() {
			title = "Updated example"
		}
		if r.URL.Query().Get("page") == "2" {
			secondListings.Add(1)
			if brokenPage.Load() {
				_, _ = w.Write([]byte(`<form>Sign in again</form>`))
				return
			}
			id, title, next = "two", "Second example", ""
		} else {
			firstListings.Add(1)
		}
		_, _ = w.Write([]byte(`<main id="catalog"><article data-id="` + id + `"><span class="title">` + title + `</span><a class="detail" href="/detail/` + id + `">Details</a></article></main>` + next))
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := model.Provider{Version: 1, ID: "html_archive", Name: "Example HTML archive", Adapter: "html", URL: server.URL + "/catalog", Enabled: true, Auth: model.Auth{Type: "none"}, RequestInterval: "1ms", PageSize: 1,
		HTTP:       model.HTTPConfig{Method: http.MethodGet},
		Pagination: model.Pagination{Type: "next", Start: 1},
		HTML: &model.HTMLConfig{PageSelector: "#catalog", RowsSelector: "article", EmptySelector: ".empty", NextSelector: "a[rel=next]", ID: model.HTMLField{Attribute: "data-id"},
			Fields:  map[string]model.HTMLField{"title": {Selector: ".title", Required: true}},
			Details: &model.HTMLDetails{URL: model.HTMLField{Selector: ".detail", Attribute: "href"}, PageSelector: "#detail", Fields: map[string]model.HTMLField{"info_hash": {Selector: ".hash", Format: "info_hash", Required: true}}}},
		Schedule: model.Schedule{KnownPages: 1}}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	pageBudget := 1
	preview, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModePreview, MaxPages: &pageBudget})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, db, preview.ID, model.StatusSucceeded)
	live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 0 {
		t.Fatal("HTML Preview published source data")
	}
	full, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull, MaxPages: &pageBudget})
	if err != nil {
		t.Fatal(err)
	}
	paused := collectScheduledAttempt(t, ctx, manager, db, full.ID, model.StatusPaused)
	if paused.Pages != 1 || paused.TraversalDone || paused.Errors != 0 || len(paused.Cursor) == 0 {
		t.Fatal("HTML detail responses changed the listing budget or checkpoint")
	}
	live, err = db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 0 {
		t.Fatal("partial HTML Full was published")
	}
	db.Close()
	restarted, err := store.Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	manager = New(restarted, registry, nil, 1)
	brokenPage.Store(true)
	if _, err := manager.Resume(ctx, full.ID); err != nil {
		t.Fatal(err)
	}
	failed := collectScheduledAttempt(t, ctx, manager, restarted, full.ID, model.StatusFailed)
	if failed.Pages != paused.Pages || !bytes.Equal(failed.Cursor, paused.Cursor) || failed.TraversalDone || failed.DistinctRecords != 1 {
		t.Fatal("HTML login response changed committed progress")
	}
	brokenPage.Store(false)
	if _, err := manager.Resume(ctx, full.ID); err != nil {
		t.Fatal(err)
	}
	complete := collectScheduledAttempt(t, ctx, manager, restarted, full.ID, model.StatusSucceeded)
	live, err = restarted.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 2 || complete.Pages != 2 || firstListings.Load() != 2 || secondListings.Load() != 2 {
		t.Fatalf("HTML resume refetched committed data or failed publication: total=%d first=%d second=%d %v", live.Total, firstListings.Load(), secondListings.Load(), err)
	}
	for _, torrent := range live.Items {
		if torrent.Fields["info_hash"] != strings.Repeat("a", 40) {
			t.Fatal("HTML detail hash did not survive normal compact storage")
		}
	}
	brokenDetail.Store(true)
	badRebuild, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	collectScheduledAttempt(t, ctx, manager, restarted, badRebuild.ID, model.StatusFailed)
	live, err = restarted.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 2 {
		t.Fatal("failed HTML details replaced a previously published catalog")
	}
	brokenDetail.Store(false)
	updated.Store(true)
	beforeSecond := secondListings.Load()
	delta, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeIncremental})
	if err != nil || delta.Mode != model.ModeIncremental {
		t.Fatalf("HTML successful Full did not establish an Incremental baseline: %v", err)
	}
	collectScheduledAttempt(t, ctx, manager, restarted, delta.ID, model.StatusSucceeded)
	live, err = restarted.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 2 || secondListings.Load() != beforeSecond {
		t.Fatal("HTML Incremental ignored its known-page boundary or deleted unvisited records")
	}
	found := false
	for _, torrent := range live.Items {
		found = found || torrent.SourceID == "one" && torrent.Fields["title"] == "Updated example"
	}
	if !found {
		t.Fatal("HTML Incremental did not merge changed listing fields")
	}
}
