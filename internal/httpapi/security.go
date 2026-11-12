package httpapi

import (
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"net/http"
	"time"
)

func (s *server) registerSecurityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/security/sessions", s.auth.protect(s.securitySessions))
	mux.HandleFunc("DELETE /api/security/sessions/{id}", s.auth.protect(s.securityRevokeSession))
	mux.HandleFunc("POST /api/security/sessions/revoke-others", s.auth.protect(s.securityRevokeOthers))
}

func securityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrAuthentication):
		writeError(w, http.StatusForbidden, "Your session is no longer valid. Sign in again.")
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "Security settings changed. Refresh and try again.")
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "Session not found")
	case errors.Is(err, model.ErrInvalid):
		writeError(w, http.StatusBadRequest, "Invalid security operation")
	default:
		writeError(w, http.StatusServiceUnavailable, "Security storage is unavailable. No change was confirmed.")
	}
}

func currentSession(r *http.Request) session { return r.Context().Value(sessionContextKey{}).(session) }

func (s *server) securitySessions(w http.ResponseWriter, r *http.Request) {
	values, err := s.options.Store.ListAdminSessions(r.Context(), currentSession(r).id)
	if err != nil {
		securityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (s *server) securityRevokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(id) != 32 {
		writeError(w, http.StatusBadRequest, "Invalid session identifier")
		return
	}
	value := currentSession(r)
	if err := s.options.Store.RevokeAdminSessions(r.Context(), value.id, id, false); err != nil {
		securityError(w, err)
		return
	}
	if id == value.id {
		s.auth.cookie(w, "", time.Unix(1, 0), -1)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) securityRevokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := s.options.Store.RevokeAdminSessions(r.Context(), currentSession(r).id, "", true); err != nil {
		securityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
