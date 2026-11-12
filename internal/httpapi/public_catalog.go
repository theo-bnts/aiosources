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
		var visitor *aiosource.PublicInstallation
		if len(authorization) == 0 && access.UseInstanceDebrid {
			visitor, err = s.options.AIOSource.PublicInstanceSearch(ctx, s.auth.origin)
		} else {
			if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") {
				writeError(w, 401, "Enter your debrid service keys to use Search.")
				return
			}
			visitor, err = s.options.AIOSource.OpenPublicInstallation(ctx, s.auth.origin, strings.TrimPrefix(authorization[0], "Bearer "))
		}
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
		// this page.
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
	s.registerVisitorCatalog(mux, "/api/public", s.publicCatalogHandler)
	mux.HandleFunc("GET /api/public/catalog-downloads/{token}", s.publicCatalogFile)
}

func (s *server) registerVisitorCatalog(mux *http.ServeMux, prefix string, handler func(time.Duration, publicCatalogAction) http.HandlerFunc) {
	mux.HandleFunc("GET "+prefix+"/catalog/torrents", handler(20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, selection, filters, ok := catalogReleaseInput(w, r)
		if !ok {
			return nil, nil
		}
		if len([]rune(aiosource.Normalize(opts.Query))) < 2 || selection.Season != nil || selection.Episode != nil || selection.File != "" {
			return nil, model.ErrInvalid
		}
		return s.options.AIOSource.CatalogTorrents(r.Context(), opts, filters, visitor)
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/search", handler(10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, filters, ok := catalogSearchInput(w, r)
		if !ok {
			return nil, nil
		}
		return s.options.AIOSource.PublicCatalogSearch(r.Context(), visitor, opts, filters)
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/{id}", handler(10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		if len(r.URL.Query().Get("q")) > 500 {
			return nil, model.ErrInvalid
		}
		return s.options.AIOSource.PublicCatalogTitle(r.Context(), visitor, id, catalogLanguage(r), aiosource.Normalize(r.URL.Query().Get("q")))
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/{id}/releases", handler(20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
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
	mux.HandleFunc("POST "+prefix+"/catalog/{id}/download", handler(65*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
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
			expires, e := s.downloadTicketExpiry(r.Context())
			if e != nil {
				return nil, e
			}
			data, e := json.Marshal(publicCatalogFileTicket{Trusted: visitor.Trusted, Service: result.Service, URL: result.URL, Audience: s.auth.origin, Generation: visitor.Generation, Expires: expires})
			if e != nil {
				return nil, e
			}
			sealed, e := s.options.Vault.Seal("public-catalog-file-v1", data)
			clear(data)
			if e != nil {
				return nil, e
			}
			result.URL = "/api/public/catalog-downloads/" + base64.RawURLEncoding.EncodeToString(sealed)
			result.URLValidSeconds = max(0, int(expires-time.Now().Unix()))
		}
		return result, err
	}))
}

type publicCatalogFileTicket struct {
	Trusted    *model.TrustedGrant `json:"trusted,omitempty"`
	Service    string              `json:"service"`
	URL        string              `json:"url"`
	Audience   string              `json:"audience"`
	Generation int64               `json:"generation"`
	Expires    int64               `json:"expires"`
}

func (s *server) publicCatalogFile(w http.ResponseWriter, r *http.Request) {
	if len(r.PathValue("token")) > 14000 {
		respondError(w, model.ErrNotFound)
		return
	}
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
	if json.Unmarshal(data, &ticket) != nil || ticket.Audience != s.auth.origin || ticket.Expires <= now || ticket.Expires > now+model.MaxDownloadTicketMinutes*60 || !model.IsDebridService(ticket.Service) || !validCatalogFileURL(ticket.URL) {
		respondError(w, model.ErrNotFound)
		return
	}
	release, ok := s.enterConnection(w, r, ticket.Trusted != nil)
	if !ok {
		return
	}
	defer release()
	authorize := func(ctx context.Context) error {
		if ticket.Trusted != nil {
			user, err := s.options.Store.TrustedUser(ctx, *ticket.Trusted)
			if err != nil || !user.SearchEnabled {
				return model.ErrNotFound
			}
			return nil
		}
		if !s.publicOriginAllowed() {
			return model.ErrNotFound
		}
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
