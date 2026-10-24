package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

func (p *ProviderHTTP) base(service string) string {
	switch service {
	case "alldebrid":
		return p.ADBase
	case "debridlink":
		return p.DLBase
	case "deepbrid":
		return p.DBBase
	case "premiumize":
		return p.PMBase
	case "realdebrid":
		return p.RDBase
	case "torbox":
		return p.TBBase
	default:
		return ""
	}
}

// All adapters share pacing, routing, deadlines, bounded reads and safe history.
// Upstream bodies and headers are never copied into diagnostics.
type providerResponseMetadata struct {
	Status     int
	RetryAfter string
}

func (p *ProviderHTTP) exchange(ctx context.Context, service, key, method, path string, body io.Reader, contentType string, response *providerResponseMetadata) (payload json.RawMessage, requestErr error) {
	status := 0
	if !model.IsDebridService(service) || p.base(service) == "" {
		return nil, model.ErrInvalid
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") || (service == "alldebrid" && allDebridDemoKey(key)) {
		return nil, ErrAuth
	}
	if err := p.pace(ctx, service); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.base(service), "/")+"/"+path, body)
	if err != nil {
		return nil, ErrInvalidResponse
	}
	req = req.WithContext(outbound.WithTarget(req.Context(), "debrid:"+service))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AIOSources/2")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := *p.Client
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if p.Timeout != nil && client.Timeout == 0 {
		timeout, err := p.Timeout(ctx)
		if err != nil {
			return nil, err
		}
		client.Timeout = timeout
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var timed net.Error
		if errors.As(err, &timed) && timed.Timeout() {
			return nil, newProviderError("timeout", "")
		}
		return nil, newProviderError("provider_unavailable", "")
	}
	defer res.Body.Close()
	status = res.StatusCode
	response.Status, response.RetryAfter = status, res.Header.Get("Retry-After")
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil || len(b) > 8<<20 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if status < 200 || status >= 300 {
			return nil, providerHTTPError(status, res.Header.Get("Retry-After"), nil)
		}
		return nil, ErrInvalidResponse
	}
	rejected := decodeProviderError(service, b, path)
	if status < 200 || status >= 300 {
		if status == http.StatusNotFound && rejected == nil {
			rejected = newProviderError("not_found", "")
		}
		return nil, providerHTTPError(status, res.Header.Get("Retry-After"), rejected)
	}
	if rejected != nil {
		return nil, newProviderError(rejected.Code, res.Header.Get("Retry-After"))
	}
	if (status == http.StatusNoContent || status == http.StatusAccepted) && len(strings.TrimSpace(string(b))) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(b) {
		return nil, ErrInvalidResponse
	}
	return b, nil
}

// Extended adapters have different success envelopes, but use the same transport.
func (p *ProviderHTTP) providerJSON(ctx context.Context, service, key, method, path string, form url.Values, out any) (requestErr error) {
	started, meta := time.Now(), providerResponseMetadata{}
	defer func() { recordProviderCall(ctx, strings.Split(path, "?")[0], meta.Status, started, requestErr) }()
	var body io.Reader
	contentType := ""
	if form != nil {
		body, contentType = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
		if service == "debridlink" {
			fields := map[string]any{}
			for key := range form {
				fields[key] = form.Get(key)
			}
			for _, key := range []string{"wait"} {
				if _, ok := fields[key]; ok {
					fields[key] = form.Get(key) == "true"
				}
			}
			encoded, err := json.Marshal(fields)
			if err != nil {
				return model.ErrInvalid
			}
			body, contentType = strings.NewReader(string(encoded)), "application/json"
		}
	}
	raw, err := p.exchange(ctx, service, key, method, path, body, contentType, &meta)
	if err != nil {
		return err
	}
	defer clear(raw)
	if service == "premiumize" {
		var envelope struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &envelope) != nil || envelope.Status != "success" {
			return ErrInvalidResponse
		}
	}
	if service == "debridlink" {
		var envelope struct {
			Success bool            `json:"success"`
			Value   json.RawMessage `json:"value"`
		}
		if json.Unmarshal(raw, &envelope) != nil || !envelope.Success || len(envelope.Value) == 0 || string(envelope.Value) == "null" {
			return ErrInvalidResponse
		}
		raw = envelope.Value
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}

func decodeProviderError(service string, raw []byte, paths ...string) *ProviderError {
	var envelope struct {
		Error     json.RawMessage `json:"error"`
		ErrorCode int             `json:"error_code"`
		Code      string          `json:"code"`
		Status    string          `json:"status"`
		Success   *bool           `json:"success"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	value := string(envelope.Error)
	failed := value != "" && value != "null" && value != "false" && value != "0" && value != `""`
	if !failed && envelope.ErrorCode == 0 && envelope.Status != "error" && (envelope.Success == nil || *envelope.Success) {
		return nil
	}
	if service == "alldebrid" || service == "torbox" {
		return providerResponseError(envelope.Error)
	}
	category := "provider_error"
	if service == "realdebrid" {
		switch envelope.ErrorCode {
		case 8, 9, 10, 11, 12, 13, 14, 15, 22:
			category = "authentication"
		case 18, 21, 23, 36:
			category = "account_limit"
		case 20:
			category = "subscription"
		case -1, 6, 17, 19, 25, 27:
			category = "provider_unavailable"
		case 5, 34:
			category = "rate_limit"
		case 7, 24, 35:
			category = "not_found"
		case 28, 30:
			category = "invalid_torrent"
		case 26, 29:
			category = "file_too_large"
		}
	} else if service == "deepbrid" {
		var code int
		_ = json.Unmarshal(envelope.Error, &code)
		switch code {
		case 1:
			if len(paths) > 0 && strings.HasPrefix(paths[0], "torrents/info") {
				category = "not_found"
			}
		case 401:
			category = "authentication"
		case 2, 403:
			category = "subscription"
		case 404:
			category = "not_found"
		case 429:
			category = "rate_limit"
		case 500, 503:
			category = "provider_unavailable"
		}
	} else {
		var code string
		_ = json.Unmarshal(envelope.Error, &code)
		if envelope.Code != "" {
			code = envelope.Code
		}
		switch code {
		case "badToken", "authentication_failed":
			category = "authentication"
		case "permission_denied", "access_denied", "unverifiedEmail":
			category = "subscription"
		case "floodDetected", "rate_limit_reached":
			category = "rate_limit"
		case "maxTorrent", "maxTransfer", "maxData", "maxLink", "account_limit_reached", "service_limit_reached":
			category = "account_limit"
		case "internalError", "freeServerOverload", "maintenanceHost", "service_down", "transient_error", "unknown_error", "link_generation_failed":
			category = "provider_unavailable"
		case "badId", "fileNotFound", "infringingFile", "not_found":
			category = "not_found"
		case "badTorrentFile":
			category = "invalid_torrent"
		case "torrentTooBig":
			category = "file_too_large"
		}
	}
	return newProviderError(category, "")
}
