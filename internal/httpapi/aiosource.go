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
	mux.HandleFunc("GET /api/catalog/torrents", s.auth.protect(s.catalogTorrents))
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
	mux.HandleFunc("GET /api/aiosource/correspondences", s.auth.protect(s.aioCorrespondences))
	mux.HandleFunc("GET /api/aiosource/correspondences/{id}", s.auth.protect(s.aioCorrespondenceDetail))
	mux.HandleFunc("DELETE /api/aiosource/correspondences/{id}/members/{occurrence}", s.auth.protect(s.aioCorrespondenceUnlink))
	mux.HandleFunc("POST /api/aiosource/correspondences/{id}/exclusions/{occurrence}/restore", s.auth.protect(s.aioCorrespondenceRestore))
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
	mux.HandleFunc("OPTIONS /stremio/{rest...}", stremioOptions)
	mux.HandleFunc("/stremio/", stremioNotFound)
}

func stremioOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(204)
}

func stremioNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, 404, "Addon route not found")
}

func (s *server) aioMatches(w http.ResponseWriter, r *http.Request) {
	opts, ok := listOptions(w, r)
	if !ok {
		return
	}
	if !model.ValidSelection(opts.Status, func(value string) bool {
		return value == "matched" || value == "ambiguous" || value == "unmatched" || value == "pending"
	}) {
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
	games, err := s.options.Store.GameStatus(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	games.Running = s.options.AIOSource.GameState()
	if c.IGDBEnabled && !games.Running {
		next := time.Now()
		if games.LastSync != nil {
			next = games.LastSync.Add(time.Duration(c.IGDBMinutes) * time.Minute)
		}
		if games.StartedAt != nil {
			next = time.Now()
		}
		if games.RetryAt != nil && games.RetryAt.After(next) {
			next = *games.RetryAt
		}
		games.NextUpdate = &next
	}
	softwareStatus, err := s.options.Store.SoftwareStatus(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	softwareStatus.Running = s.options.AIOSource.SoftwareState()
	softwareStatus.NVDKeyConfigured = c.SoftwareNVDSecret != ""
	if c.SoftwareEnabled && !softwareStatus.Running {
		next, err := s.options.Store.NextSoftwareUpdate(r.Context(), c.SoftwareMinutes)
		if err != nil {
			respondError(w, err)
			return
		}
		softwareStatus.NextUpdate = &next
	}
	musicStatus, err := s.options.Store.MusicStatus(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	musicStatus.Running = s.options.AIOSource.MusicState()
	musicStatus.LiveEnabled = c.MusicBrainzSecret != ""
	if c.MusicBrainzEnabled && !musicStatus.Running {
		next, err := s.options.Store.NextMusicUpdate(r.Context(), c.MusicBrainzMinutes, musicStatus.LiveEnabled)
		if err != nil {
			respondError(w, err)
			return
		}
		musicStatus.NextUpdate = &next
	}
	writeJSON(w, 200, map[string]any{"software": softwareStatus, "music": musicStatus, "games": games, "books": books, "config": c, "status": projection, "automation": automationProjection, "syncing": syncing, "matching": matching, "maintenance": s.options.AIOSource.ResetState(), "error": message, "matching_error": matchingError, "recovery_required": recovery})
}
func (s *server) aioConfig(w http.ResponseWriter, r *http.Request) {
	defaults := store.DefaultAIOConfig()
	c := model.AIOConfig{
		AutoCache:                defaults.AutoCache,
		IGDBMinutes:              defaults.IGDBMinutes,
		IGDBRequestsPerSecond:    defaults.IGDBRequestsPerSecond,
		IGDBReconcileDays:        defaults.IGDBReconcileDays,
		SoftwareMinutes:          defaults.SoftwareMinutes,
		SoftwareIncludeNVD:       defaults.SoftwareIncludeNVD,
		SoftwareExcludePlugins:   defaults.SoftwareExcludePlugins,
		SoftwareIncludeOS:        defaults.SoftwareIncludeOS,
		SoftwareIncludeIcons:     defaults.SoftwareIncludeIcons,
		SoftwarePlatforms:        defaults.SoftwarePlatforms,
		OpenLibraryMinutes:       defaults.OpenLibraryMinutes,
		OpenLibraryRelevantOnly:  defaults.OpenLibraryRelevantOnly,
		OpenLibraryLanguages:     defaults.OpenLibraryLanguages,
		MusicBrainzMinutes:       defaults.MusicBrainzMinutes,
		MusicBrainzOfficialOnly:  defaults.MusicBrainzOfficialOnly,
		MusicBrainzExcludeSpoken: defaults.MusicBrainzExcludeSpoken,
		MusicBrainzIncludeCovers: defaults.MusicBrainzIncludeCovers,
		MusicBrainzTypes:         defaults.MusicBrainzTypes,
		MusicBrainzLanguages:     defaults.MusicBrainzLanguages,
		TMDBRequestsPerSecond:    defaults.TMDBRequestsPerSecond,
		MatchSeconds:             defaults.MatchSeconds,
	}
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
	case "sync-software":
		err = s.options.AIOSource.StartSoftware(r.Context(), "manual")
	case "stop-software":
		s.options.AIOSource.StopSoftware()
	case "reset-software":
		err = s.options.AIOSource.StartReset("software")
	case "sync-music":
		err = s.options.AIOSource.StartMusic(r.Context(), "manual")
	case "stop-music":
		s.options.AIOSource.StopMusic()
	case "reset-music":
		err = s.options.AIOSource.StartReset("music")
	case "sync-games":
		err = s.options.AIOSource.StartGames(r.Context(), "manual")
	case "stop-games":
		s.options.AIOSource.StopGames()
	case "reset-games":
		err = s.options.AIOSource.StartReset("games")
	case "sync-books":
		err = s.options.AIOSource.StartBooks("manual")
	case "stop-books":
		s.options.AIOSource.StopBooks()
	case "sync":
		err = s.options.AIOSource.StartSync(r.Context())
	case "match":
		err = s.options.AIOSource.StartMatch()
	case "recompute-correspondences":
		err = s.options.AIOSource.StartCorrespondenceRecompute(r.Context())
	case "stop":
		s.options.AIOSource.Stop(true, true)
	case "stop-sync":
		s.options.AIOSource.Stop(true, false)
	case "stop-matching":
		s.options.AIOSource.Stop(false, true)
	case "reset-books":
		err = s.options.AIOSource.StartReset("books")
	case "reset-videos":
		err = s.options.AIOSource.StartReset("videos")
	case "reset-metadata":
		err = s.options.AIOSource.StartReset("metadata")
	case "reset-mapping":
		err = s.options.AIOSource.StartReset("mapping")
	case "reset-cache":
		err = s.options.AIOSource.StartReset("cache")
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
func parseStremioStreamResourceID(r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !strings.HasSuffix(id, ".json") || len(id) > 160 {
		return "", false
	}
	return strings.TrimSuffix(id, ".json"), true
}

func (s *server) aioStreams(w http.ResponseWriter, r *http.Request) {
	resourceID, ok := parseStremioStreamResourceID(r)
	if !ok {
		s.options.AIOSource.RecordRejectedStream(r.PathValue("type"), "", false)
		writeError(w, 400, "Invalid stream resource")
		return
	}
	streams, err := s.options.AIOSource.Streams(r.Context(), s.aioOrigin(r), r.PathValue("token"), r.PathValue("type"), resourceID)
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
