package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
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
			SearchEnabled        *bool  `json:"search_enabled"`
			TorznabEnabled       *bool  `json:"torznab_enabled"`
			StremioEnabled       *bool  `json:"stremio_enabled"`
			CatalogEnabled       *bool  `json:"catalog_enabled"`
			Password             string `json:"password"`
			UseInstanceDebrid    *bool  `json:"use_instance_debrid"`
			UseInstanceAnnounces *bool  `json:"use_instance_announces"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if !decodeJSON(w, r, &input) {
			return
		}
		input.ID = r.PathValue("id")
		if input.SearchEnabled == nil {
			input.TrustedUser.SearchEnabled = input.ID == ""
		} else {
			input.TrustedUser.SearchEnabled = *input.SearchEnabled
		}
		if input.TorznabEnabled == nil {
			input.TrustedUser.TorznabEnabled = input.ID == ""
		} else {
			input.TrustedUser.TorznabEnabled = *input.TorznabEnabled
		}
		if input.StremioEnabled == nil {
			input.TrustedUser.StremioEnabled = input.ID == ""
		} else {
			input.TrustedUser.StremioEnabled = *input.StremioEnabled
		}
		if input.CatalogEnabled == nil {
			input.TrustedUser.CatalogEnabled = input.ID == ""
		} else {
			input.TrustedUser.CatalogEnabled = *input.CatalogEnabled
		}
		if input.UseInstanceDebrid == nil {
			input.TrustedUser.UseInstanceDebrid = input.ID == ""
		} else {
			input.TrustedUser.UseInstanceDebrid = *input.UseInstanceDebrid
		}
		if input.UseInstanceAnnounces == nil {
			input.TrustedUser.UseInstanceAnnounces = input.ID == ""
		} else {
			input.TrustedUser.UseInstanceAnnounces = *input.UseInstanceAnnounces
		}
		if _, err := store.NormalizeTrustedEmail(input.Email); err != nil {
			writeError(w, 422, "Enter a valid email address")
			return
		}
		hash := ""
		if input.Password != "" {
			if !s.auth.admitPasswordRequest(w) {
				return
			}
			defer func() { <-s.auth.passwordRequests }()
			select {
			case s.auth.passwordChecks <- struct{}{}:
			case <-r.Context().Done():
				return
			}
			var err error
			hash, err = hashTrustedPassword(input.Password)
			<-s.auth.passwordChecks
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
		var input struct {
			Revision int64 `json:"revision"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := s.options.Store.DeleteTrustedUser(r.Context(), r.PathValue("id"), input.Revision); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/trusted/config", s.auth.protectTrusted("", s.trustedConfig))
	mux.HandleFunc("POST /api/trusted/connections/{feature}", s.auth.protectTrusted("", s.trustedConnection))
	for path, feature := range map[string]string{"/api/trusted/install": "stremio", "/api/trusted/credentials": "search", "/api/trusted/torznab/install": "torznab"} {
		mux.HandleFunc("POST "+path, s.auth.protectTrusted("", func(w http.ResponseWriter, r *http.Request) {
			r.SetPathValue("feature", feature)
			s.trustedConnection(w, r)
		}))
	}
	s.registerVisitorCatalog(mux, "/api/trusted", s.trustedCatalogHandler)
	s.registerTrustedStremio(mux)
	mux.HandleFunc("/trusted/torznab/api", s.publicTorznabFeed)
	mux.HandleFunc("/trusted/catalogs/{configuration}", s.publicCatalogFeed)
}

func trustedGrant(r *http.Request) model.TrustedGrant {
	user := r.Context().Value(trustedSessionKey{}).(trustedSession).User
	return model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
}

// Trusted accounts use the same bounded work capacity as private connections,
// rather than consuming anonymous visitors' per-address request quotas.
func (s *server) reserveTrusted() (func(), int, error) {
	select {
	case s.streams <- struct{}{}:
		return func() { <-s.streams }, 0, nil
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
	user := r.Context().Value(trustedSessionKey{}).(trustedSession).User
	config, err := s.options.Store.AIOConfig(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	providers := []aiosource.PublicProvider{}
	for _, p := range aiosource.PublicProviders {
		if !user.UseInstanceDebrid || config.DebridSecret(p.ID) != "" {
			providers = append(providers, p)
		}
	}
	info := map[string]any{"enabled": true, "trusted": true, "credential_scope": "trusted:" + user.ID + ":" + strconv.FormatInt(user.Revision, 10), "use_instance_debrid": user.UseInstanceDebrid, "use_instance_announces": user.UseInstanceAnnounces, "providers": providers, "search_enabled": user.SearchEnabled, "stremio_enabled": user.StremioEnabled, "torznab_enabled": user.TorznabEnabled, "catalog_enabled": user.Allows("catalog")}
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
		policy, e := s.options.Store.PublicCatalogPolicy(r.Context())
		if e != nil {
			respondError(w, e)
			return
		}
		info["catalog_fields"], info["catalog_entities"], info["catalog_revision"] = policy.Fields, policy.CatalogEntities, policy.Revision
		permitted := []aiosource.PublicPrivateSource{}
		for _, source := range sources {
			if len(policy.Sources) == 0 || slices.Contains(policy.Sources, source.ID) {
				permitted = append(permitted, source)
			}
		}
		info["catalog_sources"] = permitted
	}
	writeJSON(w, 200, info)
}

func (s *server) trustedConnection(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	user := r.Context().Value(trustedSessionKey{}).(trustedSession).User
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
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	personal, err := s.options.AIOSource.ResolveTrustedCredential(ctx, trustedGrant(r), origin, input.PublicCredentialChanges)
	if err != nil {
		publicCredentialError(w, err)
		return
	}
	defer func() { clear(personal.Services); clear(personal.Announces) }()
	if feature == "search" || feature == "stremio" {
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
	case "search":
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

func (s *server) trustedCatalogHandler(budget time.Duration, action publicCatalogAction) http.HandlerFunc {
	return s.auth.protectTrusted("search", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		r = r.WithContext(ctx)
		release, ok := s.enterConnection(w, r, true)
		if !ok {
			return
		}
		defer release()
		grant := trustedGrant(r)
		visitor := &aiosource.PublicInstallation{Trusted: &grant, Generation: grant.Generation, Audience: s.connectionOrigin()}
		metadataOnly := r.Method == http.MethodGet && !strings.HasSuffix(r.URL.Path, "/releases") && r.URL.Path != "/api/trusted/catalog/torrents"
		if !metadataOnly {
			personal, e := s.options.AIOSource.ResolveTrustedCredential(ctx, grant, s.connectionOrigin(), aiosource.PublicCredentialChanges{Credential: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")})
			if e != nil {
				publicCredentialError(w, e)
				return
			}
			defer func() { clear(personal.Services); clear(personal.Announces) }()
			var err error
			visitor, err = s.options.AIOSource.TrustedVisitorWithCredentials(ctx, grant, s.connectionOrigin(), "search", model.StreamPreferences{}, personal.Services, personal.Announces)
			if err != nil {
				respondError(w, err)
				return
			}
		}
		defer func() { clear(visitor.Services); clear(visitor.Announces) }()
		result, err := action(w, r, visitor)
		if result == nil && err == nil {
			return
		}
		if _, e := s.options.Store.TrustedUser(ctx, grant); e != nil {
			if errors.Is(e, store.ErrAuthentication) {
				writeError(w, 401, "Your account changed. Sign in again.")
			} else {
				respondError(w, e)
			}
			return
		}
		if err != nil && !errors.Is(err, aiosource.ErrPending) {
			respondError(w, err)
			return
		}
		status := 200
		if errors.Is(err, aiosource.ErrPending) {
			status = 202
			w.Header().Set("Retry-After", "15")
		}
		writeJSON(w, status, result)
	})
}
