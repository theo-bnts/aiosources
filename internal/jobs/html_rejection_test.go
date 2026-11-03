package jobs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestHTMLRejectedRecordRemainsDiagnosableAndDoesNotBlockPublication(t *testing.T) {
	ctx, _, db := schedulerDatabase(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if strings.HasPrefix(r.URL.Path, "/detail/") {
			hash := ""
			if r.URL.Path != "/detail/bad" {
				hash = `<span class="hash">` + strings.Repeat("a", 40) + `</span>`
			}
			_, _ = w.Write([]byte(`<main id="detail">` + hash + `</main>`))
			return
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`<main id="catalog"><article data-id="last"><span class="title">Last example</span><a class="detail" href="/detail/last">Details</a></article></main>`))
			return
		}
		_, _ = w.Write([]byte(`<main id="catalog"><article data-id="first"><span class="title">First example</span><a class="detail" href="/detail/first">Details</a></article><article data-id="bad"><span class="title">Unavailable example</span><a class="detail" href="/detail/bad">Details</a></article></main><a rel="next" href="?page=2">Next</a>`))
	}))
	t.Cleanup(server.Close)
	registry, err := providers.New(t.TempDir(), connectors.Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	p := model.Provider{Version: 1, ID: "html_archive", Name: "Example archive", Adapter: "html", URL: server.URL, Enabled: true, Auth: model.Auth{Type: "none"}, RequestInterval: "1ms", PageSize: 2, HTTP: model.HTTPConfig{Method: "GET"}, Pagination: model.Pagination{Type: "next", Start: 1},
		HTML: &model.HTMLConfig{InvalidRecordPolicy: "reject", PageSelector: "#catalog", RowsSelector: "article", NextSelector: "a[rel=next]", ID: model.HTMLField{Attribute: "data-id"}, Fields: map[string]model.HTMLField{"title": {Selector: ".title", Required: true}}, Details: &model.HTMLDetails{URL: model.HTMLField{Selector: ".detail", Attribute: "href"}, PageSelector: "#detail", Fields: map[string]model.HTMLField{"info_hash": {Selector: ".hash", Format: "info_hash", Required: true}}}}}
	saveScheduledProvider(t, registry, p, "")
	manager := New(db, registry, nil, 1)
	run, err := manager.Enqueue(ctx, model.StartRun{ProviderID: p.ID, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	complete := collectScheduledAttempt(t, ctx, manager, db, run.ID, model.StatusSucceeded)
	live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || live.Total != 2 || complete.Errors != 1 || complete.Pages != 2 {
		t.Fatal("rejection blocked the traversal or published an invalid record", live.Total, complete.Errors, complete.Pages, err)
	}
	raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID, Status: "error"})
	if err != nil || raw.Total != 1 || len(raw.Items) != 1 || raw.Items[0].SourceID != "bad" || raw.Items[0].Error == "" {
		t.Fatal("rejected record was not retained in run diagnostics", err)
	}
}
