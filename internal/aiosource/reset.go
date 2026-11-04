package aiosource

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// ResetState is deliberately separate from synchronization: resetting never
// downloads information, and a failed transaction leaves the old data intact.
type ResetState struct {
	Scope  string `json:"scope"`
	Status string `json:"status"`
	Phase  string `json:"phase,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Service) ResetState() ResetState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reset
}

func (s *Service) reserveReset(scope string) ([]<-chan struct{}, error) {
	switch scope {
	case "books", "videos", "metadata", "mapping", "cache":
	default:
		return nil, model.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.reset.Status == "running" {
		return nil, model.ErrBusy
	}
	// Reserve maintenance before cancelling workers so automation cannot take
	// their place. Join their complete lifetimes, including final checkpoints
	// and advisory-lock release, before removing information they could write.
	s.reset = ResetState{Scope: scope, Status: "running", Phase: "resetting"}
	var waiting []<-chan struct{}
	if scope != "cache" {
		for _, worker := range []struct {
			cancel context.CancelFunc
			done   <-chan struct{}
		}{
			{s.syncCancel, s.syncDone},
			{s.matchCancel, s.matchDone},
			{s.booksCancel, s.booksDone},
		} {
			if worker.cancel != nil {
				worker.cancel()
			}
			if worker.done != nil {
				waiting = append(waiting, worker.done)
			}
		}
	}
	if len(waiting) > 0 {
		s.reset.Phase = "stopping"
	}
	s.wg.Add(1)
	return waiting, nil
}

// StartReset acknowledges admission immediately. The transaction belongs to
// the service lifetime, not to a browser/proxy request that may time out.
func (s *Service) StartReset(scope string) error {
	waiting, err := s.reserveReset(scope)
	if err != nil {
		return err
	}
	go func() { s.finishReset(scope, s.runReset(s.ctx, scope, waiting)) }()
	return nil
}

// Reset keeps a synchronous entry point for internal callers. Both paths use
// identical admission, fencing and rollback behavior; neither holds s.mu while
// waiting for PostgreSQL or prevents status reads and shutdown cancellation.
func (s *Service) Reset(ctx context.Context, scope string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	waiting, err := s.reserveReset(scope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	err = s.runReset(ctx, scope, waiting)
	s.finishReset(scope, err)
	return err
}

func (s *Service) runReset(ctx context.Context, scope string, waiting []<-chan struct{}) error {
	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.reset.Phase = "resetting"
	s.mu.Unlock()
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	if scope != "cache" {
		names := []string{"matcher"}
		if scope == "books" {
			names = append(names, "openlibrary")
		}
		if scope == "metadata" || scope == "videos" {
			names = append(names, "metadata", "openlibrary")
		}
		for _, name := range names {
			release, locked, err := s.DB.AIOLock(ctx, name)
			if err != nil {
				return err
			}
			if !locked {
				return model.ErrBusy
			}
			releases = append(releases, release)
		}
	}
	switch scope {
	case "mapping":
		return s.DB.ResetAIOMapping(ctx)
	case "books":
		return s.DB.ResetBookInformation(ctx)
	case "videos":
		return s.DB.ResetVideoInformation(ctx)
	case "metadata":
		return s.DB.ResetAIOMetadata(ctx)
	case "cache":
		s.mu.Lock()
		for _, f := range s.flights {
			f.cancel()
		}
		s.mu.Unlock()
		return s.DB.ResetAIOCache(ctx)
	}
	return model.ErrInvalid
}

func (s *Service) finishReset(scope string, err error) {
	defer s.wg.Done()
	state := ResetState{Scope: scope, Status: "succeeded"}
	level, outcome := "info", "completed"
	if err != nil {
		state.Status = "failed"
		state.Error = "The reset could not be completed. Check the logs and retry when related operations have stopped."
		level, outcome = "error", "failed"
		if errors.Is(err, context.Canceled) || s.ctx.Err() != nil {
			state.Error = "The reset was interrupted. Check the catalog before trying again."
			level, outcome = "warn", "interrupted"
		}
	}
	final, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	if logErr := s.DB.RecordActivity(final, level, "maintenance."+scope+"_"+outcome, "", "", "", nil); logErr != nil {
		slog.Error("Could not save maintenance activity", "scope", scope)
	}
	s.mu.Lock()
	s.reset = state
	s.mu.Unlock()
}
