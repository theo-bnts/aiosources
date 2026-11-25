package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/connectors"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

type torznabCapabilityDraft struct {
	ID             string           `json:"id"`
	Adapter        string           `json:"adapter"`
	URL            string           `json:"url"`
	Auth           model.Auth       `json:"auth"`
	RequestTimeout string           `json:"request_timeout"`
	RateLimitReset string           `json:"rate_limit_reset"`
	HTTP           model.HTTPConfig `json:"http"`
	Options        map[string]any   `json:"options"`
}

func (s *server) inspectTorznabCapabilities(w http.ResponseWriter, r *http.Request) {
	var input struct {
		JSON     string `json:"json"`
		SourceID string `json:"source_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !s.sourceSizeAllowed(w, r, input.JSON) {
		return
	}
	if !utf8.ValidString(input.JSON) || !uniqueJSONObject([]byte(input.JSON)) {
		writeError(w, http.StatusUnprocessableEntity, "Repair the JSON definition before fetching capabilities.")
		return
	}
	var draft torznabCapabilityDraft
	decoder := json.NewDecoder(strings.NewReader(input.JSON))
	decoder.UseNumber()
	if err := decoder.Decode(&draft); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid connection or authentication field types in the JSON definition.")
		return
	}
	providerID := draft.ID
	if model.ValidSourceID(input.SourceID) {
		providerID = input.SourceID
	}
	provider := model.Provider{
		ID:             providerID,
		Adapter:        draft.Adapter,
		URL:            draft.URL,
		Auth:           draft.Auth,
		RequestTimeout: draft.RequestTimeout,
		RateLimitReset: draft.RateLimitReset,
		HTTP:           draft.HTTP,
		Options:        draft.Options,
	}

	select {
	case s.torznabCaps <- struct{}{}:
		defer func() { <-s.torznabCaps }()
	default:
		writeError(w, http.StatusTooManyRequests, "Too many capability checks are running. Try again shortly.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	network := &outbound.Router{
		Settings: s.options.Store.NetworkSettings,
		Secrets:  s.options.Vault.Resolve,
	}
	result, err := connectors.InspectTorznabCapabilities(ctx, provider, connectors.Environment{
		Network: network,
		Secrets: s.options.Vault.Resolve,
	})
	if err != nil {
		var capErr *connectors.TorznabCapabilityError
		if errors.As(err, &capErr) {
			writeJSON(w, capErr.Status, map[string]string{
				"error": capErr.Message,
				"code":  capErr.Code,
			})
			return
		}
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
