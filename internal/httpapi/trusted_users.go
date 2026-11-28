package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerTrustedUsers(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/trusted/login", s.trustedLogin)
	mux.HandleFunc("POST /api/trusted/logout", s.auth.protectTrusted("", s.trustedLogout))
	mux.HandleFunc("GET /api/trusted-users", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		items, err := s.options.Store.TrustedUsers(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	}))
	save := s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			model.TrustedUser
			TrendingEnabled      *bool  `json:"trending_enabled"`
			SearchEnabled        *bool  `json:"search_enabled"`
			TorznabEnabled       *bool  `json:"torznab_enabled"`
			StremioEnabled       *bool  `json:"stremio_enabled"`
			CatalogEnabled       *bool  `json:"catalog_enabled"`
			AlertsEnabled        *bool  `json:"alerts_enabled"`
			Password             string `json:"password"`
			UseInstanceDebrid    *bool  `json:"use_instance_debrid"`
			UseInstanceAnnounces *bool  `json:"use_instance_announces"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if !decodeJSON(w, r, &input) {
			return
		}
		input.ID = r.PathValue("id")
		defaultEnabled := input.ID == ""
		input.TrustedUser.TrendingEnabled = boolOrDefault(input.TrendingEnabled, defaultEnabled)
		input.TrustedUser.SearchEnabled = boolOrDefault(input.SearchEnabled, defaultEnabled)
		input.TrustedUser.TorznabEnabled = boolOrDefault(input.TorznabEnabled, defaultEnabled)
		input.TrustedUser.StremioEnabled = boolOrDefault(input.StremioEnabled, defaultEnabled)
		input.TrustedUser.CatalogEnabled = boolOrDefault(input.CatalogEnabled, defaultEnabled)
		input.TrustedUser.AlertsEnabled = boolOrDefault(input.AlertsEnabled, false)
		input.TrustedUser.UseInstanceDebrid = boolOrDefault(input.UseInstanceDebrid, defaultEnabled)
		input.TrustedUser.UseInstanceAnnounces = boolOrDefault(input.UseInstanceAnnounces, defaultEnabled)
		if _, err := store.NormalizeTrustedEmail(input.Email); err != nil {
			writeError(w, 422, "Enter a valid email address")
			return
		}
		hash := ""
		if input.Password != "" {
			release, ok := s.auth.admitPasswordRequestFor(w, false)
			if !ok {
				return
			}
			defer release()
			var err error
			if !s.auth.runPasswordWork(r.Context(), false, func() {
				hash, err = hashTrustedPassword(input.Password)
			}) {
				return
			}
			input.Password = ""
			if err != nil {
				writeError(w, 422, "Use a password with at least 12 characters and at most 1024 bytes")
				return
			}
		}
		value, err := s.options.Store.SaveTrustedUser(r.Context(), input.TrustedUser, hash)
		if errors.Is(err, store.ErrTrustedEmailConflict) {
			writeError(w, 409, "An account already uses this email address")
			return
		}
		if err != nil {
			respondError(w, err)
			return
		}
		status := 200
		if r.Method == http.MethodPost {
			status = 201
		}
		writeJSON(w, status, value)
	})
	mux.HandleFunc("POST /api/trusted-users", save)
	mux.HandleFunc("PUT /api/trusted-users/{id}", save)
	mux.HandleFunc("DELETE /api/trusted-users/{id}", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		revision, ok := decodeRevision(w, r)
		if !ok {
			return
		}
		if err := s.options.Store.DeleteTrustedUser(r.Context(), r.PathValue("id"), revision); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/trusted/config", s.auth.protectTrusted("", s.trustedConfig))
	mux.HandleFunc("POST /api/trusted/connections/{feature}", s.auth.protectTrusted("", s.trustedConnection))
	for path, feature := range map[string]string{"/api/trusted/install": "stremio", "/api/trusted/credentials": "browse", "/api/trusted/torznab/install": "torznab"} {
		mux.HandleFunc("POST "+path, s.auth.protectTrusted("", func(w http.ResponseWriter, r *http.Request) {
			r.SetPathValue("feature", feature)
			s.trustedConnection(w, r)
		}))
	}
	s.registerVisitorCatalog(mux, "/api/trusted", s.trustedCatalogHandler)
	s.registerTrustedAlerts(mux)
	s.registerTrustedStremio(mux)
	mux.HandleFunc("/trusted/torznab/api", s.publicTorznabFeed)
	mux.HandleFunc("/trusted/catalogs/{configuration}", s.publicCatalogFeed)
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func trustedGrant(r *http.Request) model.TrustedGrant {
	user := currentTrustedSession(r).User
	return model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
}

func (s *server) trustedSearchAudience() string {
	if origin := s.connectionOrigin(); origin != "" {
		return origin
	}
	return "trusted-browser"
}

// Trusted accounts use the same bounded work capacity as private connections,
// rather than consuming anonymous visitors' per-address request quotas.
func (s *server) reserveTrusted() (func(), int, error) {
	pool := s.trustedStreams
	if pool == nil {
		pool = s.streams
	}
	select {
	case pool <- struct{}{}:
		return func() { <-pool }, 0, nil
	default:
		return nil, 1, nil
	}
}

func (s *server) enterConnection(w http.ResponseWriter, r *http.Request, trusted bool) (func(), bool) {
	if !trusted {
		return s.public.enter(w, r, publicAddon)
	}
	release, _, _ := s.reserveTrusted()
	if release == nil {
		w.Header().Set("Retry-After", "1")
		writeError(w, 429, "Too many active requests. Try again shortly.")
		return nil, false
	}
	return release, true
}

func (s *server) trustedConfig(w http.ResponseWriter, r *http.Request) {
	user := currentTrustedSession(r).User
	config, err := s.options.Store.AIOConfig(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	providers := availablePublicProviders(config, !user.UseInstanceDebrid)
	info := map[string]any{"enabled": true, "trusted": true, "credential_scope": "trusted:" + user.ID + ":" + strconv.FormatInt(user.Revision, 10), "use_instance_debrid": user.UseInstanceDebrid, "use_instance_announces": user.UseInstanceAnnounces, "providers": providers, "trending_enabled": user.TrendingEnabled, "search_enabled": user.SearchEnabled, "stremio_enabled": user.StremioEnabled, "torznab_enabled": user.TorznabEnabled, "catalog_enabled": user.Allows("catalog"), "alerts_enabled": user.AlertsEnabled}
	sources, e := s.options.AIOSource.TrustedSources(r.Context(), trustedGrant(r))
	if e != nil {
		respondError(w, e)
		return
	}
	info["sources"] = sources
	if !user.UseInstanceAnnounces {
		private, e := s.options.AIOSource.TrustedPrivateSources(r.Context(), trustedGrant(r))
		if e != nil {
			respondError(w, e)
			return
		}
		info["private_sources"] = private
	}
	if user.CatalogEnabled {
		if err := s.populateCatalogPolicyConfig(r.Context(), info, sources); err != nil {
			respondError(w, err)
			return
		}
	}
	writeJSON(w, 200, info)
}

func (s *server) trustedConnection(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	user := currentTrustedSession(r).User
	if !user.Allows(feature) {
		writeError(w, 403, "This connection is not enabled for your account")
		return
	}
	origin := s.connectionOrigin()
	if origin == "" {
		writeError(w, 422, "The administrator must configure the instance connection address")
		return
	}
	var input struct {
		aiosource.PublicCredentialChanges
		Preferences model.StreamPreferences    `json:"preferences"`
		Options     model.PublicTorznabOptions `json:"options"`
		Catalog     model.PublicCatalogOptions `json:"catalog"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if !decodeJSON(w, r, &input) {
		return
	}
	defer func() { clear(input.Services); clear(input.Announces) }()
	if (feature == "torznab" || feature == "catalog") && (len(input.Services) > 0 || len(input.RemoveServices) > 0) ||
		feature == "catalog" && (len(input.Announces) > 0 || len(input.RemoveAnnounces) > 0) {
		publicCredentialError(w, model.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	personal, err := s.options.AIOSource.ResolveTrustedCredential(ctx, trustedGrant(r), origin, input.PublicCredentialChanges)
	if err != nil {
		publicCredentialError(w, err)
		return
	}
	defer func() { clear(personal.Services); clear(personal.Announces) }()
	if feature == "search" || feature == "trending" || feature == "browse" || feature == "stremio" {
		if err = s.options.AIOSource.ValidatePersonalServices(ctx, personal, true); err != nil {
			respondError(w, err)
			return
		}
	}
	token, err := s.options.AIOSource.CreateTrustedConnectionWithCredentials(ctx, trustedGrant(r), origin, feature, input.Preferences, input.Options, input.Catalog, personal)
	if err != nil {
		respondError(w, err)
		return
	}
	remembered, err := s.options.AIOSource.TrustedBrowserCredential(ctx, trustedGrant(r), origin, personal)
	if err != nil {
		respondError(w, err)
		return
	}
	result := map[string]any{"credential": remembered.Credential, "services": remembered.Services, "announces": remembered.Announces, "instance_services": remembered.InstanceServices}
	switch feature {
	case "search", "trending", "browse":
	case "stremio":
		result["manifest_url"] = origin + "/trusted/stremio/" + token + "/manifest.json"
	case "torznab":
		result["url"], result["api_key"] = origin+"/trusted/torznab/api", token
	case "catalog":
		configuration, e := aiosource.EncodePublicCatalogOptions(input.Catalog)
		if e != nil {
			respondError(w, e)
			return
		}
		result["url"], result["password"] = origin+"/trusted/catalogs/"+configuration, token
	default:
		writeError(w, 422, "Invalid connection type")
		return
	}
	writeJSON(w, 200, result)
}

func (s *server) trustedCatalogHandler(feature string, budget time.Duration, action publicCatalogAction) http.HandlerFunc {
	return s.auth.protectTrusted(feature, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		r = r.WithContext(ctx)
		release, ok := s.enterConnection(w, r, true)
		if !ok {
			return
		}
		defer release()
		grant := trustedGrant(r)
		audience := s.trustedSearchAudience()
		visitor := &aiosource.PublicInstallation{Trusted: &grant, Generation: grant.Generation, Audience: audience}
		metadataOnly := r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/releases") && r.URL.Path != "/api/trusted/catalog/torrents"
		if !metadataOnly {
			credential, _, valid := optionalBearerCredential(r)
			if !valid {
				publicCredentialError(w, aiosource.ErrAuth)
				return
			}
			personal, e := s.options.AIOSource.ResolveTrustedCredential(ctx, grant, s.connectionOrigin(), aiosource.PublicCredentialChanges{Credential: credential})
			if e != nil {
				publicCredentialError(w, e)
				return
			}
			defer func() { clear(personal.Services); clear(personal.Announces) }()
			var err error
			visitor, err = s.options.AIOSource.TrustedVisitorWithCredentials(ctx, grant, audience, feature, model.StreamPreferences{}, personal.Services, personal.Announces)
			if err != nil {
				publicCredentialError(w, err)
				return
			}
		}
		defer func() { clear(visitor.Services); clear(visitor.Announces) }()
		result, err := action(w, r, visitor)
		if result == nil && err == nil {
			return
		}
		user, e := s.options.Store.TrustedUser(ctx, grant)
		if e != nil {
			if errors.Is(e, store.ErrAuthentication) {
				writeError(w, 401, "Your account changed. Sign in again.")
			} else {
				respondError(w, e)
			}
			return
		}
		if !user.Allows(feature) {
			writeError(w, 403, "This feature is not enabled for your account")
			return
		}
		writeVisitorCatalogResult(w, result, err)
	})
}
