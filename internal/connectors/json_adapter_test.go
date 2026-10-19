package connectors

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestJSONAdapterReadsExistingDefinitionsWithoutRewritingThem(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":9007199254740993,"title":"Example release","size":1024}]`))
	}))
	t.Cleanup(upstream.Close)
	for _, adapter := range []string{"json", "http_json"} {
		t.Run(adapter, func(t *testing.T) {
			registry, err := providers.New(t.TempDir(), Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			p := protocolProvider(adapter, upstream.URL)
			p.Search.Categories = []int{}
			p.HTTP.Method = http.MethodGet
			p.Pagination.Type = "none"
			p.Mapping = model.Mapping{ID: "/id", Fields: map[string]string{"title": "/title", "size": "/size"}}
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if result := registry.Validate(string(raw)); !result.Valid {
				t.Fatal(result.Issues)
			}
			document, err := registry.Save(p.ID, string(raw), "")
			if err != nil {
				t.Fatal(err)
			}
			connector := protocolOpen(t, p, model.ModeFull, Environment{})
			page, err := connector.Fetch(t.Context(), nil)
			if err != nil || !page.Done || len(page.Items) != 1 || page.Items[0].SourceID != "9007199254740993" || page.Items[0].Fields["title"] != "Example release" {
				t.Fatalf("JSON compatibility or exact native ID lost: %+v %v", page, err)
			}
			after, err := registry.Get(p.ID)
			if err != nil || after.JSON != document.JSON || after.Revision != document.Revision || p.Adapter != adapter {
				t.Fatal("reading a source rewrote its definition", err)
			}
		})
	}
}

func TestSourceTemplatesUseTheCanonicalAdapterAndRequestedOrder(t *testing.T) {
	want := []string{"Torznab", "HTML", "JSON", "UNIT3D"}
	adapters := Adapters()
	if len(adapters) != len(want) {
		t.Fatal("unexpected template count", len(adapters))
	}
	for i, adapter := range adapters {
		var p model.Provider
		if adapter.Name != want[i] || json.Unmarshal([]byte(adapter.Template), &p) != nil || p.Adapter != adapter.Type || p.Adapter == "http_json" {
			t.Fatalf("incorrect source template at %d: %s", i, adapter.Name)
		}
	}
}
