package httpapi

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerPublicTorznab(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/public/torznab/install", func(w http.ResponseWriter, r *http.Request) {
		if !s.publicOriginAllowed() {
			writeError(w, 404, "Public access is disabled")
			return
		}
		if r.Header.Get("Origin") != s.auth.origin {
			writeError(w, 403, "Request origin is not allowed")
			return
		}
		if !s.public.admit(w, r, true) || !s.public.acquire(w) {
			return
		}
		defer func() { <-s.public.slots }()
		var input struct {
			Options   model.PublicTorznabOptions `json:"options"`
			Announces map[string]string          `json:"announces"`
		}
		defer func() { clear(input.Announces) }()
		r.Body = http.MaxBytesReader(w, r.Body, aiosource.MaxPublicInstallationSize)
		if !decodeJSON(w, r, &input) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		key, err := s.options.AIOSource.CreatePublicTorznab(ctx, s.auth.origin, input.Options, input.Announces)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				writeError(w, 404, "Public Torznab is disabled")
				return
			}
			writeError(w, 400, "Check public Torznab access, filters and your complete announce URLs. Shorten optional URLs if the configuration is too large.")
			return
		}
		writeJSON(w, 200, map[string]string{"url": s.auth.origin + "/public/torznab/api", "api_key": key})
	})
	mux.HandleFunc("/public/torznab/api", s.publicTorznabFeed)
}

func (s *server) publicTorznabFeed(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		torznabError(w, 405, 202, "Read-only endpoint")
		return
	}
	if !s.publicOriginAllowed() {
		torznabError(w, 401, 100, "Invalid public configuration")
		return
	}
	if !s.public.tryAdmit(r, false) {
		w.Header().Set("Retry-After", "60")
		torznabError(w, 429, 500, "Request limit reached")
		return
	}
	if !s.public.tryAcquire() {
		w.Header().Set("Retry-After", "1")
		torznabError(w, 429, 500, "Request limit reached")
		return
	}
	defer func() { <-s.public.slots }()
	ctx, cancel := context.WithTimeout(r.Context(), store.CatalogDownloadTimeout)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		torznabError(w, 503, 910, "Delivery unavailable")
		return
	}
	q, values, queryErr := torznabQuery(r.URL.RawQuery)
	if values == nil {
		torznabError(w, 400, 201, "Incorrect parameter")
		return
	}
	visitor, err := s.options.AIOSource.OpenPublicTorznab(ctx, s.auth.origin, values.Get("apikey"))
	if err != nil {
		torznabError(w, 401, 100, "Invalid public configuration")
		return
	}
	defer clear(visitor.Announces)
	if queryErr != nil {
		code := 201
		if errors.Is(queryErr, model.ErrNotFound) {
			code = 202
		}
		torznabError(w, 400, code, "Unsupported function or incorrect parameter")
		return
	}
	if !visitor.Access.ExposeSources && len(q.Providers) > 0 {
		torznabError(w, 400, 201, "Incorrect parameter")
		return
	}
	// Services only select known shared availability; credentials are configured
	// in the recipient's download client. No provider calls or visitor rows here.
	if len(visitor.Options.Services) > 0 {
		if q.Services == nil {
			q.Services = visitor.Options.Services
		} else {
			selected := []string{}
			for _, id := range q.Services {
				if slices.Contains(visitor.Options.Services, id) {
					selected = append(selected, id)
				}
			}
			q.Services = selected
		}
	}
	q.CachedOnly = q.CachedOnly || visitor.Options.CachedOnly
	var response any
	if q.Mode == "caps" {
		response = torznabCaps(model.Share{MediaTypes: visitor.Options.MediaTypes}, s.auth.origin)
	} else {
		policy, announces, e := s.options.AIOSource.PublicTorznabPolicy(ctx, visitor)
		if e != nil {
			torznabError(w, 503, 910, "Search unavailable")
			return
		}
		result, e := s.options.Store.PublicTorznabCatalog(ctx, q, visitor.Options.Sources, visitor.Options.MediaTypes, policy, func(rec model.AIORecord) bool { return aiosource.MatchesTorznabEpisode(rec, q.Season, q.Episode) })
		if e != nil {
			_ = s.options.Store.RecordActivity(ctx, "warn", "torznab.public_failed", "", "", "", map[string]any{"status_code": 503, "duration_ms": time.Since(start).Milliseconds()})
			torznabError(w, 503, 910, "Search unavailable; try again later")
			return
		}
		names := map[string]string{}
		if visitor.Access.ExposeSources {
			list, e := s.options.AIOSource.PublicSources(ctx)
			if e != nil {
				torznabError(w, 503, 910, "Search unavailable")
				return
			}
			for _, source := range list {
				names[source.ID] = source.Name
			}
		}
		if q.CachedOnly {
			announces = nil
		}
		response = torznabResults(result, names, s.auth.origin, announces)
		if e = s.options.Store.RecordActivity(ctx, "debug", "torznab.public_searched", "", "", "", map[string]any{"records": len(result.Items), "duration_ms": time.Since(start).Milliseconds()}); e != nil {
			torznabError(w, 503, 910, "Could not record request")
			return
		}
	}
	body, err := xml.Marshal(response)
	if err != nil {
		torznabError(w, 503, 910, "Could not encode response")
		return
	}
	if err = s.options.AIOSource.ValidatePublicTorznab(ctx, visitor); err != nil {
		torznabError(w, 401, 100, "Invalid public configuration")
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(append([]byte(xml.Header), body...))
	}
	_ = controller.Flush()
}
