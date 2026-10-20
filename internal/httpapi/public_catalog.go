package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

type publicCatalogAction func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation) (any, error)

func (s *server) publicCatalogHandler(budget time.Duration, action publicCatalogAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.publicOriginAllowed() {
			writeError(w, 404, "Public Search is disabled")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != s.auth.origin {
			writeError(w, 403, "Request origin is not allowed")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		r = r.WithContext(ctx)
		release, ok := s.public.enter(w, r, publicAddon)
		if !ok {
			return
		}
		defer release()
		access, err := s.options.Store.PublicAccess(ctx)
		if err != nil {
			respondError(w, err)
			return
		}
		if !access.Enabled || !access.SearchEnabled {
			writeError(w, 404, "Public Search is disabled")
			return
		}
		authorization := r.Header.Values("Authorization")
		if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") {
			writeError(w, 401, "Enter your debrid service keys to use Search.")
			return
		}
		visitor, err := s.options.AIOSource.OpenPublicInstallation(ctx, s.auth.origin, strings.TrimPrefix(authorization[0], "Bearer "))
		if err != nil {
			if errors.Is(err, aiosource.ErrAuth) {
				writeError(w, 401, "Your saved service configuration is no longer available. Enter your keys again.")
			} else {
				respondError(w, err)
			}
			return
		}
		defer clear(visitor.Services)
		defer clear(visitor.Announces)
		result, actionErr := action(w, r, visitor)
		if result == nil && actionErr == nil {
			return
		}
		// A slow provider response cannot outlive the permission that authorized
		// this page or reveal names after source disclosure was withdrawn.
		latest, err := s.options.Store.PublicAccess(ctx)
		if err != nil {
			respondError(w, err)
			return
		}
		if !latest.Enabled || !latest.SearchEnabled {
			writeError(w, 404, "Public Search is disabled")
			return
		}
		if latest.Generation != visitor.Generation {
			writeError(w, 401, "Your saved service configuration is no longer available. Enter your keys again.")
			return
		}
		if access.ExposeSources && !latest.ExposeSources {
			writeError(w, 409, "Public source visibility changed. Search again.")
			return
		}
		if actionErr != nil && !errors.Is(actionErr, aiosource.ErrPending) {
			respondError(w, actionErr)
			return
		}
		status := http.StatusOK
		if errors.Is(actionErr, aiosource.ErrPending) {
			status = http.StatusAccepted
			w.Header().Set("Retry-After", "15")
		}
		writeJSON(w, status, result)
	}
}

func (s *server) registerPublicCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/public/catalog/search", s.publicCatalogHandler(10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, filters, ok := catalogSearchInput(w, r)
		if !ok {
			return nil, nil
		}
		return s.options.AIOSource.PublicCatalogSearch(r.Context(), visitor, opts, filters)
	}))
	mux.HandleFunc("GET /api/public/catalog/{id}", s.publicCatalogHandler(10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		if len(r.URL.Query().Get("q")) > 500 {
			return nil, model.ErrInvalid
		}
		return s.options.AIOSource.PublicCatalogTitle(r.Context(), visitor, id, catalogLanguage(r), aiosource.Normalize(r.URL.Query().Get("q")))
	}))
	mux.HandleFunc("GET /api/public/catalog/{id}/releases", s.publicCatalogHandler(20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		opts, selection, filters, ok := catalogReleaseInput(w, r)
		if !ok {
			return nil, nil
		}
		return s.options.AIOSource.PublicCatalogReleases(r.Context(), visitor, id, selection, opts, filters)
	}))
	mux.HandleFunc("POST /api/public/catalog/{id}/download", s.publicCatalogHandler(65*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		var input model.CatalogDownloadInput
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if !decodeJSON(w, r, &input) {
			return nil, nil
		}
		result, err := s.options.AIOSource.PublicCatalogDownload(r.Context(), visitor, id, input)
		if err != nil && !errors.Is(err, aiosource.ErrPending) {
			return nil, err
		}
		if result.URL != "" {
			if !validCatalogFileURL(result.URL) {
				return nil, aiosource.ErrInvalidResponse
			}
			data, e := json.Marshal(publicCatalogFileTicket{Service: result.Service, URL: result.URL, Audience: s.auth.origin, Generation: visitor.Generation, Expires: time.Now().Add(10 * time.Minute).Unix()})
			if e != nil {
				return nil, e
			}
			sealed, e := s.options.Vault.Seal("public-catalog-file-v1", data)
			clear(data)
			if e != nil {
				return nil, e
			}
			result.URL = "/api/public/catalog-downloads/" + base64.RawURLEncoding.EncodeToString(sealed)
		}
		return result, err
	}))
	mux.HandleFunc("GET /api/public/catalog-downloads/{token}", s.publicCatalogFile)
}

type publicCatalogFileTicket struct {
	Service    string `json:"service"`
	URL        string `json:"url"`
	Audience   string `json:"audience"`
	Generation int64  `json:"generation"`
	Expires    int64  `json:"expires"`
}

func (s *server) publicCatalogFile(w http.ResponseWriter, r *http.Request) {
	if !s.publicOriginAllowed() || len(r.PathValue("token")) > 14000 {
		respondError(w, model.ErrNotFound)
		return
	}
	release, ok := s.public.enter(w, r, publicAddon)
	if !ok {
		return
	}
	defer release()
	sealed, err := base64.RawURLEncoding.DecodeString(r.PathValue("token"))
	if err != nil {
		respondError(w, model.ErrNotFound)
		return
	}
	data, err := s.options.Vault.Open("public-catalog-file-v1", sealed)
	if err != nil {
		respondError(w, model.ErrNotFound)
		return
	}
	defer clear(data)
	var ticket publicCatalogFileTicket
	now := time.Now().Unix()
	if json.Unmarshal(data, &ticket) != nil || ticket.Audience != s.auth.origin || ticket.Expires <= now || ticket.Expires > now+600 || (ticket.Service != "alldebrid" && ticket.Service != "torbox") || !validCatalogFileURL(ticket.URL) {
		respondError(w, model.ErrNotFound)
		return
	}
	authorize := func(ctx context.Context) error {
		access, e := s.options.Store.PublicAccess(ctx)
		if e != nil {
			return e
		}
		if !access.Enabled || !access.SearchEnabled || access.Generation != ticket.Generation {
			return model.ErrNotFound
		}
		return nil
	}
	if err = authorize(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	s.deliverCatalogFile(w, r, catalogFileTicket{Service: ticket.Service, URL: ticket.URL, Expires: ticket.Expires}, authorize)
}
