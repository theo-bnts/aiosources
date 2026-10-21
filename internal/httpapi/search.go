package httpapi

import (
	"net/http"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func torrentListOptions(w http.ResponseWriter, r *http.Request) (model.ListOptions, bool) {
	options, ok := listOptions(w, r, "provider")
	if !ok {
		return options, false
	}
	query := r.URL.Query()
	for key, values := range query {
		switch key {
		case "provider":
		case "q", "category", "min_size", "max_size", "published_after", "published_before", "info_hash", "sort", "limit", "offset":
			if len(values) != 1 {
				writeError(w, http.StatusBadRequest, "Search filters must have a single value except provider")
				return options, false
			}
		default:
			writeError(w, http.StatusBadRequest, "Unknown search filter")
			return options, false
		}
	}
	providers := []string{}
	for _, provider := range query["provider"] {
		if provider != "" {
			providers = append(providers, provider)
		}
	}
	filters, err := store.NormalizeSearchFilters(model.SearchFilters{Query: query.Get("q"), Providers: providers,
		Category: query.Get("category"), MinSize: query.Get("min_size"), MaxSize: query.Get("max_size"),
		PublishedAfter: query.Get("published_after"), PublishedBefore: query.Get("published_before"),
		InfoHash: query.Get("info_hash"), Sort: query.Get("sort")})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid search filters: check decimal bounds, RFC3339 dates, hash and sort")
		return options, false
	}
	limit, offset := options.Limit, options.Offset
	options = filters.ListOptions()
	options.Limit = limit
	options.Offset = offset
	return options, true
}

// Both normal search and related-record responses honor the existing local
// source output projection. Imported occurrences already carry remote fields.
func (s *server) projectTorrentFields(items []model.Torrent) error {
	definitions, err := s.options.Providers.List()
	if err != nil {
		return err
	}
	type projection struct {
		fields     []string
		attributes []string
	}
	projections := make(map[string]projection, len(definitions))
	for _, definition := range definitions {
		if definition.Valid && len(definition.OutputFields) != 0 {
			var selected projection
			allAttributes := false
			for _, name := range definition.OutputFields {
				if attribute, nested := strings.CutPrefix(name, "attributes."); nested {
					if !allAttributes {
						selected.attributes = append(selected.attributes, attribute)
					}
				} else {
					selected.fields = append(selected.fields, name)
					if name == "attributes" {
						allAttributes = true
						selected.attributes = nil
					}
				}
			}
			projections[definition.ID] = selected
		}
	}
	for index := range items {
		if items[index].Origin != nil {
			continue
		}
		if fields, present := projections[items[index].ProviderID]; present {
			capacity := len(fields.fields)
			if len(fields.attributes) > 0 {
				capacity++
			}
			selected := make(map[string]any, capacity)
			for _, name := range fields.fields {
				if value, present := items[index].Fields[name]; present {
					selected[name] = value
				}
			}
			attributes, _ := items[index].Fields["attributes"].(map[string]any)
			var selectedAttributes map[string]any
			for _, name := range fields.attributes {
				if value, present := attributes[name]; present {
					if selectedAttributes == nil {
						selectedAttributes = make(map[string]any, len(fields.attributes))
					}
					selectedAttributes[name] = value
				}
			}
			if selectedAttributes != nil {
				selected["attributes"] = selectedAttributes
			}
			items[index].Fields = selected
		}
	}
	return nil
}
