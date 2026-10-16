// Package httpapi serves the authenticated administration API and static panel.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/health"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"github.com/theo-bnts/aiosources/internal/scheduling"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/vault"
)

type Options struct {
	AIOSource     *aiosource.Service
	Store         *store.Store
	Providers     *providers.Registry
	Jobs          *jobs.Manager
	Vault         *vault.Vault
	Health        *health.Service
	AdminPassword string
	PublicURL     string
	Frontend      fs.FS
	Version       string
}

type server struct {
	options    Options
	auth       *authentication
	streams    chan struct{}
	streamAuth chan struct{}
	public     *publicAdmission
}

func New(options Options) (http.Handler, error) {
	if options.Store == nil || options.Providers == nil || options.Jobs == nil || options.Vault == nil || options.Frontend == nil {
		return nil, errors.New("API dependencies and compiled frontend are required")
	}
	auth, err := newAuthentication(options.AdminPassword, options.PublicURL, options.Store, options.Vault)
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(options.Frontend, "index.html"); err != nil {
		return nil, errors.New("compiled frontend index.html is missing; build web before starting the server")
	}
	s := &server{options: options, auth: auth, streams: make(chan struct{}, 64), streamAuth: make(chan struct{}, 64), public: &publicAdmission{windows: map[string]loginWindow{}, slots: make(chan struct{}, 8)}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := options.Store.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "Database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/session", auth.sessionHandler)
	mux.HandleFunc("POST /api/login", auth.loginHandler)
	mux.HandleFunc("POST /api/logout", auth.protect(auth.logoutHandler))
	mux.HandleFunc("GET /api/health", auth.protect(s.health))
	mux.HandleFunc("GET /api/providers", auth.protect(s.listProviders))
	mux.HandleFunc("GET /api/source-torrent-stats", auth.protect(s.sourceTorrentStats))
	mux.HandleFunc("GET /api/schedules", auth.protect(s.listSchedules))
	mux.HandleFunc("GET /api/provider-schema", auth.protect(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, providers.JSONSchema())
	}))
	mux.HandleFunc("GET /api/providers/{id}", auth.protect(s.getProvider))
	mux.HandleFunc("PUT /api/providers/{id}", auth.protect(s.saveProvider))
	mux.HandleFunc("DELETE /api/providers/{id}", auth.protect(s.deleteProvider))
	mux.HandleFunc("POST /api/providers/validate", auth.protect(s.validateProvider))
	mux.HandleFunc("GET /api/adapters", auth.protect(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"items": connectors.Adapters()})
	}))
	mux.HandleFunc("GET /api/secrets", auth.protect(s.listSecrets))
	mux.HandleFunc("PUT /api/secrets/{name}", auth.protect(s.putSecret))
	mux.HandleFunc("DELETE /api/secrets/{name}", auth.protect(s.deleteSecret))
	mux.HandleFunc("GET /api/runs", auth.protect(s.listRuns))
	mux.HandleFunc("POST /api/runs", auth.protect(s.startRun))
	mux.HandleFunc("GET /api/runs/{id}", auth.protect(s.getRun))
	mux.HandleFunc("POST /api/runs/{id}/pause", auth.protect(s.pauseRun))
	mux.HandleFunc("POST /api/runs/{id}/cancel", auth.protect(s.cancelRun))
	mux.HandleFunc("POST /api/runs/{id}/resume", auth.protect(s.resumeRun))
	mux.HandleFunc("GET /api/runs/{id}/events", auth.protect(s.runEvents))
	mux.HandleFunc("GET /api/events", auth.protect(s.events))
	mux.HandleFunc("GET /api/raw", auth.protect(s.listRaw))
	mux.HandleFunc("GET /api/raw/{id}", auth.protect(s.rawDetail))
	mux.HandleFunc("GET /api/raw/{id}/download", auth.protect(s.rawDownload))
	mux.HandleFunc("GET /api/pages/{id}/download", auth.protect(s.pageDownload))
	mux.HandleFunc("GET /api/torrents", auth.protect(s.torrents))
	s.registerAIOSource(mux)
	s.registerActivityRoutes(mux)
	s.registerSharingRoutes(mux)
	s.registerRemoteRoutes(mux)
	s.registerSecurityRoutes(mux)
	s.registerSearchRoutes(mux)
	s.registerHistoryRoutes(mux)
	s.registerSystemHealthRoutes(mux)
	s.registerStorageRoutes(mux)
	s.registerCollectionSettingsRoutes(mux)
	mux.HandleFunc("/api/", auth.protect(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "API route not found")
	}))
	mux.Handle("/", staticFrontend(options.Frontend))
	return securityHeaders(recoverRequests(mux)), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not encode the response")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func respondError(w http.ResponseWriter, err error) {
	var provider *aiosource.ProviderError
	if errors.As(err, &provider) {
		if provider.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(provider.RetryAfter))
		}
		writeJSON(w, provider.HTTPStatus(), map[string]string{"error": provider.Error(), "code": provider.Code})
		return
	}
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "Item not found")
	case errors.Is(err, model.ErrBusy):
		writeError(w, http.StatusConflict, "An operation is already queued or running. Wait for it to finish.")
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "The state has changed. Reload before trying again.")
	case errors.Is(err, model.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "Invalid configuration or operation")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, "Operation interrupted or timed out")
	default:
		// Do not expose database connection strings, source URLs or payloads.
		fmt.Fprintf(os.Stderr, "administration operation failed (%T)\n", err)
		writeError(w, http.StatusInternalServerError, "The operation failed. Check the service status.")
	}
}

func requestQuery(w http.ResponseWriter, r *http.Request, repeated ...string) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid query parameters")
		return nil, false
	}
	for key, values := range query {
		allowed := false
		for _, name := range repeated {
			allowed = allowed || key == name
		}
		if len(values) != 1 && !allowed {
			writeError(w, http.StatusBadRequest, "Query parameters must have a single value")
			return nil, false
		}
	}
	return query, true
}

func listOptions(w http.ResponseWriter, r *http.Request, repeated ...string) (model.ListOptions, bool) {
	query, ok := requestQuery(w, r, repeated...)
	if !ok {
		return model.ListOptions{}, false
	}
	options, ok := paginationOptions(w, query, 50, 200)
	if !ok {
		return options, false
	}
	options.ProviderID, options.RunID, options.Status, options.Query = query.Get("provider"), query.Get("run"), query.Get("status"), query.Get("q")
	if len(options.ProviderID) > 128 || len(options.RunID) > 128 || len(options.Query) > 500 || len(options.Status) > 32 {
		writeError(w, http.StatusBadRequest, "Filter is too long")
		return options, false
	}
	return options, true
}

func normalizedList[T any](value model.List[T]) model.List[T] {
	if value.Items == nil {
		value.Items = []T{}
	}
	return value
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.options.Store.Ping(r.Context()); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"database": "ok", "version": s.options.Version})
}

func (s *server) listProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.Providers.List()
	if err != nil {
		respondError(w, err)
		return
	}
	if items == nil {
		items = []model.ProviderSummary{}
	}
	ids := make([]string, len(items))
	for index := range items {
		ids[index] = items[index].ID
	}
	latest, err := s.options.Store.LatestRuns(r.Context(), ids)
	if err != nil {
		respondError(w, err)
		return
	}
	for index := range items {
		if items[index].Issues == nil {
			items[index].Issues = []string{}
		}
		if last, ok := latest[items[index].ID]; ok {
			items[index].LastRun = &last
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.Jobs.Schedules(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) getProvider(w http.ResponseWriter, r *http.Request) {
	document, err := s.options.Providers.Get(r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if document.Issues == nil {
		document.Issues = []string{}
	}
	if document.Provider.IsRemoteCatalog() && document.Provider.CatalogSync == nil {
		sync, _ := scheduling.CatalogSynchronization(document.Provider)
		document.Provider.CatalogSync = &sync
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *server) validateProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		JSON string `json:"json"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result := s.options.Providers.Validate(input.JSON)
	if result.Issues == nil {
		result.Issues = []string{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) saveProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		JSON     string `json:"json"`
		Revision string `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	validation := s.options.Providers.Validate(input.JSON)
	if !validation.Valid {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "Invalid JSON configuration", "details": validation.Issues})
		return
	}
	document, err := s.options.Providers.Save(r.PathValue("id"), input.JSON, input.Revision)
	if err != nil {
		respondError(w, err)
		return
	}
	s.options.Jobs.Notify("provider", document.Provider.ID)
	if document.Issues == nil {
		document.Issues = []string{}
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision string `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	id := r.PathValue("id")
	err := s.options.Providers.Delete(id, input.Revision, func() error {
		active, err := s.options.Store.HasActiveRun(r.Context(), id)
		if err != nil {
			return err
		}
		if active {
			return model.ErrBusy
		}
		return nil
	})
	if err != nil {
		respondError(w, err)
		return
	}
	s.options.Jobs.Notify("provider", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listSecrets(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.Vault.List(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	filtered := []model.SecretInfo{}
	for _, item := range items {
		if item.Name != "aiosource_installation" {
			filtered = append(filtered, item)
		}
	}
	items = filtered
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) putSecret(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("name") == "aiosource_installation" {
		writeError(w, 409, "Use the Stremio page to replace the installation token")
		return
	}
	var input struct {
		Value    string `json:"value"`
		Revision *int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Revision == nil || *input.Revision < 0 {
		writeError(w, http.StatusBadRequest, "A secret revision is required; use zero to create a new reference")
		return
	}
	if len(input.Value) > 64<<10 {
		writeError(w, http.StatusBadRequest, "Secret is too large")
		return
	}
	if err := s.options.Vault.PutAtRevision(r.Context(), r.PathValue("name"), input.Value, *input.Revision); err != nil {
		respondError(w, err)
		return
	}
	s.options.Jobs.Notify("secret", r.PathValue("name"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Revision < 1 {
		writeError(w, http.StatusBadRequest, "The current secret revision is required")
		return
	}
	// Hold the interprocess registry lock until the store has atomically checked
	// durable source/run references and deleted the encrypted value.
	err := s.options.Providers.WithDefinitions(func(definitions map[string]model.ProviderDocument) error {
		for _, document := range definitions {
			references, err := document.Provider.SecretReferences()
			if err != nil {
				return err
			}
			for _, reference := range references {
				if reference == name {
					return model.ErrConflict
				}
			}
		}
		return s.options.Vault.DeleteAtRevision(r.Context(), name, input.Revision)
	})
	if errors.Is(err, model.ErrConflict) {
		writeError(w, http.StatusConflict, "This secret changed or is still referenced by a source, active run or AIOSources configuration. Refresh before trying again.")
		return
	}
	if err != nil {
		respondError(w, err)
		return
	}
	s.options.Jobs.Notify("secret", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) startRun(w http.ResponseWriter, r *http.Request) {
	var request model.StartRun
	if !decodeJSON(w, r, &request) {
		return
	}
	run, err := s.options.Jobs.Enqueue(r.Context(), request)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *server) listRuns(w http.ResponseWriter, r *http.Request) {
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.ListRuns(r.Context(), options)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, normalizedList(result))
}

func (s *server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.options.Store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *server) pauseRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.options.Jobs.Pause(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *server) cancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.options.Jobs.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *server) resumeRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.options.Jobs.Resume(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *server) runEvents(w http.ResponseWriter, r *http.Request) {
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.Events(r.Context(), r.PathValue("id"), options)
	if err != nil {
		respondError(w, err)
		return
	}
	for i := range result.Items {
		connectors.SanitizeFailureDiagnostics(result.Items[i].Data)
	}
	writeJSON(w, http.StatusOK, normalizedList(result))
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		return
	}
	select {
	case s.streams <- struct{}{}:
		defer func() { <-s.streams }()
	default:
		writeError(w, http.StatusServiceUnavailable, "Too many live update streams")
		return
	}
	controller := http.NewResponseController(w)
	notices, unsubscribe := s.options.Jobs.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return
	}
	if _, err := io.WriteString(w, "retry: 3000\n: connected\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		var message []byte
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, ok := s.auth.current(r); !ok {
				return
			}
			message = []byte(": heartbeat\n\n")
		case notice, ok := <-notices:
			if !ok {
				return
			}
			if _, ok := s.auth.current(r); !ok {
				return
			}
			data, err := json.Marshal(notice)
			if err != nil {
				return
			}
			message = append(append([]byte("event: update\ndata: "), data...), '\n', '\n')
		}
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := w.Write(message); err != nil {
			return
		}
		if err := controller.Flush(); err != nil {
			return
		}
	}
}

func (s *server) listRaw(w http.ResponseWriter, r *http.Request) {
	options, ok := listOptions(w, r)
	if !ok {
		return
	}
	if options.RunID == "" {
		writeError(w, http.StatusBadRequest, "Select an import run to view its processed records")
		return
	}
	if options.Status != "" && options.Status != "accepted" && options.Status != "error" && options.Status != "ignored" && options.Status != "enrichment" {
		writeError(w, http.StatusBadRequest, "Unknown processing result")
		return
	}
	result, err := s.options.Store.ListRaw(r.Context(), options)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func positiveID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "Invalid identifier")
		return 0, false
	}
	return id, true
}

func (s *server) rawDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(w, r)
	if !ok {
		return
	}
	record, err := s.options.Store.Raw(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if !record.PayloadRetained {
		writeJSON(w, http.StatusOK, map[string]any{"record": record, "raw": nil, "byte_length": nil, "valid_utf8": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"record": record, "raw": string(record.Raw), "byte_length": len(record.Raw), "valid_utf8": utf8.Valid(record.Raw)})
}

func download(w http.ResponseWriter, r *http.Request, data []byte, filename string) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}

func (s *server) rawDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(w, r)
	if !ok {
		return
	}
	record, err := s.options.Store.Raw(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if !record.PayloadRetained {
		writeError(w, http.StatusNotFound, "Original record payload was not stored")
		return
	}
	download(w, r, record.Raw, fmt.Sprintf("record-%d.bin", id))
}

func (s *server) pageDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(w, r)
	if !ok {
		return
	}
	data, _, err := s.options.Store.RawPage(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	download(w, r, data, fmt.Sprintf("response-%d.bin", id))
}

func (s *server) torrents(w http.ResponseWriter, r *http.Request) {
	options, ok := torrentListOptions(w, r)
	if !ok {
		return
	}
	result, err := s.options.Store.ListTorrents(r.Context(), options)
	if err != nil {
		respondError(w, err)
		return
	}
	if err := s.projectTorrentFields(result.Items); err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, normalizedList(result))
}

func staticFrontend(frontend fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			http.NotFound(w, r)
			return
		}
		file, err := frontend.Open(name)
		if err != nil {
			// Only client navigation routes get the SPA entrypoint, never missing assets.
			if path.Ext(name) != "" || strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			file, err = frontend.Open(name)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		if seeker, ok := file.(io.ReadSeeker); ok {
			http.ServeContent(w, r, name, info.ModTime(), seeker)
			return
		}
		// An arbitrary fs.FS need not provide Seek; production embed.FS does.
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Asset read failed", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
	})
}

func recoverRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				fmt.Fprintf(os.Stderr, "administration handler panic (%T)\n", value)
				writeError(w, http.StatusInternalServerError, "Internal service error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
