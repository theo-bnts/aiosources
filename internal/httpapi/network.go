package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

func (s *server) registerNetworkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/network", s.auth.protect(s.networkSettings))
	mux.HandleFunc("PUT /api/settings/network", s.auth.protect(s.updateNetworkSettings))
	mux.HandleFunc("POST /api/settings/network/test", s.auth.protect(s.testNetworkProxy))
}

func (s *server) networkSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.options.Store.NetworkSettings(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *server) updateNetworkSettings(w http.ResponseWriter, r *http.Request) {
	var input model.NetworkSettings
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := input.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.options.Store.UpdateNetworkSettings(r.Context(), input); err != nil {
		respondError(w, err)
		return
	}
	s.networkSettings(w, r)
}

func (s *server) testNetworkProxy(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	settings, err := s.options.Store.NetworkSettings(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	found := false
	for i := range settings.Proxies {
		if settings.Proxies[i].ID == input.ID {
			// Test a saved disabled profile before directing live work through it.
			settings.Proxies[i].Enabled = true
			found = true
			break
		}
	}
	if !found {
		respondError(w, model.ErrNotFound)
		return
	}
	settings.Default, settings.Overrides = input.ID, map[string]string{}
	router := &outbound.Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }, Secrets: s.options.Vault.Resolve}
	transport := router.Transport("", outbound.HTTPTransport())
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	started := time.Now()
	// Fixed public HTTPS destination: this endpoint is never an open proxy or
	// an arbitrary URL fetcher, and sends no source or service credentials.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var ip, warp string
	if err == nil {
		var response *http.Response
		response, err = client.Do(request)
		if err == nil {
			var body []byte
			body, err = io.ReadAll(io.LimitReader(response.Body, 8193))
			response.Body.Close()
			if err == nil && response.StatusCode == http.StatusOK && len(body) <= 8192 {
				for line := range strings.SplitSeq(string(body), "\n") {
					name, value, _ := strings.Cut(strings.TrimSpace(line), "=")
					if name == "ip" {
						if parsed, parseErr := netip.ParseAddr(value); parseErr == nil {
							ip = parsed.String()
						}
					}
					if name == "warp" && (value == "on" || value == "off" || value == "plus") {
						warp = value
					}
				}
			}
		}
	}
	kind, level := "network.test_completed", "info"
	if err != nil || ip == "" {
		kind, level = "network.test_failed", "warn"
	}
	logCtx, stopLog := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	logErr := s.options.Store.RecordActivity(logCtx, level, kind, "", "", "", nil)
	stopLog()
	if logErr != nil {
		respondError(w, logErr)
		return
	}
	if kind == "network.test_failed" {
		writeError(w, http.StatusBadGateway, "Proxy connection test failed. Check the proxy address, credentials and network access.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exit_ip": ip, "warp": warp, "duration_ms": time.Since(started).Milliseconds()})
}
