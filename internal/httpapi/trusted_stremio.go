package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerTrustedStremio(mux *http.ServeMux) {
	protect := func(manifest bool, action func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			release, ok := s.enterConnection(w, r, true)
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
			connection, err := s.options.AIOSource.OpenTrustedConnection(ctx, s.connectionOrigin(), "stremio", r.PathValue("configuration"))
			if err != nil {
				writeError(w, 401, "Invalid installation token")
				return
			}
			grant := model.TrustedGrant{UserID: connection.UserID, Generation: connection.Generation}
			visitor := &aiosource.PublicInstallation{Trusted: &grant, Generation: grant.Generation, Audience: s.connectionOrigin(), Preferences: connection.Preferences}
			if !manifest {
				visitor, err = s.options.AIOSource.TrustedVisitorWithCredentials(ctx, grant, s.connectionOrigin(), "stremio", connection.Preferences, connection.Services, connection.Announces)
				if err != nil {
					writeError(w, 401, "Invalid installation token")
					return
				}
			}
			defer func() { clear(visitor.Services); clear(visitor.Announces) }()
			action(w, r, visitor)
		}
	}
	mux.HandleFunc("GET /trusted/stremio/{configuration}/manifest.json", protect(true, func(w http.ResponseWriter, r *http.Request, v *aiosource.PublicInstallation) {
		s.options.AIOSource.RecordAddonRequest("private", "manifest", "", "", time.Now(), nil)
		writeJSON(w, 200, aiosource.InstallationManifest(v.Preferences))
	}))
	mux.HandleFunc("GET /trusted/stremio/{configuration}/configure", protect(true, func(w http.ResponseWriter, r *http.Request, _ *aiosource.PublicInstallation) {
		http.Redirect(w, r, "/trusted", http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /trusted/stremio/{configuration}/stream/{type}/{id}", protect(false, func(w http.ResponseWriter, r *http.Request, v *aiosource.PublicInstallation) {
		id := r.PathValue("id")
		if !strings.HasSuffix(id, ".json") || len(id) > 160 {
			respondError(w, model.ErrInvalid)
			return
		}
		streams, err := s.options.AIOSource.PublicStreams(r.Context(), s.connectionOrigin(), r.PathValue("configuration"), r.PathValue("type"), strings.TrimSuffix(id, ".json"), v)
		if err != nil {
			respondError(w, err)
			return
		}
		if err = s.options.AIOSource.CheckPublicStremio(r.Context(), v); err != nil {
			writeError(w, 401, "Invalid installation token")
			return
		}
		writeJSON(w, 200, map[string]any{"streams": streams})
	}))
	mux.HandleFunc("GET /trusted/stremio/{configuration}/play/{service}/{source}/{type}/{id}", protect(false, func(w http.ResponseWriter, r *http.Request, v *aiosource.PublicInstallation) {
		link, err := s.options.AIOSource.TracePlayback(r.Context(), "private", r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), func(ctx context.Context) (string, error) {
			return s.options.AIOSource.PublicPlaybackSource(ctx, r.PathValue("source"), r.PathValue("service"), r.PathValue("type"), r.PathValue("id"), v)
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
		if err = s.options.AIOSource.CheckPublicStremio(r.Context(), v); err != nil {
			writeError(w, 401, "Invalid installation token")
			return
		}
		http.Redirect(w, r, link, http.StatusTemporaryRedirect)
	}))
	mux.HandleFunc("OPTIONS /trusted/stremio/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(204)
	})
}
