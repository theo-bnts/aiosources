package httpapi

import (
	"net/http"

	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerHistoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/torrents/{id}/history", s.auth.protect(s.torrentHistory))
	mux.HandleFunc("GET /api/torrents/{id}/related", s.auth.protect(s.relatedTorrents))
	mux.HandleFunc("GET /api/runs/{id}/changes", s.auth.protect(s.runChanges))
}

func (s *server) torrentHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !store.ValidOccurrenceID(id) {
		writeError(w, http.StatusNotFound, "Item not found")
		return
	}
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.TorrentHistory(r.Context(), id, options)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) relatedTorrents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !store.ValidOccurrenceID(id) {
		writeError(w, http.StatusNotFound, "Item not found")
		return
	}
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.RelatedTorrents(r.Context(), id, options)
	if err != nil {
		respondError(w, err)
		return
	}
	if err := s.projectTorrentFields(result.Items); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) runChanges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || len(id) > 128 {
		writeError(w, http.StatusNotFound, "Item not found")
		return
	}
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.RunChanges(r.Context(), id, options)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
