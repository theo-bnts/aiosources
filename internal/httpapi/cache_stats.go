package httpapi

import (
	"net/http"
	"time"
)

func (s *server) cacheStatistics(w http.ResponseWriter, r *http.Request) {
	config, err := s.options.Store.AIOConfig(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	items, err := s.options.Store.SharedAvailabilityStats(r.Context(), time.Now().Add(-time.Duration(config.CacheMinutes)*time.Minute))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
