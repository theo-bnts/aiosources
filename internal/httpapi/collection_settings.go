package httpapi

import (
	"net/http"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerCollectionSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/collections", s.auth.protect(s.collectionSettings))
	mux.HandleFunc("PUT /api/settings/collections", s.auth.protect(s.updateCollectionSettings))
}

func (s *server) collectionSettings(w http.ResponseWriter, r *http.Request) {
	overview, err := s.options.Store.CollectionOverview(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *server) updateCollectionSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Workers             *int    `json:"workers"`
		MaxQuotaRetries     *int    `json:"max_quota_retries"`
		MaxQuotaWaitSeconds *int    `json:"max_quota_wait_seconds"`
		NoProgressRequests  *int    `json:"no_progress_requests"`
		NoProgressAction    *string `json:"no_progress_action"`
		Revision            *int64  `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Workers == nil || input.MaxQuotaRetries == nil || input.MaxQuotaWaitSeconds == nil ||
		input.NoProgressRequests == nil || input.NoProgressAction == nil || input.Revision == nil {
		writeError(w, http.StatusBadRequest, "A complete collection settings object and revision are required")
		return
	}
	settings := model.CollectionSettings{
		Workers: *input.Workers, MaxQuotaRetries: *input.MaxQuotaRetries,
		MaxQuotaWaitSeconds: *input.MaxQuotaWaitSeconds,
		NoProgressRequests:  *input.NoProgressRequests, NoProgressAction: *input.NoProgressAction,
		Revision: *input.Revision,
	}
	if !settings.Valid() {
		writeError(w, http.StatusBadRequest, "Collection settings contain an invalid value or revision")
		return
	}
	if err := s.options.Store.UpdateCollectionSettings(r.Context(), settings); err != nil {
		respondError(w, err)
		return
	}
	s.collectionSettings(w, r)
}
