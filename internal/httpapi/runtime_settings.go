package httpapi

import (
	"fmt"
	"net/http"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) sourceSizeAllowed(w http.ResponseWriter, r *http.Request, definition string) bool {
	kib, err := s.options.Store.RuntimeValue(r.Context(), "payloads", "source_kib")
	if err != nil {
		respondError(w, err)
		return false
	}
	if len(definition) > kib*1024 {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("The source definition exceeds this instance's %d KiB limit", kib))
		return false
	}
	return true
}

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
	if r.PathValue("section") == "identification" {
		writeJSON(w, http.StatusOK, struct {
			model.RuntimeSection
			AutomaticWorkers int `json:"automatic_workers"`
		}{value, s.options.Store.AIOMatchWorkers()})
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
