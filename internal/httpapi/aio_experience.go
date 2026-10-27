package httpapi

import (
	"net/http"
	"strconv"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) aioReviewDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.options.Store.AIOReviewDetail(r.Context(), r.PathValue("id"), catalogLanguage(r))
	if err != nil {
		respondError(w, err)
		return
	}
	parsed := aiosource.ParseRelease(detail.Record.Title)
	detail.SuggestedQuery, detail.SuggestedYear = parsed.Title, parsed.Year
	if title, year, book := aiosource.BookSuggestion(detail.Record); book {
		detail.SuggestedQuery, detail.SuggestedYear = title, year
		detail.BookEvidence = detail.Record.BookHints
	}
	writeJSON(w, 200, detail)
}
func (s *server) aioReviewCandidates(w http.ResponseWriter, r *http.Request) {
	filters := model.CatalogSearchFilters{Language: catalogLanguage(r)}
	if raw := r.URL.Query().Get("year"); raw != "" {
		year, err := strconv.Atoi(raw)
		if err != nil || len(raw) != 4 || year < 1000 || year > 9999 {
			respondError(w, model.ErrInvalid)
			return
		}
		filters.Year = year
	}
	items, err := s.options.AIOSource.ReviewCandidates(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("type"), filters)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (s *server) aioReviewSave(w http.ResponseWriter, r *http.Request) {
	var input model.AIOReviewInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.options.AIOSource.SaveReview(r.Context(), r.PathValue("id"), input); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *server) stremioStatistics(w http.ResponseWriter, r *http.Request) {
	item, err := s.options.Store.StremioStatistics(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
func (s *server) stremioRequests(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	origin, kind := r.URL.Query().Get("origin"), r.URL.Query().Get("kind")
	if (origin != "" && origin != "private" && origin != "public") || (kind != "" && kind != "stream" && kind != "manifest" && kind != "playback") {
		writeError(w, http.StatusBadRequest, "Invalid request filters")
		return
	}
	items, err := s.options.Store.ListStremioRequestsFiltered(r.Context(), opts, origin, kind)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, items)
}
func (s *server) stremioRequest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(w, model.ErrNotFound)
		return
	}
	item, err := s.options.Store.StremioRequest(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
