package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestTorrentOutputAttributeSelection(t *testing.T) {
	const source = `{"title":"Release","size":0,"attributes":{"imdbid":["tt0111161"],"tmdbid":["278","279"],"download":["private-value"]}}`
	for _, test := range []struct {
		name   string
		fields string
		source string
		want   string
	}{
		{"selected only", "title, size, attributes.imdbid, attributes.tmdbid, attributes.tvdbid", source, `{"title":"Release","size":0,"attributes":{"imdbid":["tt0111161"],"tmdbid":["278","279"]}}`},
		{"whole object first", "attributes, attributes.imdbid", source, `{"attributes":{"imdbid":["tt0111161"],"tmdbid":["278","279"],"download":["private-value"]}}`},
		{"whole object last", "attributes.imdbid, attributes", source, `{"attributes":{"imdbid":["tt0111161"],"tmdbid":["278","279"],"download":["private-value"]}}`},
		{"missing selection", "attributes.tvdbid", source, `{}`},
		{"non-object attributes", "title, attributes.imdbid", `{"title":"Release","attributes":"legacy value"}`, `{"title":"Release"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := providers.New(t.TempDir(), connectors.Validate)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			raw, err := json.Marshal(map[string]any{
				"version": 1, "id": "fixture", "name": "Fixture", "adapter": "torznab",
				"url": "https://source.invalid/api", "enabled": false,
				"output": model.Output{Fields: strings.Split(test.fields, ", ")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Save("fixture", string(raw), ""); err != nil {
				t.Fatal(err)
			}
			var original, untouched, want map[string]any
			for _, value := range []struct {
				raw    string
				target *map[string]any
			}{{test.source, &original}, {test.source, &untouched}, {test.want, &want}} {
				if err := json.Unmarshal([]byte(value.raw), value.target); err != nil {
					t.Fatal(err)
				}
			}
			items := []model.Torrent{
				{ProviderID: "fixture", Fields: original},
				{ProviderID: "fixture", Fields: original, Origin: &model.CatalogOrigin{InstanceID: "remote", ProviderID: "upstream", SourceID: "release"}},
			}
			s := &server{options: Options{Providers: registry}}
			if err := s.projectTorrentFields(items); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(items[0].Fields, want) {
				t.Fatalf("selected fields = %#v, want %#v", items[0].Fields, want)
			}
			if !reflect.DeepEqual(original, untouched) {
				t.Fatalf("output selection changed retained metadata: %#v", original)
			}
			if !reflect.DeepEqual(items[1].Fields, untouched) {
				t.Fatalf("local output selection changed imported fields: %#v", items[1].Fields)
			}
		})
	}
}

func TestSearchRejectsMalformedQueryInsteadOfDroppingFilters(t *testing.T) {
	fixture := sharingAPI(t, "https://owner.example.test")
	for _, query := range []string{"q=private%zz", "provider=private;provider=public"} {
		response := fixture.request(http.MethodGet, "/api/torrents?"+query, "", true, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed search query was silently broadened: status %d", response.Code)
		}
	}
	response := fixture.request(http.MethodGet, "/api/torrents?q=Release%3B100%25&provider=fixture&provider=other", "", true, "")
	if response.Code != http.StatusOK {
		t.Fatalf("escaped punctuation or repeated providers was rejected: status %d", response.Code)
	}
}
