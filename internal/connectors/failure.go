package connectors

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net"

	sdk "github.com/moodiness/ingest/torznab"

	"github.com/theo-bnts/aiosources/internal/model"
)

// FailureError carries a finite safe category, never an upstream message, URL,
// credential, response body or certificate identity.
type FailureError struct{ code string }

func (e *FailureError) Error() string {
	switch e.code {
	case "authentication":
		return "source authentication was rejected"
	case "certificate":
		return "source certificate verification failed"
	case "network":
		return "source network request failed"
	case "timeout":
		return "source request timed out"
	case "http":
		return "source returned an unsuccessful HTTP response"
	case "parse":
		return "source response could not be parsed"
	case "stalled":
		return "source pagination did not advance"
	case "configuration":
		return "source configuration is invalid"
	default:
		return "source operation failed"
	}
}

func (e *FailureError) Is(target error) bool {
	return (e.code == "stalled" && target == model.ErrStalled) ||
		(e.code == "configuration" && target == model.ErrInvalid) ||
		(e.code == "timeout" && target == context.DeadlineExceeded)
}

func ValidFailureCode(code string) bool {
	switch code {
	case "authentication", "certificate", "network", "timeout", "http", "parse", "stalled", "configuration", "unknown":
		return true
	}
	return false
}

func FailureCode(err error) string {
	var failure *FailureError
	if errors.As(err, &failure) && ValidFailureCode(failure.code) {
		return failure.code
	}
	var httpError *sdk.HTTPError
	var apiError *sdk.APIError
	if errors.As(err, &apiError) && apiError.Code >= 100 && apiError.Code <= 102 {
		return "authentication"
	}
	if errors.As(err, &httpError) {
		if httpError.StatusCode == 401 || httpError.StatusCode == 403 {
			return "authentication"
		}
		return "http"
	}
	var verification *tls.CertificateVerificationError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var authority x509.UnknownAuthorityError
	var roots x509.SystemRootsError
	if errors.As(err, &verification) || errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &authority) || errors.As(err, &roots) {
		return "certificate"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, model.ErrStalled) {
		return "stalled"
	}
	if errors.Is(err, errQuota) {
		return "http"
	}
	if errors.Is(err, errResponseLimit) {
		return "parse"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	}
	var jsonError *json.SyntaxError
	var jsonType *json.UnmarshalTypeError
	var xmlError *xml.SyntaxError
	if errors.As(err, &jsonError) || errors.As(err, &jsonType) || errors.As(err, &xmlError) {
		return "parse"
	}
	if errors.Is(err, model.ErrInvalid) {
		return "configuration"
	}
	return "unknown"
}

func safeFailure(err error, fallback string) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	code := FailureCode(err)
	if code == "unknown" && ValidFailureCode(fallback) {
		code = fallback
	}
	return &FailureError{code: code}
}

func httpFailure(status int) error {
	if status == 401 || status == 403 {
		return &FailureError{code: "authentication"}
	}
	return &FailureError{code: "http"}
}
