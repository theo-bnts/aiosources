package httpapi

import (
	"net/http"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerStorageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/storage", s.auth.protect(s.storageSettings))
	mux.HandleFunc("PUT /api/settings/storage", s.auth.protect(s.updateStorageSettings))
	mux.HandleFunc("POST /api/settings/storage/reset-sources", s.auth.protect(s.resetSourceData))
}

func (s *server) storageSettings(w http.ResponseWriter, r *http.Request) {
	result, err := s.options.Store.StorageOverview(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) updateStorageSettings(w http.ResponseWriter, r *http.Request) {
	var input model.StorageSettings
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Mode.Valid() || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "Choose a valid storage mode and include the current revision")
		return
	}
	if err := s.options.Store.UpdateStorageSettings(r.Context(), input); err != nil {
		respondError(w, err)
		return
	}
	s.storageSettings(w, r)
}

func (s *server) resetSourceData(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64 `json:"revision"`
		Confirm  bool  `json:"confirm"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Confirm || input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "Confirm the source data reset with the current storage settings revision")
		return
	}
	if err := s.options.Store.ResetSourceData(r.Context(), input.Revision); err != nil {
		respondError(w, err)
		return
	}
	if s.options.Jobs != nil {
		s.options.Jobs.Notify("providers", "")
	}
	s.storageSettings(w, r)
}
