package httpapi

import (
	"net/http"
)

func (s *server) registerSystemHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system-health", s.auth.protect(s.systemHealth))
}

func (s *server) systemHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.options.Health == nil {
		writeError(w, http.StatusServiceUnavailable, "System health monitoring is unavailable")
		return
	}
	report := s.options.Health.Report()
	if report == nil {
		writeError(w, http.StatusServiceUnavailable, "The first health measurement is still pending")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
