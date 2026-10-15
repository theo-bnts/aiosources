package httpapi

import (
	"github.com/theo-bnts/aiosources/internal/model"
	"net/http"
)

func (s *server) sourceTorrentStats(w http.ResponseWriter, r *http.Request) {
	sources, err := s.options.Providers.List()
	if err != nil {
		respondError(w, err)
		return
	}
	ids := make([]string, len(sources))
	for index, source := range sources {
		ids[index] = source.ID
	}
	items, err := s.options.Store.SourceTorrentStats(r.Context(), ids)
	if err != nil {
		respondError(w, err)
		return
	}
	if s.options.Health != nil {
		if report := s.options.Health.Report(); report != nil && report.Database.Available && report.Database.MeasuredAt != nil {
			estimates := make(map[string]int64, len(report.SourceStorage))
			for _, estimate := range report.SourceStorage {
				estimates[estimate.ProviderID] = estimate.Bytes
			}
			for i := range items.Items {
				if size, ok := estimates[items.Items[i].ProviderID]; ok {
					items.Items[i].Storage = &model.SourceStorageUsage{Bytes: size, MeasuredAt: *report.Database.MeasuredAt}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, items)
}
