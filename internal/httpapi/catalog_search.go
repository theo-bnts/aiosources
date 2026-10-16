package httpapi

import (
	"errors"
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
func (s *server) catalogSearch(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	if len([]rune(aiosource.Normalize(opts.Query))) < 2 {
		writeError(w, http.StatusUnprocessableEntity, "Enter at least two letters or digits to search.")
		return
	}
	items, err := s.options.AIOSource.CatalogSearch(r.Context(), opts, r.URL.Query().Get("type"), catalogLanguage(r))
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
func (s *server) catalogReleases(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r)
	if !ok {
		return
	}
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	selection := model.CatalogSelection{}
	for key, dest := range map[string]**int{"season": &selection.Season, "episode": &selection.Episode} {
		if raw := r.URL.Query().Get(key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				respondError(w, model.ErrInvalid)
				return
			}
			*dest = &n
		}
	}
	opts.Sort = r.URL.Query().Get("sort")
	preferences := model.StreamPreferences{Language: catalogLanguage(r), Mode: r.URL.Query().Get("language_mode")}
	if !aiosource.ValidStreamPreferences(preferences) {
		respondError(w, model.ErrInvalid)
		return
	}
	items, err := s.options.AIOSource.CatalogReleases(r.Context(), id, selection, opts, preferences)
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
	if errors.Is(err, aiosource.ErrPending) {
		w.Header().Set("Retry-After", "15")
		writeJSON(w, http.StatusAccepted, item)
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
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
