package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// ProviderError retains a fixed category, never an upstream message, URL or key.
// RetryAfter is bounded and only meaningful for temporary provider failures.
type ProviderError struct {
	Code       string
	RetryAfter int
	cause      error
}

func (e *ProviderError) Error() string { return model.ProviderFailureMessage(e.Code) }
func (e *ProviderError) Unwrap() error { return e.cause }
func (e *ProviderError) HTTPStatus() int {
	switch e.Code {
	case "rate_limit":
		return http.StatusTooManyRequests
	case "account_limit", "not_cached":
		return http.StatusConflict
	case "subscription":
		return http.StatusForbidden
	case "provider_unavailable", "timeout":
		return http.StatusServiceUnavailable
	case "invalid_torrent", "file_too_large":
		return http.StatusUnprocessableEntity
	case "not_found":
		return http.StatusNotFound
	default:
		// A provider credential failure must not expire the admin session.
		return http.StatusBadGateway
	}
}

// Codes come from the providers' error tables; unrecognized values are not kept.
// https://docs.alldebrid.com/ and https://api-docs.torbox.app/
func providerResponseError(raw json.RawMessage) *ProviderError {
	var code string
	if json.Unmarshal(raw, &code) != nil {
		var object struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &object)
		code = object.Code
	}
	category := "provider_error"
	switch code {
	case "AUTH_MISSING_APIKEY", "AUTH_BAD_APIKEY", "AUTH_BLOCKED", "AUTH_USER_BANNED", "NO_AUTH", "BAD_TOKEN", "AUTH_ERROR", "OAUTH_VERIFICATION_ERROR":
		category = "authentication"
	case "MAGNET_TOO_MANY", "MAGNET_TOO_MANY_ACTIVE", "LINK_TOO_MANY_DOWNLOADS", "LINK_HOST_LIMIT_REACHED", "FREE_TRIAL_LIMIT_REACHED", "MONTHLY_LIMIT", "ACTIVE_LIMIT":
		category = "account_limit"
	case "MUST_BE_PREMIUM", "MAGNET_MUST_BE_PREMIUM", "PLAN_RESTRICTED_FEATURE":
		category = "subscription"
	case "COOLDOWN_LIMIT":
		category = "rate_limit"
	case "MAINTENANCE", "MAGNET_INTERNAL_ERROR", "MAGNET_UPLOAD_FAILED", "LINK_HOST_UNAVAILABLE", "LINK_HOST_FULL", "LINK_TEMPORARY_UNAVAILABLE", "DATABASE_ERROR", "NO_SERVERS_AVAILABLE_ERROR", "DOWNLOAD_SERVER_ERROR", "SEARCH_ERROR":
		category = "provider_unavailable"
	case "MAGNET_INVALID_URI", "MAGNET_INVALID_FILE", "MAGNET_NO_URI", "MAGNET_PROCESSING_FAILED", "BOZO_TORRENT":
		category = "invalid_torrent"
	case "MAGNET_TOO_LARGE", "MAGNET_MAGNET_TOO_BIG", "DOWNLOAD_TOO_LARGE":
		category = "file_too_large"
	case "MAGNET_INVALID_ID", "LINK_DOWN", "ITEM_NOT_FOUND", "LINK_OFFLINE":
		category = "not_found"
	case "DOWNLOAD_NOT_CACHED":
		category = "not_cached"
	}
	return newProviderError(category, "")
}

func newProviderError(code, retryAfter string) *ProviderError {
	e := &ProviderError{Code: code}
	if code == "authentication" {
		e.cause = ErrAuth
	}
	if code == "timeout" {
		e.cause = context.DeadlineExceeded
	}
	if code == "rate_limit" || code == "provider_unavailable" || code == "timeout" {
		e.RetryAfter = 60
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
			e.RetryAfter = min(max(seconds, 1), 3600)
		} else if date, err := http.ParseTime(retryAfter); err == nil {
			e.RetryAfter = min(max(int(time.Until(date).Seconds())+1, 1), 3600)
		}
	}
	return e
}

func providerHTTPError(status int, retryAfter string, decoded *ProviderError) *ProviderError {
	if status == http.StatusTooManyRequests {
		// AllDebrid also reports full accounts with HTTP 429. Waiting alone
		// does not free capacity; preserve that documented account failure.
		if decoded != nil && decoded.Code == "account_limit" {
			return decoded
		}
		return newProviderError("rate_limit", retryAfter)
	}
	if status >= 500 {
		return newProviderError("provider_unavailable", retryAfter)
	}
	if decoded != nil && decoded.Code != "provider_error" {
		return newProviderError(decoded.Code, retryAfter)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return newProviderError("authentication", "")
	}
	return newProviderError("provider_error", "")
}

func providerFailureCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrAuth):
		return "authentication"
	}
	var remote *ProviderError
	if errors.As(err, &remote) {
		return remote.Code
	}
	return "provider_error"
}

func stopProviderChecks(err error) bool {
	switch providerFailureCode(err) {
	case "authentication", "subscription", "rate_limit", "account_limit", "provider_unavailable", "timeout":
		return true
	}
	return false
}
