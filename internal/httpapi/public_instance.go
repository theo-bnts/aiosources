package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Admission data is bounded, volatile and never appears in history or exports.
type publicAdmission struct {
	mu      sync.Mutex
	windows map[string]loginWindow
	slots   chan struct{}
}

func (a *publicAdmission) acquire(w http.ResponseWriter) bool {
	select {
	case a.slots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "Too many public requests. Try again shortly.")
		return false
	}
}

func (a *publicAdmission) admit(w http.ResponseWriter, r *http.Request, setup bool) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = "unknown"
	}
	a.mu.Lock()
	now := time.Now()
	for k, v := range a.windows {
		if !now.Before(v.until) {
			delete(a.windows, k)
		}
	}
	key := host
	if setup {
		key += "/setup"
	}
	v := a.windows[key]
	limit := 60
	if setup {
		limit = 6
	}
	if len(a.windows) >= 1024 && v.until.IsZero() || v.attempts >= limit {
		a.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "Too many public requests. Try again shortly.")
		return false
	}
	if v.until.IsZero() {
		v.until = now.Add(time.Minute)
	}
	v.attempts++
	a.windows[key] = v
	a.mu.Unlock()
	return true
}
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
	mux.HandleFunc("GET /api/stremio/public-access", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		value, err := s.options.Store.PublicAccess(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/stremio/public-access", s.auth.protect(func(w http.ResponseWriter, r *http.Request) {
		var value model.PublicAccess
		if !decodeJSON(w, r, &value) {
			return
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
		if !s.public.acquire(w) {
			return
		}
		defer func() { <-s.public.slots }()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		value, err := s.options.Store.PublicAccess(ctx)
		if err != nil {
			respondError(w, err)
			return
		}
		enabled := value.Enabled && s.publicOriginAllowed()
		info := map[string]any{"enabled": enabled, "providers": aiosource.PublicProviders}
		if enabled && value.AllowPrivateDownloads {
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
	mux.HandleFunc("POST /api/public/install", func(w http.ResponseWriter, r *http.Request) {
		if !s.publicOriginAllowed() {
			writeError(w, 404, "Public access is disabled")
			return
		}
		if r.Header.Get("Origin") != s.auth.origin {
			writeError(w, 403, "Request origin is not allowed")
			return
		}
		if !s.public.admit(w, r, true) {
			return
		}
		if !s.public.acquire(w) {
			return
		}
		defer func() { <-s.public.slots }()
		var input struct {
			Services  map[string]string `json:"services"`
			Announces map[string]string `json:"announces"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, aiosource.MaxPublicInstallationSize)
		if !decodeJSON(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		hasAnnounces := len(input.Announces) > 0
		configuration, err := s.options.AIOSource.CreatePublicInstallation(ctx, s.auth.origin, input.Services, input.Announces)
		clear(input.Services)
		clear(input.Announces)
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
		writeJSON(w, 200, map[string]string{"manifest_url": s.auth.origin + "/public/stremio/" + configuration + "/manifest.json"})
	})
	protect := func(budget time.Duration, next func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), budget)
			defer cancel()
			r = r.WithContext(ctx)
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if !s.publicOriginAllowed() {
				writeError(w, 401, "Invalid public installation")
				return
			}
			if !s.public.admit(w, r, false) {
				return
			}
			if !s.public.acquire(w) {
				return
			}
			defer func() { <-s.public.slots }()
			visitor, err := s.options.AIOSource.OpenPublicInstallation(r.Context(), s.auth.origin, r.PathValue("configuration"))
			if err != nil {
				writeError(w, 401, "Invalid public installation")
				return
			}
			defer func() { clear(visitor.Services); clear(visitor.Announces) }()
			next(w, r, visitor)
		}
	}
	mux.HandleFunc("GET /public/stremio/{configuration}/manifest.json", protect(5*time.Second, func(w http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) {
		s.options.AIOSource.RecordAddonRequest("public", "manifest", "", "", time.Now(), nil)
		writeJSON(w, 200, aiosource.Manifest())
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
