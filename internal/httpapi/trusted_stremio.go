package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *server) registerTrustedStremio(mux *http.ServeMux) {
	protect := func(manifest bool, action func(http.ResponseWriter, *http.Request, *aiosource.PublicInstallation)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
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
				respondError(w, err)
				return
			}
			grant := model.TrustedGrant{UserID: connection.UserID, Generation: connection.Generation}
			visitor := &aiosource.PublicInstallation{Trusted: &grant, Generation: grant.Generation, Audience: s.connectionOrigin(), Preferences: connection.Preferences}
			if !manifest {
				visitor, err = s.options.AIOSource.TrustedVisitorWithCredentials(ctx, grant, s.connectionOrigin(), "stremio", connection.Preferences, connection.Services, connection.Announces)
				if err != nil {
					respondError(w, err)
					return
				}
			}
			defer func() { clear(visitor.Services); clear(visitor.Announces) }()
			action(w, r, visitor)
		}
	}
	s.registerVisitorStremioRoutes(
		mux,
		"/trusted/stremio",
		"private",
		"/trusted",
		s.connectionOrigin,
		func(w http.ResponseWriter, _ *http.Request) {
			respondError(w, model.ErrInvalid)
		},
		respondError,
		protect,
	)
}
