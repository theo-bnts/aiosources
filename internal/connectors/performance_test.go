package connectors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
