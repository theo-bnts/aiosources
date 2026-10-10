package aiosource

import (
	"context"
	"errors"
	"net"

	"github.com/theo-bnts/aiosources/internal/model"
)

type metadataError struct {
	code   string
	status int
	cause  error
}

func (e *metadataError) Error() string { return "TMDB request failed (" + e.code + ")" }
func (e *metadataError) Unwrap() error { return e.cause }

func metadataTransportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	code := "network"
	var timed net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timed) && timed.Timeout()) {
		code = "timeout"
	}
	return &metadataError{code: code, cause: err}
}

func metadataHTTPError(status int) error {
	code := "http"
	var cause error
	if status == 401 || status == 403 {
		code, cause = "authentication", ErrAuth
	}
	return &metadataError{code: code, status: status, cause: cause}
}

func metadataDiagnostic(operation string, err error) model.ServiceDiagnostic {
	d := model.ServiceDiagnostic{Operation: operation}
	if err == nil {
		return d
	}
	var remote *metadataError
	switch {
	case errors.Is(err, context.Canceled):
		d.FailureCode = "interrupted"
	case errors.As(err, &remote):
		d.FailureCode, d.StatusCode = remote.code, remote.status
	case errors.Is(err, ErrAuth):
		d.FailureCode = "authentication"
	case errors.Is(err, errMetadataStorage), operation == "storage", operation == "identification":
		d.FailureCode = "storage"
	case errors.Is(err, context.DeadlineExceeded):
		d.FailureCode = "timeout"
	case errors.Is(err, ErrInvalidResponse):
		d.FailureCode = "parse"
	case errors.Is(err, errMetadataGap):
		d.FailureCode = "change_gap"
	default:
		d.FailureCode = "unknown"
	}
	return d
}

func metadataStorageError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errMetadataStorage
}

var errMetadataGap = errors.New("TMDB change gap exceeds 14 days; reset required")
