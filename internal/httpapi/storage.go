package httpapi

import (
	"net/http"
	"slices"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerStorageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/storage", s.auth.protect(s.storageSettings))
	mux.HandleFunc("PUT /api/settings/storage", s.auth.protect(s.updateStorageSettings))
	mux.HandleFunc("POST /api/settings/storage/reset-sources", s.auth.protect(s.resetSourceData))
	mux.HandleFunc("GET /api/settings/storage/indexes", s.auth.protect(s.storageIndexes))
	mux.HandleFunc("PUT /api/settings/storage/indexes", s.auth.protect(s.updateStorageIndexes))
	mux.HandleFunc("GET /api/settings/storage/retention", s.auth.protect(s.storageRetention))
	mux.HandleFunc("PUT /api/settings/storage/retention", s.auth.protect(s.updateStorageRetention))
}

func (s *server) storageRetention(w http.ResponseWriter, r *http.Request) {
	value, err := s.options.Store.StorageRetention(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *server) updateStorageRetention(w http.ResponseWriter, r *http.Request) {
	var value model.StorageRetentionSettings
	if !decodeJSON(w, r, &value) {
		return
	}
	if err := s.options.Store.UpdateStorageRetention(r.Context(), value); err != nil {
		respondError(w, err)
		return
	}
	s.storageRetention(w, r)
}

func (s *server) storageSettings(w http.ResponseWriter, r *http.Request) {
	result, err := s.options.Store.StorageOverview(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	result.AvailableFields = []string{}
	addField := func(field string) {
		if !model.RequiredTorrentField(field) && !slices.Contains(result.AvailableFields, field) {
			result.AvailableFields = append(result.AvailableFields, field)
		}
	}
	if s.options.Providers != nil {
		err = s.options.Providers.WithDefinitions(func(definitions map[string]model.ProviderDocument) error {
			for _, definition := range definitions {
				for field := range definition.Provider.Mapping.Fields {
					addField(field)
				}
				for _, field := range definition.Provider.Output.Fields {
					addField(field)
				}
				if html := definition.Provider.HTML; html != nil {
					for field := range html.Fields {
						addField(field)
					}
					if html.Details != nil {
						for field := range html.Details.Fields {
							addField(field)
						}
					}
				}
			}
			return nil
		})
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if result.Settings.Options != nil {
		for _, field := range result.Settings.Options.AdditionalFields {
			if !slices.Contains(result.AvailableFields, field) {
				result.AvailableFields = append(result.AvailableFields, field)
			}
		}
	}
	slices.Sort(result.AvailableFields)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) storageIndexes(w http.ResponseWriter, r *http.Request) {
	result, err := s.options.Store.StorageIndexes(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *server) updateStorageIndexes(w http.ResponseWriter, r *http.Request) {
	var input model.StorageIndexSettings
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.options.Store.UpdateStorageIndexes(r.Context(), input); err != nil {
		respondError(w, err)
		return
	}
	s.storageIndexes(w, r)
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
