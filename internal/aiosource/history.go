package aiosource

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type operationHistory struct {
	service  *Service
	run      model.OperationRun
	reported bool
}

func (s *Service) beginOperation(ctx context.Context, kind, service, trigger, resource, lock string) (*operationHistory, error) {
	run, err := s.DB.BeginOperation(ctx, model.OperationRun{Kind: kind, Service: service, Trigger: trigger, Resource: resource}, lock)
	if err != nil {
		return nil, err
	}
	return &operationHistory{service: s, run: run}, nil
}

func (h *operationHistory) checkpoint(ctx context.Context, phase string) {
	h.run.Phase = phase
	if err := h.service.DB.SaveOperation(ctx, h.run); err != nil && !h.reported {
		h.reported = true
		slog.Error("Could not save an operation history checkpoint", "service", h.run.Service, "run_id", h.run.ID)
	}
}

func (h *operationHistory) finish(ctx context.Context, err error) {
	h.run.Status = "succeeded"
	if errors.Is(err, context.Canceled) {
		h.run.Status = "cancelled"
		h.run.Error = "The operation was stopped. Saved data and pending work are preserved."
		if h.service.ctx.Err() != nil {
			h.run.Status = "interrupted"
		}
	} else if err != nil {
		h.run.Status = "failed"
		if h.run.Errors == 0 {
			h.run.Errors = 1
		}
		switch h.run.Kind {
		case "metadata":
			h.run.Error = metadataDiagnostic(h.run.Phase, err).Message()
		case "matching":
			h.run.Error = "Torrent identification failed. Saved results and pending work are preserved."
		case "cache":
			h.run.Error = "Cache verification failed. Saved availability is preserved and the check can be retried."
		case "cleanup":
			h.run.Error = "Temporary magnet cleanup failed. Pending cleanup is preserved for a later attempt."
		case "download":
			h.run.Error = "The download could not be prepared. Check the debrid service and try again."
		}
		var remote *ProviderError
		if (h.run.Kind == "cache" || h.run.Kind == "cleanup" || h.run.Kind == "download") && errors.As(err, &remote) {
			h.run.Error += " " + remote.Error()
		}
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	h.checkpoint(final, h.run.Phase)
}
