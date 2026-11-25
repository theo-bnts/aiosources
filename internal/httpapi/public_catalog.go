package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

type publicCatalogAction func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation) (any, error)

func optionalBearerCredential(r *http.Request) (string, bool, bool) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", true, false
	}
	return strings.TrimPrefix(values[0], "Bearer "), true, true
}

func publicCatalogDisabledMessage(feature string) string {
	if feature == "trending" {
		return "Public Trending is disabled"
	}
	return "Public Search is disabled"
}

func publicCatalogCredentialMessage(feature string) string {
	if feature == "trending" {
		return "Enter your debrid service keys to use Trending."
	}
	return "Enter your debrid service keys to use Search."
}

func (s *server) publicCatalogHandler(feature string, budget time.Duration, action publicCatalogAction) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.publicOriginAllowed() {
			writeError(w, 404, publicCatalogDisabledMessage(feature))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.hasPublicOrigin(r) {
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
		if !access.Enabled || !access.Allows(feature) {
			writeError(w, 404, publicCatalogDisabledMessage(feature))
			return
		}
		token, present, valid := optionalBearerCredential(r)
		var visitor *aiosource.PublicInstallation
		if !present && access.UseInstanceDebrid {
			visitor, err = s.options.AIOSource.PublicInstanceCatalog(ctx, s.auth.origin, feature)
		} else {
			if !present || !valid {
				writeError(w, 401, publicCatalogCredentialMessage(feature))
				return
			}
			visitor, err = s.options.AIOSource.OpenPublicInstallation(ctx, s.auth.origin, token)
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
		if !latest.Enabled || !latest.Allows(feature) {
			writeError(w, 404, publicCatalogDisabledMessage(feature))
			return
		}
		if latest.Generation != visitor.Generation {
			writeError(w, 401, "Your saved service configuration is no longer available. Enter your keys again.")
			return
		}
		writeVisitorCatalogResult(w, result, actionErr)
	}
}

func writeVisitorCatalogResult(w http.ResponseWriter, result any, err error) {
	if err != nil && !errors.Is(err, aiosource.ErrPending) {
		respondError(w, err)
		return
	}
	status := http.StatusOK
	if errors.Is(err, aiosource.ErrPending) {
		status = http.StatusAccepted
		w.Header().Set("Retry-After", "15")
	}
	writeJSON(w, status, result)
}

func (s *server) registerPublicCatalog(mux *http.ServeMux) {
	s.registerVisitorCatalog(mux, "/api/public", s.publicCatalogHandler)
	mux.HandleFunc("GET /api/public/catalog-downloads/{token}", s.publicCatalogFile)
}

func (s *server) registerVisitorCatalog(mux *http.ServeMux, prefix string, handler func(string, time.Duration, publicCatalogAction) http.HandlerFunc) {
	mux.HandleFunc("GET "+prefix+"/catalog/torrents", handler("search", 20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, selection, filters, ok := catalogReleaseInput(w, r)
		if !ok {
			return nil, nil
		}
		if len([]rune(aiosource.Normalize(opts.Query))) < 2 || selection.Season != nil || selection.Episode != nil || selection.File != "" {
			return nil, model.ErrInvalid
		}
		return s.options.AIOSource.CatalogTorrents(r.Context(), opts, filters, visitor)
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/search", handler("search", 10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, filters, ok := catalogSearchInput(w, r)
		if !ok {
			return nil, nil
		}
		return s.options.AIOSource.PublicCatalogSearch(r.Context(), visitor, opts, filters)
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/trending", handler("trending", 30*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		opts, ok := catalogTrendingInput(w, r)
		if !ok {
			return nil, nil
		}
		return s.options.AIOSource.PublicCatalogTrending(r.Context(), visitor, opts)
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/{id}", handler("browse", 10*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		if len(r.URL.Query().Get("q")) > 500 {
			return nil, model.ErrInvalid
		}
		return s.options.AIOSource.PublicCatalogTitle(r.Context(), visitor, id, catalogLanguage(r), aiosource.Normalize(r.URL.Query().Get("q")))
	}))
	mux.HandleFunc("GET "+prefix+"/catalog/{id}/releases", handler("browse", 20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
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
	mux.HandleFunc("POST "+prefix+"/catalog/{id}/download", handler("browse", 65*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) (any, error) {
		id, ok := catalogID(w, r)
		if !ok {
			return nil, nil
		}
		ticketFeature := "browse"
		if id == 0 {
			ticketFeature = "search"
			if visitor.Trusted != nil {
				if !currentTrustedSession(r).User.Allows("search") {
					writeError(w, 403, "This feature is not enabled for your account")
					return nil, nil
				}
			} else {
				access, err := s.options.Store.PublicAccess(r.Context())
				if err != nil {
					return nil, err
				}
				if !access.Enabled || !access.SearchEnabled {
					writeError(w, 404, publicCatalogDisabledMessage("search"))
					return nil, nil
				}
			}
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
			audience := s.auth.origin
			sessionID := ""
			if visitor.Trusted != nil {
				ts, ok := r.Context().Value(trustedSessionKey{}).(trustedSession)
				if !ok || ts.ID == "" {
					return nil, aiosource.ErrAuth
				}
				audience = s.trustedSearchAudience()
				sessionID = ts.ID
			}
			result.URL, result.URLValidSeconds, e = s.sealDownloadTicket("public-catalog-file-v1", "/api/public/catalog-downloads/", expires, publicCatalogFileTicket{
				Trusted:    visitor.Trusted,
				Session:    sessionID,
				Feature:    ticketFeature,
				Service:    result.Service,
				URL:        result.URL,
				Audience:   audience,
				Generation: visitor.Generation,
				Expires:    expires,
			})
			if e != nil {
				return nil, e
			}
		}
		return result, err
	}))
}

type publicCatalogFileTicket struct {
	Trusted    *model.TrustedGrant `json:"trusted,omitempty"`
	Session    string              `json:"session,omitempty"`
	Feature    string              `json:"feature,omitempty"`
	Service    string              `json:"service"`
	URL        string              `json:"url"`
	Audience   string              `json:"audience"`
	Generation int64               `json:"generation"`
	Expires    int64               `json:"expires"`
}

func (s *server) publicCatalogFile(w http.ResponseWriter, r *http.Request) {
	var ticket publicCatalogFileTicket
	now := time.Now().Unix()
	if !s.openDownloadTicket("public-catalog-file-v1", r.PathValue("token"), &ticket) {
		respondError(w, model.ErrNotFound)
		return
	}
	expectedAudience := s.auth.origin
	if ticket.Trusted != nil {
		expectedAudience = s.trustedSearchAudience()
	}
	if expectedAudience == "" || ticket.Audience != expectedAudience || !model.IsDebridService(ticket.Service) || !validDownloadTicket(ticket.URL, ticket.Expires, now) {
		respondError(w, model.ErrNotFound)
		return
	}
	release, ok := s.enterConnection(w, r, ticket.Trusted != nil)
	if !ok {
		return
	}
	defer release()
	feature := ticket.Feature
	if feature == "" {
		feature = "search"
	}
	authorize := func(ctx context.Context) error {
		if ticket.Trusted != nil {
			if ticket.Session == "" {
				return model.ErrNotFound
			}
			ts, err := s.auth.lookupTrusted(r.WithContext(ctx))
			if errors.Is(err, store.ErrAuthentication) {
				return model.ErrNotFound
			}
			if err != nil {
				return err
			}
			if ts.ID != ticket.Session || ts.User.ID != ticket.Trusted.UserID || ts.User.Generation != ticket.Trusted.Generation || !ts.User.Allows(feature) {
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
		if !access.Enabled || !access.Allows(feature) || access.Generation != ticket.Generation {
			return model.ErrNotFound
		}
		return nil
	}
	if err := authorize(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	s.deliverCatalogFile(w, r, catalogFileTicket{Service: ticket.Service, URL: ticket.URL, Expires: ticket.Expires}, authorize)
}
