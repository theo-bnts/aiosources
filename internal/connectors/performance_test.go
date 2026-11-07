package connectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Includes transport, lossless response extraction, normalization and cursor
// validation. No external source or quota is involved.
func BenchmarkJSONCollectionPage(b *testing.B) {
	items := make([]map[string]any, 100)
	for i := range items {
		items[i] = map[string]any{"id": i + 1, "title": fmt.Sprintf("Synthetic.Film.%d.2026", i),
			"metadata": map[string]any{"size": 123456789, "hash": fmt.Sprintf("%040x", i+1), "imdb": "tt1234567", "tmdb": 101, "languages": []string{"fr", "en"}, "categories": []int{2000}}}
	}
	body, _ := json.Marshal(map[string]any{"data": items})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	b.Cleanup(server.Close)
	p := protocolProvider("http_json", server.URL)
	p.PageSize, p.HTTP.ItemsPath, p.Pagination.Type = 100, "/data", "none"
	p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title", "size": "/metadata/size", "info_hash": "/metadata/hash", "imdb_id": "/metadata/imdb", "tmdb_id": "/metadata/tmdb", "languages": "/metadata/languages", "categories": "/metadata/categories"}}
	c, err := New(b.Context(), p, model.ModeFull, Environment{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = c.Close() })
	b.ResetTimer()
	for b.Loop() {
		page, err := c.Fetch(b.Context(), nil)
		if err != nil || len(page.Items) != 100 || !page.Done || page.Items[0].Error != "" {
			b.Fatal("invalid page", err)
		}
	}
	b.ReportMetric(100, "records/page")
}

func BenchmarkHTMLCollectionPage(b *testing.B) {
	var rows strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&rows, `<article data-id="%d"><span class="title">Synthetic Documentary %d</span><span class="size">1 GiB</span><a class="magnet" href="magnet:?xt=urn:btih:%040x">Download</a></article>`, i, i, i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { htmlResponse(w, htmlListing(rows.String())) }))
	b.Cleanup(server.Close)
	p := htmlProvider(server.URL)
	p.PageSize = 100
	c, err := New(b.Context(), p, model.ModeFull, Environment{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = c.Close() })
	b.ResetTimer()
	for b.Loop() {
		page, err := c.Fetch(b.Context(), nil)
		if err != nil || !page.Done || len(page.Items) != 100 || page.Items[99].Fields["info_hash"] != fmt.Sprintf("%040x", 100) {
			b.Fatal("HTML traversal lost fields or completion", err)
		}
	}
	b.ReportMetric(100, "records/page")
}

func BenchmarkTorznabCollectionPage(b *testing.B) {
	var items strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&items, `<item><title>Synthetic Documentary %d</title><guid>fixture-%d</guid><size>1024</size><torznab:attr name="infohash" value="%040x"/></item>`, i, i, i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("t") == "caps" {
			fmt.Fprint(w, strings.ReplaceAll(protocolCaps, `"2"`, `"100"`))
			return
		}
		if r.URL.Query().Get("offset") == "100" {
			fmt.Fprint(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><torznab:response offset="100" total="100"/></channel></rss>`)
			return
		}
		fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><torznab:response offset="0" total="100"/>%s</channel></rss>`, items.String())
	}))
	b.Cleanup(server.Close)
	p := protocolProvider("torznab", server.URL)
	p.PageSize = 100
	c, err := New(b.Context(), p, model.ModeFull, Environment{})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = c.Close() })
	b.ResetTimer()
	for b.Loop() {
		page, err := c.Fetch(b.Context(), nil)
		records := 0
		for _, record := range page.Items {
			if record.Auxiliary {
				continue
			}
			records++
			if record.Error != "" || record.Fields["info_hash"] == "" {
				b.Fatal("Torznab record lost required fields")
			}
		}
		if err != nil || records != 100 {
			b.Fatal("Torznab traversal lost fields or completion", err)
		}
		if !page.Done {
			last, err := c.Fetch(b.Context(), page.Next)
			if err != nil || !last.Done || len(last.Items) != 0 {
				b.Fatal("Torznab traversal did not confirm its end", err)
			}
		}
	}
	b.ReportMetric(100, "records/page")
}
