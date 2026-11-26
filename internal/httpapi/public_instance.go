package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) publicOriginAllowed() bool {
	u, err := url.Parse(s.auth.origin)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && ip != nil && ip.IsLoopback()
}
func (s *server) registerPublicInstance(mux *http.ServeMux) {
	s.registerPublicTorznab(mux)
	s.registerPublicCatalog(mux)
	s.registerPublicSharing(mux)
	mux.HandleFunc("GET /api/public-access", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		value, err := s.options.Store.PublicAccess(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/public-access", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			model.PublicAccess
			TrendingEnabled *bool `json:"trending_enabled"`
			StremioEnabled  *bool `json:"stremio_enabled"`
			CatalogEnabled  *bool `json:"catalog_enabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		value := input.PublicAccess
		if input.TrendingEnabled == nil || input.StremioEnabled == nil || input.CatalogEnabled == nil {
			// A settings form opened before the upgrade must not revoke Trending, Stremio
			// or catalog access merely because it does not yet contain the new control.
			current, err := s.options.Store.PublicAccess(r.Context())
			if err != nil {
				respondError(w, err)
				return
			}
			if input.TrendingEnabled == nil {
				input.TrendingEnabled = &current.TrendingEnabled
			}
			if input.StremioEnabled == nil {
				input.StremioEnabled = &current.StremioEnabled
			}
			if input.CatalogEnabled == nil {
				input.CatalogEnabled = &current.CatalogEnabled
			}
		}
		value.TrendingEnabled = *input.TrendingEnabled
		value.StremioEnabled = *input.StremioEnabled
		value.CatalogEnabled = *input.CatalogEnabled
		if value.Enabled && !s.publicOriginAllowed() {
			writeError(w, 400, "Set PUBLIC_URL to the external HTTPS origin before enabling public access.")
			return
		}
		if err := s.options.Store.SetPublicAccess(r.Context(), value); err != nil {
			respondError(w, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/public/config", func(w http.ResponseWriter, r *http.Request) {
		// Listing consented source names reads definitions too. Bound this work
		// like other anonymous requests, without charging homepage refreshes
		// against the visitor's setup or addon request quota.
		release, ok := s.public.enter(w, r, publicConfig)
		if !ok {
			return
		}
		defer release()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		value, err := s.options.Store.PublicAccess(ctx)
		if err != nil {
			respondError(w, err)
			return
		}
		enabled := value.Enabled && s.publicOriginAllowed()
		info := map[string]any{"enabled": enabled, "use_instance_debrid": enabled && value.UseInstanceDebrid, "use_instance_announces": enabled && value.UseInstanceAnnounces, "providers": aiosource.PublicProviders, "stremio_enabled": enabled && value.StremioEnabled, "torznab_enabled": enabled && value.TorznabEnabled, "trending_enabled": enabled && value.TrendingEnabled, "search_enabled": enabled && value.SearchEnabled, "catalog_enabled": enabled && value.CatalogEnabled}
		var allSources []aiosource.PublicPrivateSource
		if enabled {
			var e error
			allSources, e = s.options.AIOSource.PublicSources(ctx)
			if e != nil {
				respondError(w, e)
				return
			}
			info["sources"] = allSources
			sources, err := s.options.AIOSource.PublicPrivateSources(ctx)
			if err != nil {
				respondError(w, err)
				return
			}
			if len(sources) > 0 && !value.UseInstanceAnnounces {
				info["private_sources"] = sources
			}
		}
		if enabled && value.UseInstanceDebrid {
			config, e := s.options.Store.AIOConfig(ctx)
			if e != nil {
				respondError(w, e)
				return
			}
			info["providers"] = availablePublicProviders(config, false)
		}
		if enabled && value.CatalogEnabled {
			if err := s.populateCatalogPolicyConfig(ctx, info, allSources); err != nil {
				respondError(w, err)
				return
			}
		}
		writeJSON(w, 200, info)
	})
	install := func(stremio bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !s.publicOriginAllowed() {
				writeError(w, 404, "Public access is disabled")
				return
			}
			if !s.hasPublicOrigin(r) {
				writeError(w, 403, "Request origin is not allowed")
				return
			}
			release, ok := s.public.enter(w, r, publicSetup)
			if !ok {
				return
			}
			defer release()
			var input struct {
				aiosource.PublicCredentialChanges
				Preferences model.StreamPreferences `json:"preferences"`
			}
			defer func() { clear(input.Services); clear(input.Announces) }()
			// A remembered encrypted configuration and replacement values can be
			// larger than either final URL. The sealed result keeps its own hard cap.
			r.Body = http.MaxBytesReader(w, r.Body, 2*aiosource.MaxPublicInstallationSize)
			if !decodeJSON(w, r, &input) {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			if !aiosource.ValidStreamPreferences(input.Preferences) {
				respondError(w, model.ErrInvalid)
				return
			}
			saved, err := s.options.AIOSource.ResolvePublicCredential(ctx, s.auth.origin, input.PublicCredentialChanges)
			if err != nil {
				publicCredentialError(w, err)
				return
			}
			defer func() { clear(saved.Services); clear(saved.Announces) }()
			hasAnnounces := len(saved.Announces) > 0
			create := s.options.AIOSource.CreatePublicInstallation
			if !stremio {
				create = s.options.AIOSource.CreatePublicSearchCredential
			}
			configuration, err := create(ctx, s.auth.origin, saved.Services, saved.Announces)
			if err == nil {
				configuration, err = aiosource.WithInstallationPreferences(configuration, input.Preferences)
			}
			if err == nil {
				err = s.options.AIOSource.CheckPublicConfigurationSize(ctx, "stremio", configuration)
			}
			if err != nil {
				if errors.Is(err, model.ErrNotFound) {
					writeError(w, 404, "Public access is disabled")
				} else if hasAnnounces && errors.Is(err, model.ErrInvalid) {
					writeError(w, 400, "Check the service keys and complete announce URLs. Visitor announce URLs must be enabled for the selected private sources.")
				} else {
					writeError(w, 400, "Could not validate the service keys. Check the keys, subscription and provider connection.")
				}
				return
			}
			credential, err := s.options.AIOSource.PublicBrowserCredential(ctx, s.auth.origin, saved)
			if err != nil {
				respondError(w, err)
				return
			}
			manifest := ""
			if stremio {
				manifest = s.auth.origin + "/public/stremio/" + configuration + "/manifest.json"
			}
			writeJSON(w, 200, struct {
				aiosource.PublicBrowserCredential
				ManifestURL string `json:"manifest_url"`
			}{credential, manifest})
		}
	}
	mux.HandleFunc("POST /api/public/install", install(true))
	mux.HandleFunc("POST /api/public/credentials", install(false))
	protect := func(manifest bool, next func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if !s.publicOriginAllowed() {
				writeError(w, 401, "Invalid public installation")
				return
			}
			release, ok := s.public.enter(w, r, publicAddon)
			if !ok {
				return
			}
			defer release()
			budget, err := s.options.Store.ConnectionTimeout(r.Context(), "stremio")
			if err != nil {
				respondError(w, err)
				return
			}
			if manifest {
				budget = min(budget, 5*time.Second)
			}
			ctx, cancel := context.WithTimeout(r.Context(), budget)
			defer cancel()
			r = r.WithContext(ctx)
			deadline, _ := ctx.Deadline()
			if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
				respondError(w, err)
				return
			}
			visitor, err := s.options.AIOSource.OpenPublicInstallation(r.Context(), s.auth.origin, r.PathValue("configuration"))
			if err != nil {
				writeError(w, 401, "Invalid public installation")
				return
			}
			defer func() { clear(visitor.Services); clear(visitor.Announces) }()
			if err := s.options.AIOSource.CheckPublicStremio(ctx, visitor); err != nil {
				writeError(w, 401, "Invalid public installation")
				return
			}
			next(w, r, visitor)
		}
	}
	s.registerVisitorStremioRoutes(
		mux,
		"/public/stremio",
		"public",
		"/public/connections/stremio",
		func() string { return s.auth.origin },
		func(w http.ResponseWriter, r *http.Request) {
			s.options.AIOSource.RecordAddonRequest("public", "stream", r.PathValue("type"), "", time.Now(), model.ErrInvalid)
			writeError(w, 400, "Invalid stream resource")
		},
		func(w http.ResponseWriter, _ error) {
			writeError(w, 401, "Invalid public installation")
		},
		protect,
	)
}

func (s *server) registerVisitorStremioRoutes(
	mux *http.ServeMux,
	prefix string,
	historyOrigin string,
	configureTarget string,
	originFn func() string,
	onInvalidStream func(http.ResponseWriter, *http.Request),
	onAuthFail func(http.ResponseWriter, error),
	protect func(bool, func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc,
) {
	mux.HandleFunc("GET "+prefix+"/{configuration}/manifest.json", protect(true, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		s.options.AIOSource.RecordAddonRequest(historyOrigin, "manifest", "", "", time.Now(), nil)
		writeJSON(w, 200, aiosource.InstallationManifest(visitor.Preferences))
	}))
	mux.HandleFunc("GET "+prefix+"/{configuration}/configure", protect(true, func(w http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) {
		http.Redirect(w, r, configureTarget, http.StatusSeeOther)
	}))
	mux.HandleFunc("GET "+prefix+"/{configuration}/stream/{type}/{id}", protect(false, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		resourceID, ok := parseStremioStreamResourceID(r)
		if !ok {
			onInvalidStream(w, r)
			return
		}
		streams, err := s.options.AIOSource.PublicStreams(r.Context(), originFn(), r.PathValue("configuration"), r.PathValue("type"), resourceID, visitor)
		if err != nil {
			respondError(w, err)
			return
		}
		if err := s.options.AIOSource.CheckPublicStremio(r.Context(), visitor); err != nil {
			onAuthFail(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"streams": streams})
	}))
	mux.HandleFunc("GET "+prefix+"/{configuration}/play/{service}/{source}/{type}/{id}", protect(false, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		link, err := s.options.AIOSource.TracePlayback(r.Context(), historyOrigin, r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), func(ctx context.Context) (string, error) {
			return s.options.AIOSource.PublicPlaybackSource(ctx, r.PathValue("source"), r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), visitor)
		})
		if errors.Is(err, aiosource.ErrPending) {
			w.Header().Set("Retry-After", "10")
			writeError(w, 425, "Playback is being prepared. Try again shortly.")
			return
		}
		if err != nil {
			respondError(w, err)
			return
		}
		if err := s.options.AIOSource.CheckPublicStremio(r.Context(), visitor); err != nil {
			onAuthFail(w, err)
			return
		}
		http.Redirect(w, r, link, http.StatusTemporaryRedirect)
	}))
	mux.HandleFunc("OPTIONS "+prefix+"/{rest...}", stremioOptions)
	mux.HandleFunc(prefix+"/", stremioNotFound)
}

func availablePublicProviders(config model.AIOConfig, includeAll bool) []aiosource.PublicProvider {
	providers := []aiosource.PublicProvider{}
	for _, provider := range aiosource.PublicProviders {
		if includeAll || config.DebridSecret(provider.ID) != "" {
			providers = append(providers, provider)
		}
	}
	return providers
}

func (s *server) hasPublicOrigin(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	return len(origins) == 1 && origins[0] == s.auth.origin
}

func (s *server) populateCatalogPolicyConfig(ctx context.Context, info map[string]any, sources []aiosource.PublicPrivateSource) error {
	policy, err := s.options.Store.PublicCatalogPolicy(ctx)
	if err != nil {
		return err
	}
	info["catalog_fields"] = policy.Fields
	info["catalog_revision"] = policy.Revision
	info["catalog_entities"] = policy.CatalogEntities
	permitted := []aiosource.PublicPrivateSource{}
	for _, source := range sources {
		if len(policy.Sources) == 0 || slices.Contains(policy.Sources, source.ID) {
			permitted = append(permitted, source)
		}
	}
	info["catalog_sources"] = permitted
	return nil
}

func publicCredentialError(w http.ResponseWriter, err error) {
	if errors.Is(err, aiosource.ErrAuth) {
		writeError(w, 401, "Your saved credentials are no longer available. Enter them again.")
	} else if errors.Is(err, model.ErrInvalid) {
		writeError(w, 400, "Check the service keys and complete announce URLs for the permitted private sources.")
	} else {
		respondError(w, err)
	}
}
