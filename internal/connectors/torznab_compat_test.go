package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

// These synthetic fixtures preserve native identity and traversal contracts.
// All upstream responses come from owned loopback servers.
func TestTorznabNativeLinkIdentityAndExactLocalCategories(t *testing.T) {
	const hash = "e34672bd9f2af6298786169de488301d7951b98f"
	const firstID = "1323669c-a008-499a-bf38-75b1a6aa99d6"
	const secondID = "1323669c-a008-499a-bf38-75b1a6aa99d7"
	item := func(link string, category int) string {
		return fmt.Sprintf(`<item><guid>%s</guid><title>Shared content</title><link>%s</link><x:attr name="infohash" value="%s"/><x:attr name="category" value="%d"/></item>`, hash, link, hash, category)
	}
	fragments := []string{
		item("https://tracker.example/torrents/"+firstID+"?apikey=private", 2020),
		item("/torrents/"+secondID+"/", 5040),
		item("/torrents/excluded-parent", 2000),
		item("/torrents/excluded-child", 2021),
		item("https://tracker.example/download?apikey=private&amp;next=/torrents/not-a-path-id", 2020),
	}
	body := `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel>` + strings.Join(fragments, "") + `</channel></rss>`
	caps := strings.ReplaceAll(protocolCaps, `="2"`, `="100"`)
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("cat") {
			t.Error("local category selection changed the global feed request")
		}
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, caps)
			return
		}
		step := searches.Add(1)
		wantOffset := "0"
		if step == 2 {
			wantOffset = "5"
		}
		if step > 2 || r.URL.Query().Get("offset") != wantOffset {
			t.Errorf("global traversal skipped archived items: step=%d query=%s", step, r.URL.RawQuery)
		}
		if step == 1 {
			_, _ = io.WriteString(w, body)
		} else {
			_, _ = io.WriteString(w, `<rss><channel/></rss>`)
		}
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL+"/api")
	p.PageSize = 100
	p.Options = map[string]any{
		"id_source":        "link",
		"id_pattern":       `^[^?#]*?/torrents/+([^?#]*?[^/?#])/*(?:[?#].*)?$`,
		"local_categories": []int{2010, 2020, 2030, 2040, 2060, 2510, 5040, 5060, 5070, 5080, 5091, 5092},
	}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	page, err := connector.Fetch(t.Context(), nil)
	if err != nil || page.Done {
		t.Fatalf("global page failed or a short page ended traversal: %#v, %v", page, err)
	}
	protocolRaw(t, page, body, append([]string{caps}, fragments...)...)
	for i, want := range []string{firstID, secondID} {
		record := page.Items[i+1]
		if record.SourceID != want || record.Error != "" || record.Ignored || record.Fields["guid"] != hash || record.Fields["info_hash"] != hash {
			t.Errorf("distinct native release sharing a GUID/hash was lost: %#v", record)
		}
	}
	for _, record := range page.Items[3:5] {
		if !record.Ignored || record.Error != "" {
			t.Errorf("exact category selection accepted a parent or adjacent child: %#v", record)
		}
	}
	invalid := page.Items[5]
	if invalid.SourceID != "" || invalid.Error == "" {
		t.Errorf("missing configured native ID fell back to the content hash: %#v", invalid)
	}
	last, err := connector.Fetch(t.Context(), page.Next)
	if err != nil || !last.Done || searches.Load() != 2 {
		t.Fatalf("empty global response did not finish traversal: %#v, %v", last, err)
	}
}

func TestTorznabGUIDQueryIdentityAcrossCategoryScopes(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		step := searches.Add(1)
		category, id := "2000", "11622"
		if step == 2 {
			category, id = "5000", "11623"
		}
		if step > 2 || r.URL.Query().Get("cat") != category || r.URL.Query().Get("offset") != "0" {
			t.Errorf("category traversal changed: step=%d query=%s", step, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><x:response offset="0" total="1"/><item><title>Native release</title><guid>https://feed.example/?action=torrent&amp;id=%s</guid><link>https://feed.example/api?t=get&amp;apikey=private</link><x:attr name="infohash" value="abcdef0123456789abcdef0123456789abcdef01"/></item></channel></rss>`, id)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL+"/api")
	p.Search.Categories = []int{2000, 5000}
	p.Options = map[string]any{
		"category_scope": "each",
		"id_source":      "guid",
		"id_pattern":     `[?&]id=([1-9][0-9]*)(?:&[^#]*)?(?:#.*)?$`,
	}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	first, err := connector.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 2 || first.Items[1].SourceID != "11622" || first.Items[1].Error != "" {
		t.Fatalf("first native numeric ID was not preserved: %#v, %v", first, err)
	}
	last, err := connector.Fetch(t.Context(), first.Next)
	if err != nil || !last.Done || len(last.Items) != 1 || last.Items[0].SourceID != "11623" || last.Items[0].Error != "" {
		t.Fatalf("second category did not preserve its numeric ID: %#v, %v", last, err)
	}
	if last.Items[0].Fields["guid"] != "https://feed.example/?action=torrent&id=11623" {
		t.Errorf("native identity extraction overwrote the original GUID: %#v", last.Items[0].Fields)
	}
}

func TestTorznabSuffixIdentityContinuesBeyondAdvisoryTotals(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		step := searches.Add(1)
		offset, items := "0", `<item><guid>https://releases.example/torrent/example-2160p-abcdef01</guid></item><item><guid>https://releases.example/torrent/example-movie-2021-multi-2160p-group-324748</guid></item>`
		if step == 2 {
			offset, items = "2", `<item><guid>https://releases.example/torrent/another-title-324749/</guid></item>`
		} else if step >= 3 {
			offset, items = "3", ""
		}
		if step > 3 || r.URL.Query().Get("offset") != offset {
			t.Errorf("advisory traversal offset changed: step=%d query=%s", step, r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%s" total="1"/>%s</channel></rss>`, offset, items)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL+"/api/torznab/all")
	p.Options = map[string]any{
		"id_source":  "guid",
		"id_pattern": `-([^/?#-]+)/?(?:[?#].*)?$`,
		"total_mode": "advisory",
	}
	connector := protocolOpen(t, p, model.ModeFull, Environment{})
	first, err := connector.Fetch(t.Context(), nil)
	if err != nil || first.Done || len(first.Items) != 3 || first.Items[1].SourceID != "abcdef01" || first.Items[2].SourceID != "324748" {
		t.Fatalf("capped total rejected real releases or changed suffix identities: %#v, %v", first, err)
	}
	_ = connector.Close()
	resumed := protocolOpen(t, p, model.ModeFull, Environment{})
	second, err := resumed.Fetch(t.Context(), first.Next)
	if err != nil || second.Done || len(second.Items) != 2 || second.Items[1].SourceID != "324749" {
		t.Fatalf("resuming past a capped total lost a release: %#v, %v", second, err)
	}
	last, err := resumed.Fetch(t.Context(), second.Next)
	if err != nil || !last.Done || searches.Load() != 3 {
		t.Fatalf("empty page did not terminate advisory traversal: %#v, %v", last, err)
	}
	if _, err := resumed.Fetch(t.Context(), last.Next); err != nil || searches.Load() != 3 {
		t.Fatalf("completed advisory traversal made another request: searches=%d err=%v", searches.Load(), err)
	}
}

func TestTorznabGUIDLessNativeLinksNeverMergeByHash(t *testing.T) {
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="3"/>` +
		`<item><link>https://tracker.example/torrents/first</link><x:attr name="infohash" value="` + hash + `"/></item>` +
		`<item><link>https://tracker.example/torrents/second</link><x:attr name="infohash" value="` + hash + `"/></item>` +
		`<item><x:attr name="infohash" value="` + hash + `"/></item></channel></rss>`
	caps := strings.ReplaceAll(protocolCaps, `="2"`, `="100"`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, caps)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.PageSize = 100
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if err != nil || !page.Done || len(page.Items) != 4 {
		t.Fatalf("native-link listing failed: page=%#v err=%v", page, err)
	}
	for index, id := range []string{"first", "second"} {
		record := page.Items[index+1]
		if record.SourceID != "https://tracker.example/torrents/"+id || record.Error != "" || record.Fields["info_hash"] != hash {
			t.Errorf("native link identity was replaced by its shared content hash: %#v", record)
		}
	}
	if record := page.Items[3]; record.SourceID != "" || record.Error == "" {
		t.Fatalf("hash-only item acquired a native source identity: %#v", record)
	}
}

func TestTorznabConfiguredIdentityDoesNotInheritFallbackErrors(t *testing.T) {
	body := `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="0" total="2"/>` +
		`<item><link>https://tracker.example/torrents/first?passkey=private</link></item>` +
		`<item><link>https://tracker.example/torrents/second?passkey=private</link><x:attr name="infohash" value="broken"/></item></channel></rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.Options = map[string]any{"id_source": "link", "id_pattern": `/torrents/([^?]+)\?`}
	page, err := protocolOpen(t, p, model.ModeFull, Environment{}).Fetch(t.Context(), nil)
	if err != nil || !page.Done || len(page.Items) != 3 {
		t.Fatalf("configured identity listing failed: page=%#v err=%v", page, err)
	}
	if record := page.Items[1]; record.SourceID != "first" || record.Error != "" {
		t.Fatalf("valid configured identity inherited an unused fallback error: %#v", record)
	}
	if record := page.Items[2]; record.SourceID != "second" || record.Error == "" {
		t.Fatalf("configured identity suppressed an actual metadata error: %#v", record)
	}
}

func TestTorznabKnownPagesRequireCompletePriorIdentitiesAndSurviveLookupFailure(t *testing.T) {
	ids := []string{"known-0", "ignored-new", "known-1", "known-broken", "known-2", "known-3"}
	requests := make(chan int, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil || offset < 0 || offset >= len(ids) {
			t.Error("collection crossed its known-page boundary")
			http.Error(w, "unexpected page", http.StatusBadRequest)
			return
		}
		requests <- offset
		category, size := 2000, "1"
		if offset == 1 {
			category = 5000
		}
		if offset == 3 {
			size = "invalid"
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%d" total="100"/><item><guid>%s</guid><title>Release</title><x:attr name="category" value="%d"/><x:attr name="size" value="%s"/></item></channel></rss>`, offset, ids[offset], category, size)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.Search.Categories = []int{2000}
	p.Options = map[string]any{"category_scope": "each", "local_categories": []any{2000}}
	p.Schedule.KnownPages = 2
	failLookup := true
	env := Environment{KnownIDs: func(_ context.Context, ids []string) (map[string]bool, error) {
		known := make(map[string]bool, len(ids))
		for _, id := range ids {
			if id == "known-3" && failLookup {
				return nil, errors.New("membership temporarily unavailable")
			}
			known[id] = id != "ignored-new"
		}
		return known, nil
	}}
	var checkpoint json.RawMessage
	for offset := 0; offset < len(ids)-1; offset++ {
		connector := protocolOpen(t, p, model.ModeIncremental, env)
		page, err := connector.Fetch(t.Context(), checkpoint)
		_ = connector.Close()
		if err != nil || page.Done {
			t.Fatalf("filtered or malformed identities established a false known-page boundary at %d: %+v %v", offset, page, err)
		}
		checkpoint = page.Next
	}
	connector := protocolOpen(t, p, model.ModeIncremental, env)
	failed, err := connector.Fetch(t.Context(), checkpoint)
	_ = connector.Close()
	if err == nil || failed.Error == "" || failed.Done || !bytes.Equal(failed.Next, checkpoint) {
		t.Fatalf("failed membership lookup changed the committed boundary: %+v %v", failed, err)
	}
	failLookup = false
	resumed := protocolOpen(t, p, model.ModeIncremental, env)
	last, err := resumed.Fetch(t.Context(), checkpoint)
	if err != nil || !last.Done {
		t.Fatalf("retry lost the preceding known page or failed to reach its boundary: %+v %v", last, err)
	}
	for _, want := range []int{0, 1, 2, 3, 4, 5, 5} {
		select {
		case got := <-requests:
			if got != want {
				t.Fatalf("committed pages replayed or failed response skipped: got %d want %d", got, want)
			}
		default:
			t.Fatalf("missing source request at offset %d", want)
		}
	}
}

func TestTorznabQueryPlanCategoryOverlapAndResume(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		step := int(searches.Add(1)) - 1
		if step >= 8 {
			t.Error("completed query plan performed another search")
			http.Error(w, "unexpected search", http.StatusBadRequest)
			return
		}
		query := []string{"", "archive"}[step/4]
		category := []string{"2000", "5000"}[(step/2)%2]
		offset := step % 2
		if r.URL.Query().Get("q") != query || r.URL.Query().Get("cat") != category || r.URL.Query().Get("offset") != strconv.Itoa(offset) {
			t.Errorf("query/category continuation replayed or skipped a scope: step=%d URL=%s", step, r.URL)
		}
		id := "shared"
		if offset == 1 {
			id = fmt.Sprintf("unique-%d", step/2)
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%d" total="2"/><item><guid>%s</guid><title>Release</title></item></channel></rss>`, offset, id)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.PageSize = 1
	p.Search.Categories = []int{2000, 5000}
	p.Options = map[string]any{"category_scope": "each", "search_queries": []any{"", "archive"}}
	var checkpoint json.RawMessage
	positions := make(map[string]bool)
	scopes := make(map[any]bool)
	var previousScope any
	for step := range 8 {
		// Reopening at every committed page exercises both scope crossings and
		// resumption partway through the later query without replaying the first.
		connector := protocolOpen(t, p, model.ModeIncremental, Environment{})
		page, err := connector.Fetch(t.Context(), checkpoint)
		_ = connector.Close()
		if err != nil || page.Done != (step == 7) {
			t.Fatalf("query plan ended at the wrong boundary at step %d: %+v %v", step, page, err)
		}
		var ids []string
		for _, item := range page.Items {
			if !item.Auxiliary {
				if item.Error != "" || item.Ignored {
					t.Fatalf("query overlap rejected a native identity: %+v", item)
				}
				ids = append(ids, item.SourceID)
			}
		}
		wantID := "shared"
		if step%2 == 1 {
			wantID = fmt.Sprintf("unique-%d", step/2)
		}
		if len(ids) != 1 || ids[0] != wantID {
			t.Fatalf("query union lost or merged an occurrence: got %v want %s", ids, wantID)
		}
		if positions[page.Position] {
			t.Fatalf("distinct query/category pages share position %q", page.Position)
		}
		positions[page.Position] = true
		scope := page.Metadata["fingerprint_scope"]
		if scope == nil {
			t.Fatal("query page has no repeated-page protection scope")
		}
		if step%2 == 0 {
			if scopes[scope] {
				t.Fatalf("query/category crossover reused repeated-page scope %v", scope)
			}
			scopes[scope] = true
		} else if scope != previousScope {
			t.Fatalf("pagination within a query/category lost repeated-page protection: %v != %v", scope, previousScope)
		}
		previousScope = scope
		checkpoint = page.Next
	}
	completed := protocolOpen(t, p, model.ModeIncremental, Environment{})
	page, err := completed.Fetch(t.Context(), checkpoint)
	if err != nil || !page.Done || searches.Load() != 8 {
		t.Fatalf("completed query plan did not remain terminal: %+v %v searches=%d", page, err, searches.Load())
	}
}

func TestTorznabQueryPlanCombinedKnownBoundary(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, protocolCaps)
			return
		}
		step := int(searches.Add(1)) - 1
		if step >= 6 {
			t.Error("query known-page boundary was not respected")
			http.Error(w, "unexpected search", http.StatusBadRequest)
			return
		}
		query, offset := "first", step
		if step >= 2 {
			query, offset = "later", step-2
		}
		if r.URL.Query().Get("q") != query || r.URL.Query().Get("offset") != strconv.Itoa(offset) {
			t.Errorf("known-page crossover replayed or skipped a query: step=%d URL=%s", step, r.URL)
		}
		id := fmt.Sprintf("known-%d", step)
		if step == 3 {
			id = "new-in-later-query"
		}
		_, _ = fmt.Fprintf(w, `<rss xmlns:x="http://torznab.com/schemas/2015/feed"><channel><x:response offset="%d" total="100"/><item><guid>%s</guid></item></channel></rss>`, offset, id)
	}))
	t.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.PageSize = 1
	p.Search.Categories = []int{2000, 5000}
	p.Schedule.KnownPages = 2
	p.Options = map[string]any{"search_queries": []any{"first", "later"}}
	env := Environment{KnownIDs: func(_ context.Context, ids []string) (map[string]bool, error) {
		known := make(map[string]bool, len(ids))
		for _, id := range ids {
			known[id] = id != "new-in-later-query"
		}
		return known, nil
	}}
	var checkpoint json.RawMessage
	foundNew := false
	for step := range 6 {
		connector := protocolOpen(t, p, model.ModeIncremental, env)
		page, err := connector.Fetch(t.Context(), checkpoint)
		_ = connector.Close()
		if err != nil || page.Done != (step == 5) {
			t.Fatalf("known pages skipped a later query or leaked its streak at step %d: %+v %v", step, page, err)
		}
		if page.Metadata["known_pages_managed"] != true {
			t.Fatal("global known-page stopping could bypass the remaining query plan")
		}
		for _, item := range page.Items {
			if item.SourceID == "new-in-later-query" && item.Error == "" && !item.Ignored {
				foundNew = true
			}
		}
		checkpoint = page.Next
	}
	if !foundNew || searches.Load() != 6 {
		t.Fatalf("later query did not contribute its new identity: found=%t searches=%d", foundNew, searches.Load())
	}
}

func TestTorznabQueryPlanRejectsInvalidStateBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "validation must be offline", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	for _, tc := range []struct {
		name    string
		queries any
		query   string
	}{
		{"null", nil, ""},
		{"scalar", "archive", ""},
		{"object", map[string]any{"query": "archive"}, ""},
		{"empty", []any{}, ""},
		{"nonstring", []any{"archive", 2010}, ""},
		{"null_entry", []any{nil}, ""},
		{"conflict", []any{"archive"}, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := protocolProvider("torznab", server.URL)
			p.Search.Query = tc.query
			p.Options = map[string]any{"search_queries": tc.queries}
			if err := Validate(p); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("invalid query plan accepted: %v", err)
			}
			connector, err := New(t.Context(), p, model.ModeIncremental, Environment{})
			if connector != nil {
				_ = connector.Close()
			}
			if err == nil {
				t.Fatal("invalid query plan opened a collection")
			}
		})
	}
	p := protocolProvider("torznab", server.URL)
	p.Options = map[string]any{"search_queries": []any{"first", "later"}}
	for _, index := range []int{-1, 2, 3} {
		for _, done := range []bool{false, true} {
			connector := protocolOpen(t, p, model.ModeIncremental, Environment{})
			checkpoint := protocolCursor(t, map[string]any{
				"version": 1, "scope": 0, "scopes": [][]int{{2000}},
				"offset": 0, "query_index": index, "done": done,
			})
			if _, err := connector.Fetch(t.Context(), checkpoint); err == nil {
				t.Fatalf("out-of-plan cursor accepted: query_index=%d done=%t", index, done)
			}
			_ = connector.Close()
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid configuration or cursor contacted upstream %d times", requests.Load())
	}
}
