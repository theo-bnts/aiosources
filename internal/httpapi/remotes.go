package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/scheduling"
)

func (s *server) registerRemoteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remotes", s.auth.protect(s.listRemotes))
}

type remoteSummary struct {
	ReceivedEntities model.CatalogEntities `json:"received_entities,omitzero"`
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	URL              string                `json:"url"`
	Enabled          bool                  `json:"enabled"`
	Valid            bool                  `json:"valid"`
	Revision         string                `json:"revision"`
	SecretRef        string                `json:"secret_ref"`
	Sync             model.CatalogSync     `json:"sync"`
	SyncState        string                `json:"sync_state"`
	NextSyncAt       *time.Time            `json:"next_sync_at,omitempty"`
	SyncError        string                `json:"sync_error,omitempty"`
	LastRun          *model.Run            `json:"last_run,omitempty"`
	HasCheckpoint    bool                  `json:"has_checkpoint"`
	LastSyncedAt     *time.Time            `json:"last_synced_at,omitempty"`
}

func (s *server) listRemotes(w http.ResponseWriter, r *http.Request) {
	items := []remoteSummary{}
	ids := []string{}
	// Definitions remain the only configuration authority. Take one registry
	// snapshot instead of reopening each JSON file or maintaining another table.
	err := s.options.Providers.WithDefinitions(func(definitions map[string]model.ProviderDocument) error {
		for id, document := range definitions {
			p := document.Provider
			if !p.IsRemoteCatalog() {
				continue
			}
			sync, _ := scheduling.CatalogSynchronization(p)
			items = append(items, remoteSummary{ID: id, Name: p.Name, URL: p.URL, Enabled: p.Enabled, Valid: len(document.Issues) == 0, Revision: document.Revision, SecretRef: p.Auth.SecretRef, Sync: sync, SyncState: "manual"})
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		respondError(w, err)
		return
	}
	clocks, err := s.options.Jobs.CatalogSynchronizations(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	states, err := s.options.Store.RemoteStates(r.Context(), ids)
	if err != nil {
		respondError(w, err)
		return
	}
	for index := range items {
		item := &items[index]
		for _, clock := range clocks {
			if clock.ProviderID != item.ID {
				continue
			}
			item.LastRun = clock.LastRun
			if clock.Revision == item.Revision {
				item.SyncState, item.NextSyncAt, item.SyncError = clock.State, clock.NextRunAt, clock.LastError
			}
			break
		}
		if state, ok := states[item.ID]; ok && state.Endpoint == item.URL && state.Checkpoint != "" {
			item.ReceivedEntities = state.Entities
			item.HasCheckpoint = true
			item.LastSyncedAt = &state.LastSyncedAt
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
