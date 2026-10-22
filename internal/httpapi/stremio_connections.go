package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/store"
)

type privateConnectionContextKey struct{}

// Named installations share the durable admission policy used by Torznab,
// with enough time for the existing playback preparation deadline. Public
// installations do not use this middleware or create saved access records.
func (s *server) privateConnectionRequest(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	id, key, err := aiosource.PrivateConnectionKey(r.PathValue("token"))
	if err != nil {
		privateConnectionError(w, err)
		return
	}
	budget, budgetErr := s.options.Store.ConnectionTimeout(r.Context(), "stremio")
	if budgetErr != nil {
		respondError(w, budgetErr)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	controller := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		privateConnectionError(w, err)
		return
	}
	access, err := s.options.Store.BeginStremioRequest(ctx, id, key)
	if err != nil {
		privateConnectionError(w, err)
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = access.Close() })
	defer func() { stop(); _ = access.Close() }()
	next(w, r.WithContext(context.WithValue(ctx, privateConnectionContextKey{}, access)))
	_ = controller.Flush()
}

func validatePrivateConnection(w http.ResponseWriter, r *http.Request) bool {
	if access, ok := r.Context().Value(privateConnectionContextKey{}).(*store.CatalogDownload); ok {
		if err := access.Validate(r.Context()); err != nil {
			privateConnectionError(w, err)
			return false
		}
	}
	return true
}

func privateConnectionError(w http.ResponseWriter, err error) {
	var quota *store.CatalogQuotaError
	if errors.As(err, &quota) {
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		writeError(w, http.StatusTooManyRequests, "Request limit reached. Try again shortly.")
	} else if errors.Is(err, store.ErrCatalogAuthentication) || errors.Is(err, aiosource.ErrAuth) {
		writeError(w, http.StatusUnauthorized, "Invalid installation token")
	} else {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "Installation access is temporarily unavailable")
	}
}
