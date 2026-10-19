package connectors

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestHTMLConfigurationUsesTheStrictSourceContract(t *testing.T) {
	registry, err := providers.New(t.TempDir(), Validate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	for _, adapter := range Adapters() {
		if result := registry.Validate(adapter.Template); !result.Valid {
			t.Fatalf("source template %s is invalid: %v", adapter.Name, result.Issues)
		}
	}
	for _, test := range []struct {
		name string
		edit func(*model.Provider)
	}{
		{"missing HTML config", func(p *model.Provider) { p.HTML = nil }},
		{"JSON mapping", func(p *model.Provider) { p.Mapping.ID = "/id" }},
		{"JSON items", func(p *model.Provider) { p.HTTP.ItemsPath = "/data" }},
		{"invalid CSS", func(p *model.Provider) { p.HTML.RowsSelector = "[" }},
		{"missing listing marker", func(p *model.Provider) { p.HTML.PageSelector = "" }},
		{"missing next selector", func(p *model.Provider) { p.HTML.NextSelector = "" }},
		{"cursor pagination", func(p *model.Provider) { p.Pagination.Type = "cursor" }},
		{"JSON response pointer", func(p *model.Provider) { p.Pagination.TotalPath = "/total" }},
		{"unsupported option", func(p *model.Provider) { p.Options = map[string]any{"execute_script": true} }},
		{"unsupported format", func(p *model.Provider) { p.HTML.Fields["title"] = model.HTMLField{Format: "script"} }},
		{"conflicting format", func(p *model.Provider) { p.HTML.Fields["title"] = model.HTMLField{Format: "integer"} }},
		{"multiple titles", func(p *model.Provider) { p.HTML.Fields["title"] = model.HTMLField{Selector: ".title", All: true} }},
		{"multiple hashes", func(p *model.Provider) { p.HTML.Fields["info_hash"] = model.HTMLField{Format: "torrent", All: true} }},
		{"invalid attribute", func(p *model.Provider) { p.HTML.ID.Attribute = "data.id" }},
		{"invalid pattern", func(p *model.Provider) { p.HTML.ID.Pattern = "(" }},
		{"ambiguous pattern", func(p *model.Provider) { p.HTML.ID.Pattern = "([a-z]+)([0-9]+)" }},
		{"missing title", func(p *model.Provider) { delete(p.HTML.Fields, "title") }},
		{"invalid date timezone", func(p *model.Provider) {
			p.HTML.Fields["published_at"] = model.HTMLField{Format: "date", Timezone: "Invalid/Timezone"}
		}},
		{"GET body", func(p *model.Provider) { p.HTTP.Body = map[string]any{"q": ""} }},
		{"nonobject form", func(p *model.Provider) { p.HTTP.Method = http.MethodPost; p.HTTP.Body = "q=x" }},
		{"secret literal form", func(p *model.Provider) {
			p.HTTP.Method = http.MethodPost
			p.HTTP.Body = map[string]any{"token": "private"}
		}},
		{"colliding pagination params", func(p *model.Provider) {
			p.Pagination.Type = "page"
			p.Pagination.PageParam, p.Pagination.SizeParam = "position", "position"
		}},
		{"local categories without mapping", func(p *model.Provider) { p.Options = map[string]any{"local_categories": []any{json.Number("1")}} }},
		{"JSON adapter with HTML config", func(p *model.Provider) { p.Adapter = "http_json" }},
		{"Torznab adapter with HTML config", func(p *model.Provider) { p.Adapter = "torznab" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := htmlProvider("https://archive.invalid/catalog")
			test.edit(&p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if Validate(p) == nil || registry.Validate(string(raw)).Valid {
				t.Fatal("invalid HTML source was accepted")
			}
		})
	}
	for _, test := range []struct{ name, fragment string }{
		{"unknown HTML key", `"unexpected":true`},
		{"wrong CSS type", `"rows_selector":42`},
		{"null details", `"details":null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := strings.Replace(htmlTemplate(), `"page_selector": "#catalog",`, `"page_selector": "#catalog",`+test.fragment+`,`, 1)
			if registry.Validate(raw).Valid {
				t.Fatal("HTML document bypassed strict nested schema checks")
			}
		})
	}
}

func FuzzHTMLTorrentMetadataDoesNotPanic(f *testing.F) {
	for _, seed := range [][]byte{nil, []byte("d4:infodee"), []byte("i1e"), []byte("d4:info999999999999999999999999999:x"), []byte("d4:infod4:name0:12:piece length0:6:pieces0:ee")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		hash, err := htmlTorrentHash(data)
		if err == nil {
			if _, err := normalizeField("info_hash", hash, ""); err != nil {
				t.Fatal("torrent parser returned an invalid identity")
			}
		}
	})
}
