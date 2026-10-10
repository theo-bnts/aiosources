package aiosource

import (
	"context"
	"errors"
	"time"
)

type playbackError struct {
	stage string
	cause error
}

func (e *playbackError) Error() string { return "provider playback failed during " + e.stage }
func (e *playbackError) Unwrap() error { return e.cause }

func (s *Service) recordPlaybackFailure(service string, err error) {
	if err == nil || errors.Is(err, ErrPending) || errors.Is(err, context.Canceled) {
		return
	}
	stage, code := "request", "upstream"
	var failure *playbackError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	case errors.Is(err, ErrAuth):
		code = "authentication"
	case errors.Is(err, ErrInvalidResponse):
		code = "parse"
	case stage == "file_selection":
		code = "no_matching_file"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	_ = s.DB.RecordPlaybackFailure(ctx, service, stage, code)
}
