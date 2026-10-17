package httpapi

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) torznabFeed(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		torznabError(w, 405, 202, "Read-only endpoint")
		return
	}
	if s.sharingPublicURL() == "" {
		torznabError(w, 503, 910, "HTTPS Torznab access is not configured")
		return
	}
	// Authenticate before reporting capabilities or parameter errors. A session,
	// Stremio token, public installation or catalog password is never sufficient.
	select {
	case s.torznab <- struct{}{}:
		defer func() { <-s.torznab }()
	default:
		w.Header().Set("Retry-After", "1")
		torznabError(w, 429, 500, "Request limit reached")
		return
	}
	q, values, queryErr := torznabQuery(r.URL.RawQuery)
	if values == nil {
		torznabError(w, 400, 201, "Incorrect parameter")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), store.CatalogDownloadTimeout)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		torznabError(w, 503, 910, "Delivery unavailable")
		return
	}
	download, err := s.options.Store.BeginTorznabRequest(ctx, r.PathValue("id"), values.Get("apikey"))
	if err != nil {
		var quota *store.CatalogQuotaError
		if errors.As(err, &quota) {
			w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
			torznabError(w, 429, 500, "Request limit reached")
		} else if errors.Is(err, store.ErrCatalogAuthentication) {
			torznabError(w, 401, 100, "Incorrect API key")
		} else {
			torznabError(w, 503, 910, "Delivery unavailable")
		}
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = download.Close() })
	defer func() { stop(); _ = download.Close() }()
	if queryErr != nil {
		code := 201
		if errors.Is(queryErr, model.ErrNotFound) {
			code = 202
		}
		torznabError(w, 400, code, "Unsupported function or incorrect parameter")
		return
	}
	share, err := s.options.Store.Share(ctx, r.PathValue("id"))
	if err != nil {
		torznabError(w, 503, 910, "Delivery unavailable")
		return
	}
	var response any
	if q.Mode == "caps" {
		response = torznabCaps(share, s.sharingPublicURL())
	} else {
		match := func(rec model.AIORecord) bool {
			return aiosource.MatchesTorznabEpisode(rec, q.Season, q.Episode)
		}
		result, e := download.Torznab(ctx, q, match)
		if e != nil {
			if errors.Is(e, store.ErrCatalogAuthentication) {
				torznabError(w, 401, 100, "Incorrect API key")
				return
			}
			_ = s.options.Store.RecordActivity(ctx, "warn", "torznab.failed", "", "", "", map[string]any{"status_code": 503, "duration_ms": time.Since(started).Milliseconds()})
			torznabError(w, 503, 910, "Search unavailable; try again later")
			return
		}
		// Display names are best effort, as in Stremio. An unreadable source
		// definition cannot revoke an already authorized published record.
		providers, _ := s.options.Providers.List()
		names := map[string]string{}
		for _, p := range providers {
			names[p.ID] = p.Name
		}
		response = torznabResults(result, names, s.sharingPublicURL())
		// Only counts enter ordinary seven-day logs. Never store the key, search
		// text, recipient URL, IP address, titles, hashes or returned magnets.
		if err = s.options.Store.RecordActivity(ctx, "debug", "torznab.searched", "", "", "", map[string]any{"records": len(result.Items), "duration_ms": time.Since(started).Milliseconds()}); err != nil {
			torznabError(w, 503, 910, "Could not record request")
			return
		}
	}
	body, err := xml.Marshal(response)
	if err != nil {
		torznabError(w, 503, 910, "Could not encode response")
		return
	}
	if err = download.Validate(ctx); err != nil {
		torznabError(w, 401, 100, "Incorrect API key")
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(append([]byte(xml.Header), body...))
	}
	_ = controller.Flush()
}
