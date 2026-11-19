package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/inbound"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

func (s *server) registerNetworkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/network", s.auth.protect(s.networkSettings))
	mux.HandleFunc("GET /api/settings/network/incoming", s.auth.protect(s.incomingNetworkStatus))
	mux.HandleFunc("PUT /api/settings/network", s.auth.protect(s.updateNetworkSettings))
	mux.HandleFunc("POST /api/settings/network/test", s.auth.protect(s.testNetworkProxy))
}

// These are the administrator's current request addresses, never stored or logged.
func (s *server) incomingNetworkStatus(w http.ResponseWriter, r *http.Request) {
	settings, err := s.options.Store.NetworkSettings(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	resolver, err := inbound.New(settings.Incoming)
	if err != nil {
		respondError(w, model.ErrInvalid)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"connection_ip": inbound.Direct(r), "visitor_ip": resolver.ClientIP(r)})
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
	s.policies.invalidate()
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
	target := ""
	found := false
	for i := range settings.Proxies {
		if settings.Proxies[i].ID == input.ID {
			// Test a saved disabled profile before directing live work through it.
			settings.Proxies[i].Enabled = true
			if settings.Proxies[i].Type == model.NetworkMediaFlow {
				target = "debrid:alldebrid"
			}
			found = true
			break
		}
	}
	if !found {
		respondError(w, model.ErrNotFound)
		return
	}
	settings.Default, settings.Overrides = model.NetworkSystem, map[string]string{}
	if target == "" {
		settings.Default = input.ID
	} else {
		settings.Overrides[target] = input.ID
	}
	router := &outbound.Router{Settings: func(context.Context) (model.NetworkSettings, error) { return settings, nil }, Secrets: s.options.Vault.Resolve}
	transport := router.Transport(target, outbound.HTTPTransport())
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	started := time.Now()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	message := "Proxy connection test failed. Check the proxy address, credentials and network access."
	if target != "" {
		err = checkMediaFlowRedirect(ctx, client)
		if err != nil {
			message = err.Error()
		}
	}
	var ip, warp string
	if err == nil {
		var response *http.Response
		response, err = networkProbe(ctx, client, "/cdn-cgi/trace")
		if err == nil {
			var body []byte
			body, err = io.ReadAll(io.LimitReader(response.Body, 8193))
			response.Body.Close()
			if err == nil && response.StatusCode == http.StatusOK && len(body) <= 8192 {
				for line := range strings.SplitSeq(string(body), "\n") {
					name, value, _ := strings.Cut(strings.TrimSpace(line), "=")
					if name == "ip" {
						if parsed, parseErr := netip.ParseAddr(value); parseErr == nil && parsed.Zone() == "" {
							ip = parsed.Unmap().String()
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
		writeError(w, http.StatusBadGateway, message)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exit_ip": ip, "warp": warp, "duration_ms": time.Since(started).Milliseconds()})
}

func networkProbe(ctx context.Context, client *http.Client, path string) (*http.Response, error) {
	// Fixed public HTTPS destinations. Tests carry no service credentials.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.cloudflare.com"+path, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func checkMediaFlowRedirect(ctx context.Context, client *http.Client) error {
	response, err := networkProbe(ctx, client, "/ips")
	if err != nil {
		return outbound.ErrUnavailable
	}
	defer response.Body.Close()
	location, err := response.Location()
	if response.StatusCode < 300 || response.StatusCode >= 400 || err != nil || location.String() != "https://www.cloudflare.com/ips/" {
		return errors.New("MediaFlow Proxy did not return the expected redirect. Check that MediaFlow Proxy Light has APP__PROXY__FOLLOW_REDIRECTS=false and can reach www.cloudflare.com")
	}
	return nil
}
