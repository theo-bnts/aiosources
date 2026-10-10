package httpapi

import "net/http"

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
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
