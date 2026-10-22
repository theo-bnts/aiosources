package httpapi

import (
	"net/http"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerRuntimeSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/runtime/{section}", s.auth.protect(s.runtimeSettings))
	mux.HandleFunc("PUT /api/settings/runtime/{section}", s.auth.protect(s.saveRuntimeSettings))
}

func (s *server) runtimeSettings(w http.ResponseWriter, r *http.Request) {
	value, err := s.options.Store.RuntimeSection(r.Context(), r.PathValue("section"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) saveRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	var input model.RuntimeSection
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.options.Store.UpdateRuntimeSection(r.Context(), r.PathValue("section"), input); err != nil {
		respondError(w, err)
		return
	}
	s.runtimeSettings(w, r)
}
