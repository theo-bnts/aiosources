package httpapi

import (
	"context"
	"errors"
	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *server) registerAIOSource(mux *http.ServeMux) {
	if s.options.AIOSource == nil {
		return
	}
	s.registerPublicInstance(mux)
	mux.HandleFunc("GET /api/catalog-downloads/{token}", s.auth.protect(s.catalogFile))
	mux.HandleFunc("POST /api/operations/{id}/download", s.auth.protect(s.operationDownload))
	mux.HandleFunc("GET /api/catalog/search", s.auth.protect(s.catalogSearch))
	mux.HandleFunc("GET /api/catalog/{id}", s.auth.protect(s.catalogTitle))
	mux.HandleFunc("GET /api/catalog/{id}/releases", s.auth.protect(s.catalogReleases))
	mux.HandleFunc("POST /api/catalog/{id}/download", s.auth.protect(s.catalogDownload))
	mux.HandleFunc("GET /api/aiosource", s.auth.protect(s.aioStatus))
	mux.HandleFunc("GET /api/aiosource/controls", s.auth.protect(s.aioStatus))
	mux.HandleFunc("GET /api/aiosource/information", s.auth.protect(s.aioStatus))
	mux.HandleFunc("GET /api/debrid/statistics", s.auth.protect(s.cacheStatistics))
	mux.HandleFunc("GET /api/operations", s.auth.protect(s.operationHistory))
	mux.HandleFunc("GET /api/operations/{id}", s.auth.protect(s.operationDetail))
	mux.HandleFunc("GET /api/operations/{id}/events", s.auth.protect(s.operationEvents))
	mux.HandleFunc("GET /api/aiosource/matches", s.auth.protect(s.aioMatches))
	mux.HandleFunc("GET /api/aiosource/matches/{id}", s.auth.protect(s.aioReviewDetail))
	mux.HandleFunc("PUT /api/aiosource/matches/{id}", s.auth.protect(s.aioReviewSave))
	mux.HandleFunc("GET /api/aiosource/candidates", s.auth.protect(s.aioReviewCandidates))
	mux.HandleFunc("GET /api/stremio/statistics", s.auth.protect(s.stremioStatistics))
	mux.HandleFunc("GET /api/stremio/requests", s.auth.protect(s.stremioRequests))
	mux.HandleFunc("GET /api/stremio/requests/{id}", s.auth.protect(s.stremioRequest))
	mux.HandleFunc("PUT /api/aiosource/config", s.auth.protect(s.aioConfig))
	mux.HandleFunc("POST /api/aiosource/{action}", s.auth.protect(s.aioAction))
	mux.HandleFunc("GET /stremio/{token}/manifest.json", s.aioProtect(func(w http.ResponseWriter, r *http.Request) {
		manifest, err := s.options.AIOSource.PrivateManifest(r.Context(), r.PathValue("token"))
		if err != nil {
			respondError(w, err)
			return
		}
		if !validatePrivateConnection(w, r) {
			return
		}
		s.options.AIOSource.RecordAddonRequest("private", "manifest", "", "", time.Now(), nil)
		writeJSON(w, 200, manifest)
	}))
	mux.HandleFunc("GET /stremio/{token}/configure", s.aioProtect(func(w http.ResponseWriter, r *http.Request) {
		if validatePrivateConnection(w, r) {
			http.Redirect(w, r, "/addon/settings", http.StatusSeeOther)
		}
	}))
	mux.HandleFunc("GET /stremio/{token}/stream/{type}/{id}", s.aioProtect(s.aioStreams))
	mux.HandleFunc("GET /stremio/{token}/play/{service}/{provider}/{source}/{type}/{id}", s.aioProtect(s.aioPlayback))
	mux.HandleFunc("OPTIONS /stremio/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(204)
	})
	mux.HandleFunc("/stremio/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "Addon route not found") })
}

func (s *server) aioMatches(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	if opts.Status != "" && opts.Status != "matched" && opts.Status != "ambiguous" && opts.Status != "unmatched" && opts.Status != "pending" {
		writeError(w, http.StatusBadRequest, "Unknown identification result")
		return
	}
	result, err := s.options.Store.ListAIOMatchDiagnostics(r.Context(), opts)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *server) aioProtect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if !s.authorizeAddon(w, r) {
			return
		}
		select {
		case s.streams <- struct{}{}:
			defer func() { <-s.streams }()
		case <-r.Context().Done():
			return
		default:
			if r.Pattern == "GET /stremio/{token}/stream/{type}/{id}" {
				s.options.AIOSource.RecordRejectedStream(r.PathValue("type"), strings.TrimSuffix(r.PathValue("id"), ".json"), true)
			}
			writeError(w, 429, "Too many active requests")
			return
		}
		s.privateConnectionRequest(w, r, next)
	}
}

func (s *server) authorizeAddon(w http.ResponseWriter, r *http.Request) bool {
	// Authentication precedes the playback limit, so it needs its own bound.
	// Invalid tokens cannot create an unbounded database-waiting queue.
	select {
	case s.streamAuth <- struct{}{}:
		defer func() { <-s.streamAuth }()
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "Too many active requests")
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.options.AIOSource.ValidateToken(ctx, r.PathValue("token")); err != nil {
		if errors.Is(err, aiosource.ErrAuth) {
			writeError(w, http.StatusUnauthorized, "Invalid installation token")
		} else {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "Authentication storage is unavailable")
		}
		return false
	}
	return true
}
func (s *server) aioStatus(w http.ResponseWriter, r *http.Request) {
	c, err := s.options.Store.AIOConfig(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	controls := r.URL.Path == "/api/aiosource/controls"
	information := r.URL.Path == "/api/aiosource/information"
	var state model.AIOStatus
	if controls {
		state, err = s.options.Store.AIOControlStatus(r.Context())
	} else if information {
		state, err = s.options.Store.AIOInformationStatus(r.Context())
	} else {
		state, err = s.options.Store.AIOStatus(r.Context())
	}
	if err != nil {
		respondError(w, err)
		return
	}
	syncing, matching, message := s.options.AIOSource.State()
	matchingError := message
	if state.SyncError != "" {
		message = strings.TrimSpace(state.SyncError + " " + message)
	}
	recovery := aiosource.MetadataRecoveryRequired(state, time.Now())
	automation := aiosource.AutomationPlan(c, state, syncing, matching, time.Now())
	var projection any = state
	var automationProjection any = automation
	if controls {
		projection = map[string]any{"has_information": state.Works > 0, "cleanup_pending": state.CleanupPending, "cleanup_failed": state.CleanupFailed, "last_sync": state.LastSync, "last_match": state.LastMatch, "sync_error": state.SyncError}
	} else if information {
		projection = map[string]any{"video_works": state.VideoWorks, "pending": state.Pending, "last_sync": state.LastSync, "sync_error": state.SyncError, "inventory_date": state.InventoryDate, "applied_through": state.AppliedThrough, "recovery_started_at": state.RecoveryStartedAt}
		// No substitute identification counters or automation flags: these have
		// deliberately not been calculated by the information-only read.
		automationProjection = map[string]any{"updates": automation.Updates, "next_update": automation.NextUpdate}
	}
	books, err := s.options.Store.OpenLibraryStatus(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	books.Running = s.options.AIOSource.BookState()
	if c.OpenLibraryEnabled && !books.Running {
		next, err := s.options.Store.NextOpenLibraryUpdate(r.Context(), c.OpenLibraryMinutes)
		if err != nil {
			respondError(w, err)
			return
		}
		books.NextUpdate = &next
	}
	writeJSON(w, 200, map[string]any{"books": books, "config": c, "status": projection, "automation": automationProjection, "syncing": syncing, "matching": matching, "error": message, "matching_error": matchingError, "recovery_required": recovery})
}
func (s *server) aioConfig(w http.ResponseWriter, r *http.Request) {
	defaults := store.DefaultAIOConfig()
	c := model.AIOConfig{OpenLibraryMinutes: defaults.OpenLibraryMinutes, TMDBRequestsPerSecond: defaults.TMDBRequestsPerSecond, MatchSeconds: defaults.MatchSeconds}
	if !decodeJSON(w, r, &c) {
		return
	}
	if err := s.options.AIOSource.SaveConfig(r.Context(), c); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *server) aioAction(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.PathValue("action") {
	case "sync-books":
		err = s.options.AIOSource.StartBooks("manual")
	case "stop-books":
		s.options.AIOSource.StopBooks()
	case "sync":
		err = s.options.AIOSource.StartSync(r.Context())
	case "match":
		err = s.options.AIOSource.StartMatch()
	case "stop":
		s.options.AIOSource.Stop(true, true)
	case "stop-sync":
		s.options.AIOSource.Stop(true, false)
	case "stop-matching":
		s.options.AIOSource.Stop(false, true)
	case "reset-books":
		err = s.options.AIOSource.Reset(r.Context(), "books")
	case "reset-videos":
		err = s.options.AIOSource.Reset(r.Context(), "videos")
	case "reset-metadata":
		err = s.options.AIOSource.Reset(r.Context(), "metadata")
	case "reset-mapping":
		err = s.options.AIOSource.Reset(r.Context(), "mapping")
	case "reset-cache":
		err = s.options.AIOSource.Reset(r.Context(), "cache")
	case "retry-cleanup":
		c, e := s.options.Store.AIOConfig(r.Context())
		if e != nil {
			err = e
			break
		}
		if c.AllDebridSecret == "" {
			err = model.ErrInvalid
			break
		}
		key, e := s.options.Vault.Resolve(r.Context(), c.AllDebridSecret)
		if e != nil {
			err = e
			break
		}
		err = s.options.AIOSource.RecoverCleanup(r.Context(), key, true)
	default:
		writeError(w, 404, "AIOSources action not found")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *server) aioOrigin(r *http.Request) string {
	if s.options.PublicURL != "" {
		return s.options.PublicURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	u := url.URL{Scheme: scheme, Host: r.Host}
	return u.String()
}
func (s *server) aioStreams(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !strings.HasSuffix(id, ".json") || len(id) > 160 {
		s.options.AIOSource.RecordRejectedStream(r.PathValue("type"), "", false)
		writeError(w, 400, "Invalid stream resource")
		return
	}
	budget, err := s.options.Store.ConnectionTimeout(r.Context(), "stremio")
	if err != nil {
		respondError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), max(time.Second, budget-5*time.Second))
	defer cancel()
	streams, err := s.options.AIOSource.Streams(ctx, s.aioOrigin(r), r.PathValue("token"), r.PathValue("type"), strings.TrimSuffix(id, ".json"))
	if err != nil {
		respondError(w, err)
		return
	}
	if !validatePrivateConnection(w, r) {
		return
	}
	writeJSON(w, 200, map[string]any{"streams": streams})
}
func (s *server) aioPlayback(w http.ResponseWriter, r *http.Request) {
	preferences, err := s.options.AIOSource.PrivatePreferences(r.Context(), r.PathValue("token"))
	if err != nil {
		respondError(w, err)
		return
	}
	link, err := s.options.AIOSource.TracePlayback(r.Context(), "private", r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), func(ctx context.Context) (string, error) {
		return s.options.AIOSource.PlaybackWithPreferences(ctx, r.PathValue("service"), r.PathValue("provider"), r.PathValue("source"), r.PathValue("type"), r.PathValue("id"), preferences)
	})
	if errors.Is(err, aiosource.ErrPending) {
		w.Header().Set("Retry-After", "10")
		writeError(w, 425, "Torrent is not ready yet. Try again shortly.")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	if !validatePrivateConnection(w, r) {
		return
	}
	http.Redirect(w, r, link, http.StatusTemporaryRedirect)
}
