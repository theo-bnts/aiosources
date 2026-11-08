package aiosource

import (
	"context"
	"log/slog"
	"time"
)

func (s *Service) storageIndexes() {
	defer s.wg.Done()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	failed := false
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
		changed, err := s.DB.ReconcileStorageIndexes(ctx)
		cancel()
		if s.ctx.Err() != nil {
			return
		}
		if err != nil && !failed {
			slog.Warn("Optional search index change will be retried")
		}
		failed = err != nil
		delay := 10 * time.Second
		if changed {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}
