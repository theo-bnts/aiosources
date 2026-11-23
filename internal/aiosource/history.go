package aiosource

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
	"github.com/theo-bnts/aiosources/internal/store"
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
			if h.run.Service == "igdb" {
				h.run.Error = "Game information update failed. Saved progress is preserved."
			}
			if h.run.Service == "software" {
				h.run.Error = "Software information update failed. Saved progress is preserved."
				var se *softwareError
				if errors.As(err, &se) && se.message != "" {
					h.run.Error = se.message
				}
			}
			if h.run.Service == "openlibrary" {
				h.run.Error = "Book information update failed. Saved progress is preserved." + operationStorageDetail(err)
				switch {
				case errors.Is(err, errBookDetailsBudget):
					h.run.Error = errBookDetailsBudget.Error() + ". Saved progress is preserved."
				case errors.Is(err, errBookContinuity):
					h.run.Error = errBookContinuity.Error() + ". Saved progress is preserved."
				case errors.Is(err, errBookRequest) && h.run.Phase == "book_changes":
					h.run.Error = "Open Library changes could not be retrieved. Saved progress is preserved."
				}
			}
			if h.run.Service == "musicbrainz" {
				h.run.Error = "Music information update failed. Saved progress is preserved."
				var me *musicError
				if errors.As(err, &me) {
					h.run.Error = me.message
				} else if errors.Is(err, music.ErrUnsupportedSchema) {
					h.run.Error = musicSchemaMessage
				}
			}
		case "matching":
			h.run.Error = "Torrent identification failed. Saved results and pending work are preserved." + operationStorageDetail(err)
		case "cache":
			h.run.Error = "Cache verification failed. Saved availability is preserved and the check can be retried."
		case "cleanup":
			h.run.Error = "Temporary magnet cleanup failed. Pending cleanup is preserved for a later attempt."
		case "download":
			h.run.Error = "The download could not be prepared. Check the debrid service and try again."
		}
		var remote *ProviderError
		if h.run.Kind == "cache" || h.run.Kind == "cleanup" || h.run.Kind == "download" {
			switch {
			case errors.As(err, &remote):
				h.run.Error += " " + remote.Error()
			case errors.Is(err, ErrInvalidResponse):
				h.run.Error += " " + model.ProviderFailureMessage("invalid_response")
			default:
				h.run.Error += operationStorageDetail(err)
			}
		}
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	h.checkpoint(final, h.run.Phase)
}

func operationStorageDetail(err error) string {
	var storage *store.DatabaseError
	var state interface{ SQLState() string }
	switch {
	case errors.As(err, &storage):
		return " " + storage.Error() + "."
	case errors.As(err, &state):
		return " Database operation failed (SQLSTATE " + state.SQLState() + ")."
	case errors.Is(err, context.DeadlineExceeded):
		return " The operation exceeded its time limit."
	case errors.Is(err, model.ErrConflict):
		return " Stored data changed while the operation was running."
	}
	return ""
}
