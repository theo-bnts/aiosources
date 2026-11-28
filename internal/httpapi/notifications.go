package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func (s *server) registerNotificationRoutes(mux *http.ServeMux) {
	if s.options.AIOSource == nil {
		return
	}
	mux.HandleFunc("GET /api/notifications/channels", s.auth.protect(s.listNotificationChannels))
	mux.HandleFunc("POST /api/notifications/channels", s.auth.protect(s.saveNotificationChannel))
	mux.HandleFunc("PUT /api/notifications/channels/{id}", s.auth.protect(s.saveNotificationChannel))
	mux.HandleFunc("DELETE /api/notifications/channels/{id}", s.auth.protect(s.deleteNotificationChannel))
	mux.HandleFunc("POST /api/notifications/channels/{id}/test", s.auth.protect(s.testNotificationChannel))

	mux.HandleFunc("GET /api/alerts", s.auth.protect(s.listAdminAlerts))
	mux.HandleFunc("POST /api/alerts", s.auth.protect(s.createAdminAlert))
	mux.HandleFunc("PUT /api/alerts/{id}", s.auth.protect(s.updateAdminAlert))
	mux.HandleFunc("DELETE /api/alerts/{id}", s.auth.protect(s.deleteAdminAlert))
}

func (s *server) registerTrustedAlerts(mux *http.ServeMux) {
	if s.options.AIOSource == nil {
		return
	}
	mux.HandleFunc("GET /api/trusted/notifications/channels", s.auth.protectTrusted("alerts", s.listTrustedNotificationChannels))
	mux.HandleFunc("GET /api/trusted/alerts", s.auth.protectTrusted("alerts", s.listTrustedAlerts))
	mux.HandleFunc("POST /api/trusted/alerts", s.auth.protectTrusted("alerts", s.createTrustedAlert))
	mux.HandleFunc("PUT /api/trusted/alerts/{id}", s.auth.protectTrusted("alerts", s.updateTrustedAlert))
	mux.HandleFunc("DELETE /api/trusted/alerts/{id}", s.auth.protectTrusted("alerts", s.deleteTrustedAlert))
}

func (s *server) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.AIOSource.NotificationChannels(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) listTrustedNotificationChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.AIOSource.TrustedNotificationChannels(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) saveNotificationChannel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name            string                          `json:"name"`
		Type            string                          `json:"type"`
		Enabled         *bool                           `json:"enabled"`
		IsDefault       bool                            `json:"is_default"`
		AllowTrusted    *bool                           `json:"allow_trusted"`
		Config          model.NotificationChannelConfig `json:"config"`
		SecretRef       string                          `json:"secret_ref"`
		HeaderSecretRef string                          `json:"header_secret_ref"`
		Revision        int64                           `json:"revision"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if !decodeJSON(w, r, &input) {
		return
	}
	id := r.PathValue("id")
	creating := id == ""
	channel := model.NotificationChannel{
		ID:              id,
		Name:            input.Name,
		Type:            input.Type,
		Enabled:         boolOrDefault(input.Enabled, creating),
		IsDefault:       input.IsDefault,
		AllowTrusted:    boolOrDefault(input.AllowTrusted, true),
		Config:          input.Config,
		SecretRef:       input.SecretRef,
		HeaderSecretRef: input.HeaderSecretRef,
		Revision:        input.Revision,
	}
	if err := channel.NormalizeAndValidate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	saved, err := s.options.AIOSource.SaveNotificationChannel(r.Context(), channel)
	if err != nil {
		respondError(w, err)
		return
	}
	status := http.StatusOK
	if creating {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

func (s *server) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	revision, ok := decodeRevision(w, r)
	if !ok {
		return
	}
	if err := s.options.AIOSource.DeleteNotificationChannel(r.Context(), r.PathValue("id"), revision); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) testNotificationChannel(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.options.AIOSource.TestNotificationChannel(ctx, r.PathValue("id")); err != nil {
		if errors.Is(err, model.ErrNotFound) || errors.Is(err, model.ErrInvalid) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			respondError(w, err)
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type alertFiltersJSON struct {
	Providers    []string        `json:"providers,omitempty"`
	Language     string          `json:"language,omitempty"`
	LanguageMode string          `json:"language_mode,omitempty"`
	MinSize      json.RawMessage `json:"min_size,omitempty"`
	MaxSize      json.RawMessage `json:"max_size,omitempty"`
	CachedOnly   bool            `json:"cached_only,omitempty"`
}

type alertRequestJSON struct {
	WorkID    int64            `json:"work_id"`
	Season    *int             `json:"season,omitempty"`
	Episode   *int             `json:"episode,omitempty"`
	ChannelID string           `json:"channel_id"`
	Filters   alertFiltersJSON `json:"filters"`
	Revision  int64            `json:"revision,omitempty"`
}

func parseOptionalByteSize(raw json.RawMessage) (*int64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var numeric int64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		if numeric < 0 {
			return nil, errors.New("Size must not be negative")
		}
		if numeric == 0 {
			return nil, nil
		}
		return &numeric, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, nil
		}
		parsed, err := model.ParseByteSize(text)
		if err != nil {
			return nil, err
		}
		if parsed == 0 {
			return nil, nil
		}
		return &parsed, nil
	}
	return nil, errors.New("Size must be bytes or a value such as 1 GB")
}

func decodeReleaseAlertInput(w http.ResponseWriter, r *http.Request) (model.ReleaseAlertInput, bool) {
	var raw alertRequestJSON
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if !decodeJSON(w, r, &raw) {
		return model.ReleaseAlertInput{}, false
	}
	minSize, err := parseOptionalByteSize(raw.Filters.MinSize)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return model.ReleaseAlertInput{}, false
	}
	maxSize, err := parseOptionalByteSize(raw.Filters.MaxSize)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return model.ReleaseAlertInput{}, false
	}
	input := model.ReleaseAlertInput{
		WorkID:    raw.WorkID,
		Season:    raw.Season,
		Episode:   raw.Episode,
		ChannelID: strings.TrimSpace(raw.ChannelID),
		Revision:  raw.Revision,
		Filters: model.ReleaseAlertFilters{
			Providers:    raw.Filters.Providers,
			Language:     raw.Filters.Language,
			LanguageMode: raw.Filters.LanguageMode,
			MinSize:      minSize,
			MaxSize:      maxSize,
			CachedOnly:   raw.Filters.CachedOnly,
		},
	}
	if err := input.Filters.NormalizeAndValidate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return model.ReleaseAlertInput{}, false
	}
	return input, true
}

func respondAlertError(w http.ResponseWriter, err error, isTV bool) {
	switch {
	case errors.Is(err, aiosource.ErrAlertAlreadyAvailable):
		if isTV {
			writeError(w, http.StatusConflict, "Source releases are already available for this episode.")
		} else {
			writeError(w, http.StatusConflict, "Source releases are already available for this title.")
		}
	case errors.Is(err, store.ErrDuplicateReleaseAlert):
		writeError(w, http.StatusConflict, "An alert is already active for this target.")
	case errors.Is(err, model.ErrNotFound), errors.Is(err, model.ErrConflict), errors.Is(err, model.ErrBusy), errors.Is(err, model.ErrConfigurationLimit), errors.Is(err, model.ErrInvalid), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		respondError(w, err)
	default:
		var dbErr *store.DatabaseError
		if errors.As(err, &dbErr) {
			respondError(w, err)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

func (s *server) listAdminAlerts(w http.ResponseWriter, r *http.Request) {
	items, err := s.options.AIOSource.ReleaseAlerts(r.Context(), "", "", catalogLanguage(r))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) createAdminAlert(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeReleaseAlertInput(w, r)
	if !ok {
		return
	}
	created, err := s.options.AIOSource.CreateReleaseAlert(r.Context(), model.AlertOwnerAdmin, "", nil, input, catalogLanguage(r))
	if err != nil {
		respondAlertError(w, err, input.Season != nil || input.Episode != nil)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) updateAdminAlert(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeReleaseAlertInput(w, r)
	if !ok {
		return
	}
	updated, err := s.options.AIOSource.UpdateReleaseAlert(r.Context(), r.PathValue("id"), model.AlertOwnerAdmin, "", nil, input, catalogLanguage(r))
	if err != nil {
		respondAlertError(w, err, false)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *server) deleteAdminAlert(w http.ResponseWriter, r *http.Request) {
	revision, ok := decodeRevision(w, r)
	if !ok {
		return
	}
	if err := s.options.AIOSource.DeleteReleaseAlert(r.Context(), r.PathValue("id"), model.AlertOwnerAdmin, "", revision); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listTrustedAlerts(w http.ResponseWriter, r *http.Request) {
	user := currentTrustedSession(r).User
	items, err := s.options.AIOSource.ReleaseAlerts(r.Context(), model.AlertOwnerTrusted, user.ID, catalogLanguage(r))
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) createTrustedAlert(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeReleaseAlertInput(w, r)
	if !ok {
		return
	}
	grant := trustedGrant(r)
	created, err := s.options.AIOSource.CreateReleaseAlert(r.Context(), model.AlertOwnerTrusted, grant.UserID, &grant, input, catalogLanguage(r))
	if err != nil {
		respondAlertError(w, err, input.Season != nil || input.Episode != nil)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) updateTrustedAlert(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeReleaseAlertInput(w, r)
	if !ok {
		return
	}
	grant := trustedGrant(r)
	updated, err := s.options.AIOSource.UpdateReleaseAlert(r.Context(), r.PathValue("id"), model.AlertOwnerTrusted, grant.UserID, &grant, input, catalogLanguage(r))
	if err != nil {
		respondAlertError(w, err, false)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *server) deleteTrustedAlert(w http.ResponseWriter, r *http.Request) {
	revision, ok := decodeRevision(w, r)
	if !ok {
		return
	}
	user := currentTrustedSession(r).User
	if err := s.options.AIOSource.DeleteReleaseAlert(r.Context(), r.PathValue("id"), model.AlertOwnerTrusted, user.ID, revision); err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
