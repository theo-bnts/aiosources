package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func catalogID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 53)
	if err != nil || id < 1 {
		respondError(w, model.ErrNotFound)
		return 0, false
	}
	return id, true
}
func catalogSearchInput(w http.ResponseWriter, r *http.Request) (model.ListOptions, model.CatalogSearchFilters, bool) {
	opts, ok := listOptions(w, r)
	filters := model.CatalogSearchFilters{Type: r.URL.Query().Get("type"), Language: catalogLanguage(r)}
	if !ok {
		return opts, filters, false
	}
	if len([]rune(aiosource.Normalize(opts.Query))) < 2 {
		writeError(w, http.StatusUnprocessableEntity, "Enter at least two letters or digits to search.")
		return opts, filters, false
	}
	if raw := r.URL.Query().Get("year"); raw != "" {
		year, err := strconv.Atoi(raw)
		if err != nil || len(raw) != 4 || year < 1000 || year > 9999 {
			writeError(w, http.StatusUnprocessableEntity, "Enter a single four-digit year.")
			return opts, filters, false
		}
		filters.Year = year
	}
	return opts, filters, true
}

func (s *server) catalogSearch(w http.ResponseWriter, r *http.Request) {
	opts, filters, ok := catalogSearchInput(w, r)
	if !ok {
		return
	}
	items, err := s.options.AIOSource.CatalogSearch(r.Context(), opts, filters)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *server) catalogTitle(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query().Get("q")) > 500 {
		respondError(w, model.ErrInvalid)
		return
	}
	item, err := s.options.AIOSource.CatalogTitle(r.Context(), id, catalogLanguage(r), aiosource.Normalize(r.URL.Query().Get("q")))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func catalogReleaseInput(w http.ResponseWriter, r *http.Request) (model.ListOptions, model.CatalogSelection, model.CatalogReleaseFilters, bool) {
	opts, ok := listOptions(w, r)
	selection := model.CatalogSelection{File: r.URL.Query().Get("file")}
	filters := model.CatalogReleaseFilters{}
	if !ok {
		return opts, selection, filters, false
	}
	for key, dest := range map[string]**int{"season": &selection.Season, "episode": &selection.Episode} {
		if raw := r.URL.Query().Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				respondError(w, model.ErrInvalid)
				return opts, selection, filters, false
			}
			*dest = &n
		}
	}
	opts.Sort = r.URL.Query().Get("sort")
	preferences := model.StreamPreferences{Language: catalogLanguage(r), Mode: r.URL.Query().Get("language_mode")}
	if raw := r.URL.Query().Get("sorting"); raw != "" {
		if len(raw) > 512 {
			respondError(w, model.ErrInvalid)
			return opts, selection, filters, false
		}
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&preferences.Sorting) != nil || decoder.Decode(new(any)) != io.EOF {
			respondError(w, model.ErrInvalid)
			return opts, selection, filters, false
		}
	}
	if raw := r.URL.Query().Get("response_seconds"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			respondError(w, model.ErrInvalid)
			return opts, selection, filters, false
		}
		preferences.ResponseSeconds = n
	}
	if !aiosource.ValidStreamPreferences(preferences) {
		respondError(w, model.ErrInvalid)
		return opts, selection, filters, false
	}
	filters.StreamPreferences = preferences
	if raw := r.URL.Query().Get("cached_only"); raw != "" {
		cached, err := strconv.ParseBool(raw)
		if err != nil {
			respondError(w, model.ErrInvalid)
			return opts, selection, filters, false
		}
		filters.CachedOnly = cached
	}
	return opts, selection, filters, true
}

func (s *server) catalogReleases(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	opts, selection, filters, ok := catalogReleaseInput(w, r)
	if !ok {
		return
	}
	items, err := s.options.AIOSource.CatalogReleases(r.Context(), id, selection, opts, filters)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *server) catalogDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	var input model.CatalogDownloadInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.options.AIOSource.CatalogDownload(r.Context(), id, input)
	s.writeCatalogDownload(w, r, item, err)
}

func catalogLanguage(r *http.Request) string {
	value := r.URL.Query().Get("language")
	if value == "" {
		value = strings.Split(strings.Split(r.Header.Get("Accept-Language"), ",")[0], ";")[0]
	}
	if lang := aiosource.NormalizeLanguage(value); lang != "" {
		return lang
	}
	return "en"
}
