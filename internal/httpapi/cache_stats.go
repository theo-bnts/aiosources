package httpapi

import "net/http"

func (s *server) cacheStatistics(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.AIOSource.CacheStatistics(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
