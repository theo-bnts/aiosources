package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerSharingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/shares", s.auth.protect(s.listShares))
	mux.HandleFunc("POST /api/shares", s.auth.protect(s.createShare))
	mux.HandleFunc("PUT /api/shares/{id}", s.auth.protect(s.updateShare))
	mux.HandleFunc("POST /api/shares/{id}/rotate", s.auth.protect(s.rotateShare))
	mux.HandleFunc("DELETE /api/shares/{id}", s.auth.protect(s.deleteShare))
	// Own every method at this exact path. Never route a sharing credential to
	// administrator authentication or permit write methods on the public feed.
	mux.HandleFunc("/api/catalogs/{id}", s.catalogFeed)
	mux.HandleFunc("GET /api/torznab/endpoints", s.auth.protect(s.listShares))
	mux.HandleFunc("POST /api/torznab/endpoints", s.auth.protect(s.createShare))
	mux.HandleFunc("PUT /api/torznab/endpoints/{id}", s.auth.protect(s.updateShare))
	mux.HandleFunc("POST /api/torznab/endpoints/{id}/rotate", s.auth.protect(s.rotateShare))
	mux.HandleFunc("DELETE /api/torznab/endpoints/{id}", s.auth.protect(s.deleteShare))
	mux.HandleFunc("GET /api/stremio/connections", s.auth.protect(s.listShares))
	mux.HandleFunc("POST /api/stremio/connections", s.auth.protect(s.createShare))
	mux.HandleFunc("PUT /api/stremio/connections/{id}", s.auth.protect(s.updateShare))
	mux.HandleFunc("POST /api/stremio/connections/{id}/rotate", s.auth.protect(s.rotateShare))
	mux.HandleFunc("DELETE /api/stremio/connections/{id}", s.auth.protect(s.deleteShare))
	mux.HandleFunc("/torznab/{id}/api", s.torznabFeed)
}

func shareRequestFormat(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/stremio/connections") {
		return "stremio"
	}
	if strings.HasPrefix(r.URL.Path, "/api/torznab/") {
		return "torznab"
	}
	return "catalog"
}

func (s *server) shareRequestAllowed(w http.ResponseWriter, r *http.Request) bool {
	item, err := s.options.Store.Share(r.Context(), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return false
	}
	if item.Format != shareRequestFormat(r) {
		respondError(w, model.ErrNotFound)
		return false
	}
	return true
}

func (s *server) sharingPublicURL() string {
	origin := s.connectionOrigin()
	if !strings.HasPrefix(origin, "https://") {
		return ""
	}
	return origin
}

// Private app connections follow the explicitly configured origin, including
// HTTP on a trusted internal network. Never infer it from forwarded headers.
func (s *server) connectionOrigin() string {
	origin, err := url.Parse(s.options.PublicURL)
	if err != nil || (origin.Scheme != "https" && origin.Scheme != "http") || origin.Host == "" || origin.Hostname() == "" || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return ""
	}
	return origin.Scheme + "://" + origin.Host
}

func (s *server) shareURL(share *model.Share) {
	if share.Format == "stremio" {
		share.URL = strings.TrimSuffix(s.options.PublicURL, "/") + "/stremio/s1." + url.PathEscape(share.ID)
		return
	}
	if share.Format == "torznab" {
		if origin := s.connectionOrigin(); origin != "" {
			share.URL = origin + "/torznab/" + url.PathEscape(share.ID) + "/api"
		}
		return
	}
	if origin := s.sharingPublicURL(); origin != "" {
		share.URL = origin + "/api/catalogs/" + url.PathEscape(share.ID)
	}
}

type sharingSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *server) listShares(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.Store.SharesByFormat(r.Context(), shareRequestFormat(r))
	if err != nil {
		respondError(w, err)
		return
	}
	providers, err := s.options.Providers.List()
	if err != nil {
		respondError(w, err)
		return
	}
	sources := []sharingSource{}
	for _, provider := range providers {
		if provider.Valid {
			sources = append(sources, sharingSource{ID: provider.ID, Name: provider.Name})
		}
	}
	for index := range items {
		s.shareURL(&items[index])
	}
	publicURL := s.sharingPublicURL()
	if shareRequestFormat(r) == "stremio" {
		publicURL = s.options.PublicURL
	} else if shareRequestFormat(r) == "torznab" {
		publicURL = s.connectionOrigin()
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "fields": store.CatalogFields(), "sources": sources, "public_url": publicURL})
}

func (s *server) validateShare(w http.ResponseWriter, r *http.Request, input model.ShareInput) bool {
	if err := store.ValidateShare(input); err != nil {
		message := "Provide a name, safe fields, source permissions, requests per minute between 1 and 3600, and concurrent downloads between 1 and 16"
		if input.Format == "stremio" {
			message = "Provide a name, source permissions, valid playback filters and limits within their allowed ranges (zero means no limit)"
		} else if input.Format == "torznab" {
			message = "Provide a name, source permissions, valid media and cache filters, and limits within their allowed ranges (zero means no limit)"
		}
		writeError(w, http.StatusUnprocessableEntity, message)
		return false
	}
	if input.Format == "catalog" && input.Enabled && s.sharingPublicURL() == "" {
		writeError(w, http.StatusUnprocessableEntity, "Configure PUBLIC_URL with an HTTPS public origin before enabling sharing")
		return false
	}
	if input.Format == "torznab" && input.Enabled && s.connectionOrigin() == "" {
		writeError(w, http.StatusUnprocessableEntity, "Configure PUBLIC_URL with the HTTP or HTTPS origin used by your clients")
		return false
	}
	if input.Scope == "all" {
		return true
	}
	providers, err := s.options.Providers.List()
	if err != nil {
		respondError(w, err)
		return false
	}
	valid := make(map[string]bool, len(providers))
	for _, provider := range providers {
		valid[provider.ID] = provider.Valid
	}
	for _, id := range input.SourceIDs {
		if !valid[id] {
			// A removed/broken definition must never prevent the owner from
			// disabling its unchanged grant. New or enabled selections still
			// require current valid definitions.
			if !input.Enabled && r.PathValue("id") != "" {
				current, err := s.options.Store.Share(r.Context(), r.PathValue("id"))
				if err != nil {
					respondError(w, err)
					return false
				}
				if current.Scope == input.Scope && slices.Equal(current.SourceIDs, input.SourceIDs) {
					return true
				}
			}
			writeError(w, http.StatusUnprocessableEntity, "Every selected source must be an existing valid provider")
			return false
		}
	}
	return true
}

func (s *server) createShare(w http.ResponseWriter, r *http.Request) {
	var input model.ShareInput
	if !s.decodeShare(w, r, &input) || !s.validateShare(w, r, input) {
		return
	}
	result, err := s.options.Store.CreateShare(r.Context(), input)
	if err != nil {
		respondError(w, err)
		return
	}
	s.shareURL(&result.Share)
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) updateShare(w http.ResponseWriter, r *http.Request) {
	var input model.ShareInput
	if !s.shareRequestAllowed(w, r) || !s.decodeShare(w, r, &input) || !s.validateShare(w, r, input) {
		return
	}
	if input.RequestsPerMinute == nil || input.MaxConcurrentDownloads == nil {
		writeError(w, http.StatusUnprocessableEntity, "Provide requests_per_minute and max_concurrent_downloads when updating a share")
		return
	}
	result, err := s.options.Store.UpdateShare(r.Context(), r.PathValue("id"), input)
	if err != nil {
		respondError(w, err)
		return
	}
	s.shareURL(&result)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) rotateShare(w http.ResponseWriter, r *http.Request) {
	if !s.shareRequestAllowed(w, r) {
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.options.Store.RotateShare(r.Context(), r.PathValue("id"), input.Revision)
	if err != nil {
		respondError(w, err)
		return
	}
	s.shareURL(&result.Share)
	writeJSON(w, http.StatusOK, result)
}

func (s *server) deleteShare(w http.ResponseWriter, r *http.Request) {
	if !s.shareRequestAllowed(w, r) {
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.options.Store.DeleteShare(r.Context(), r.PathValue("id"), input.Revision); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) decodeShare(w http.ResponseWriter, r *http.Request, input *model.ShareInput) bool {
	if !decodeJSON(w, r, input) {
		return false
	}
	format := shareRequestFormat(r)
	if input.Format != "" && input.Format != format {
		respondError(w, model.ErrInvalid)
		return false
	}
	input.Format = format
	if format == "torznab" || format == "stremio" {
		input.Fields = store.CatalogFields()
	}
	return true
}

func (s *server) catalogFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Authorization")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "Shared catalogues are read-only")
		return
	}
	// Configuring an HTTPS origin is the explicit reverse-proxy deployment
	// boundary. Never infer it from Host, Forwarded, or X-Forwarded-Proto.
	if s.sharingPublicURL() == "" {
		writeError(w, http.StatusServiceUnavailable, "HTTPS catalogue sharing is not configured")
		return
	}
	authorizations := r.Header.Values("Authorization")
	if len(authorizations) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="catalogue"`)
		writeError(w, http.StatusUnauthorized, "A valid sharing password is required")
		return
	}
	parts := strings.Fields(authorizations[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="catalogue"`)
		writeError(w, http.StatusUnauthorized, "A valid sharing password is required")
		return
	}
	budget, budgetErr := s.options.Store.ConnectionTimeout(r.Context(), "catalog")
	if budgetErr != nil {
		respondError(w, budgetErr)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "Catalogue delivery is unavailable")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid catalogue query")
		return
	}
	for key, values := range query {
		if len(values) != 1 || (key != "limit" && key != "cursor" && key != "checkpoint") {
			writeError(w, http.StatusBadRequest, "Only limit, cursor, or checkpoint catalogue parameters are allowed")
			return
		}
	}
	if query.Has("cursor") && query.Has("checkpoint") {
		writeError(w, http.StatusBadRequest, "Use either cursor or checkpoint, never both")
		return
	}
	request := store.CatalogPageRequest{Cursor: query.Get("cursor"), Checkpoint: query.Get("checkpoint"), Head: r.Method == http.MethodHead}
	if query.Has("limit") {
		request.Limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || request.Limit < 1 || request.Limit > 1000 {
			writeError(w, http.StatusBadRequest, "Limit must be between 1 and 1000")
			return
		}
	}
	download, err := s.options.Store.BeginCatalogDownload(ctx, r.PathValue("id"), parts[1])
	if err != nil {
		catalogFeedError(w, err)
		return
	}
	stopRelease := context.AfterFunc(ctx, func() { _ = download.Close() })
	defer func() {
		stopRelease()
		_ = download.Close()
	}()
	result, err := download.Page(ctx, request)
	if err != nil {
		catalogFeedError(w, err)
		return
	}
	var body []byte
	if !request.Head {
		body, err = json.Marshal(result)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not encode the catalogue response")
			return
		}
	}
	if err := download.Validate(ctx); err != nil {
		catalogFeedError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if !request.Head {
		if _, err := w.Write(body); err != nil {
			return
		}
	}
	// net/http can buffer a small response until ServeHTTP returns. Flush before
	// releasing admission so even buffered delivery is inside the lease.
	_ = controller.Flush()
}

func catalogFeedError(w http.ResponseWriter, err error) {
	var quota *store.CatalogQuotaError
	switch {
	case errors.Is(err, store.ErrCatalogAuthentication):
		w.Header().Set("WWW-Authenticate", `Bearer realm="catalogue"`)
		writeError(w, http.StatusUnauthorized, "A valid sharing password is required")
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		writeError(w, http.StatusTooManyRequests, quota.Error())
	case errors.Is(err, store.ErrCatalogCursor):
		writeError(w, http.StatusBadRequest, "Invalid or outdated catalogue continuation; restart the download")
	case errors.Is(err, store.ErrCatalogExpired):
		writeJSON(w, http.StatusGone, map[string]string{"code": model.CatalogSnapshotExpiredCode, "error": "Catalog snapshot expired; restart the download"})
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, http.StatusServiceUnavailable, "Catalogue delivery timed out or was interrupted")
	default:
		respondError(w, err)
	}
}
