package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerPublicSharing(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/public-catalog", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		policy, err := s.options.Store.PublicCatalogPolicy(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		providers, err := s.options.Providers.List()
		if err != nil {
			respondError(w, err)
			return
		}
		sources := []sharingSource{}
		for _, p := range providers {
			if p.Valid {
				sources = append(sources, sharingSource{ID: p.ID, Name: p.Name})
			}
		}
		writeJSON(w, 200, map[string]any{"policy": policy, "fields": store.CatalogFields(), "sources": sources})
	}))
	mux.HandleFunc("PUT /api/public-catalog", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		var policy model.PublicCatalogPolicy
		if !decodeJSON(w, r, &policy) {
			return
		}
		if len(policy.Sources) > 0 {
			providers, err := s.options.Providers.List()
			if err != nil {
				respondError(w, err)
				return
			}
			valid := make(map[string]bool, len(providers))
			for _, provider := range providers {
				valid[provider.ID] = provider.Valid
			}
			for _, id := range policy.Sources {
				if !valid[id] {
					writeError(w, 422, "Every selected source must be an existing valid provider")
					return
				}
			}
		}
		if err := s.options.Store.SetPublicCatalogPolicy(r.Context(), policy); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/public/catalogs/install", func(w http.ResponseWriter, r *http.Request) {
		if !s.publicOriginAllowed() {
			writeError(w, 404, "Public access is disabled")
			return
		}
		if r.Header.Get("Origin") != s.auth.origin {
			writeError(w, 403, "Request origin is not allowed")
			return
		}
		release, ok := s.public.enter(w, r, publicSetup)
		if !ok {
			return
		}
		defer release()
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var options model.PublicCatalogOptions
		if !decodeJSON(w, r, &options) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		configuration, password, err := s.options.AIOSource.CreatePublicCatalog(ctx, s.auth.origin, options)
		if err != nil {
			if errors.Is(err, store.ErrCatalogAuthentication) {
				writeError(w, 422, "Selected catalog data is no longer available")
			} else {
				respondError(w, err)
			}
			return
		}
		if err = s.options.Store.RecordActivity(ctx, "debug", "catalog.public_link_created", "", "", "", map[string]any{"records": 0}); err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"url": s.auth.origin + "/public/catalogs/" + configuration, "password": password})
	})
	mux.HandleFunc("/public/catalogs/{configuration}", s.publicCatalogFeed)
}

func (s *server) publicCatalogFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Authorization, "+model.CatalogEntitiesHeader+", "+model.CatalogDetailsHeader)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, 405, "Shared catalogs are read-only")
		return
	}
	if !s.publicOriginAllowed() {
		writeError(w, 404, "Public access is disabled")
		return
	}
	release, ok := s.public.enter(w, r, publicAddon)
	if !ok {
		return
	}
	defer release()
	authorizations := r.Header.Values("Authorization")
	if len(authorizations) != 1 {
		catalogFeedError(w, store.ErrCatalogAuthentication)
		return
	}
	parts := strings.Fields(authorizations[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		catalogFeedError(w, store.ErrCatalogAuthentication)
		return
	}
	budget, err := s.options.Store.ConnectionTimeout(r.Context(), "catalog")
	if err != nil {
		respondError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, 503, "Catalog delivery is unavailable")
		return
	}
	grant, err := s.options.AIOSource.OpenPublicCatalog(ctx, s.auth.origin, r.PathValue("configuration"), parts[1])
	if err != nil {
		if errors.Is(err, aiosource.ErrAuth) || errors.Is(err, model.ErrNotFound) {
			catalogFeedError(w, store.ErrCatalogAuthentication)
		} else {
			respondError(w, err)
		}
		return
	}
	request, ok := catalogPageInput(w, r)
	if !ok {
		return
	}
	start := time.Now()
	result, revision, err := s.options.Store.PublicCatalogPage(ctx, grant, request)
	if err != nil {
		_ = s.options.Store.RecordActivity(ctx, "warn", "catalog.public_failed", "", "", "", map[string]any{"duration_ms": time.Since(start).Milliseconds()})
		catalogFeedError(w, err)
		return
	}
	var body []byte
	if !request.Head {
		body, err = json.Marshal(result)
		if err != nil {
			writeError(w, 500, "Could not encode the catalog response")
			return
		}
	}
	records := len(result.Items)
	if request.Head {
		records = 0
	}
	if err = s.options.Store.RecordActivity(ctx, "debug", "catalog.public_exported", "", "", "", map[string]any{"records": records, "duration_ms": time.Since(start).Milliseconds(), "done": result.NextCursor == ""}); err != nil {
		respondError(w, err)
		return
	}
	if err = s.options.Store.ValidatePublicCatalog(ctx, grant, revision); err != nil {
		catalogFeedError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if !request.Head {
		if _, err = w.Write(body); err != nil {
			return
		}
	}
	_ = controller.Flush()
}
