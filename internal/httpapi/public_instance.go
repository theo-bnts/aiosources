package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
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
			StremioEnabled *bool `json:"stremio_enabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		value := input.PublicAccess
		if input.StremioEnabled != nil {
			value.StremioEnabled = *input.StremioEnabled
		} else {
			// A settings form opened before the upgrade must not revoke Stremio
			// merely because it does not yet contain the new control.
			current, err := s.options.Store.PublicAccess(r.Context())
			if err != nil {
				respondError(w, err)
				return
			}
			value.StremioEnabled = current.StremioEnabled
		}
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
		info := map[string]any{"enabled": enabled, "providers": aiosource.PublicProviders, "stremio_enabled": enabled && value.StremioEnabled, "torznab_enabled": enabled && value.TorznabEnabled, "search_enabled": enabled && value.SearchEnabled}
		if enabled && value.ExposeSources {
			allSources, e := s.options.AIOSource.PublicSources(ctx)
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
			if len(sources) > 0 {
				info["private_sources"] = sources
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
			if r.Header.Get("Origin") != s.auth.origin {
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
			if err == nil && len(configuration) > aiosource.MaxPublicInstallationSize {
				writeError(w, 400, "The installation link is too large. Shorten optional announce URLs.")
				return
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
	protect := func(budget time.Duration, next func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc {
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
			effective := budget
			if budget > 5*time.Second {
				configured, err := s.options.Store.ConnectionTimeout(r.Context(), "stremio")
				if err != nil {
					respondError(w, err)
					return
				}
				effective = configured
			}
			ctx, cancel := context.WithTimeout(r.Context(), effective)
			defer cancel()
			r = r.WithContext(ctx)
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
	mux.HandleFunc("GET /public/stremio/{configuration}/manifest.json", protect(5*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		s.options.AIOSource.RecordAddonRequest("public", "manifest", "", "", time.Now(), nil)
		writeJSON(w, 200, aiosource.InstallationManifest(visitor.Preferences))
	}))
	mux.HandleFunc("GET /public/stremio/{configuration}/configure", protect(5*time.Second, func(w http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) {
		http.Redirect(w, r, "/public", http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /public/stremio/{configuration}/stream/{type}/{id}", protect(20*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		id := r.PathValue("id")
		if !strings.HasSuffix(id, ".json") || len(id) > 160 {
			s.options.AIOSource.RecordAddonRequest("public", "stream", r.PathValue("type"), "", time.Now(), model.ErrInvalid)
			writeError(w, 400, "Invalid stream resource")
			return
		}
		streams, err := s.options.AIOSource.PublicStreams(r.Context(), s.auth.origin, r.PathValue("configuration"), r.PathValue("type"), strings.TrimSuffix(id, ".json"), visitor)
		if err != nil {
			respondError(w, err)
			return
		}
		if err := s.options.AIOSource.CheckPublicStremio(r.Context(), visitor); err != nil {
			writeError(w, 401, "Invalid public installation")
			return
		}
		writeJSON(w, 200, map[string]any{"streams": streams})
	}))
	mux.HandleFunc("GET /public/stremio/{configuration}/play/{service}/{source}/{type}/{id}", protect(65*time.Second, func(w http.ResponseWriter, r *http.Request, visitor *aiosource.PublicInstallation) {
		link, err := s.options.AIOSource.TracePlayback(r.Context(), "public", r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), func(ctx context.Context) (string, error) {
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
			writeError(w, 401, "Invalid public installation")
			return
		}
		http.Redirect(w, r, link, http.StatusTemporaryRedirect)
	}))
	mux.HandleFunc("OPTIONS /public/stremio/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(204)
	})
	mux.HandleFunc("/public/stremio/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "Addon route not found") })
}

func publicCredentialError(w http.ResponseWriter, err error) {
	if errors.Is(err, aiosource.ErrAuth) {
		writeError(w, 401, "Your saved credentials are no longer available. Enter them again.")
	} else {
		respondError(w, err)
	}
}
