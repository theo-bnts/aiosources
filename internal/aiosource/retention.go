package aiosource

import (
	"context"
	"log/slog"
	"time"
)

func (s *Service) retainHistory() {
	defer s.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	failed := false
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		}
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		removed, err := s.DB.PruneHistory(ctx)
		if s.ctx.Err() != nil {
			cancel()
			return
		}
		cancel()
		activity, stop := context.WithTimeout(s.ctx, 5*time.Second)
		if err != nil && !failed {
			slog.Warn("History cleanup will be retried")
			_ = s.DB.RecordActivity(activity, "warn", "history.retention_failed", "", "", "", nil)
		} else if err == nil && failed {
			_ = s.DB.RecordActivity(activity, "info", "history.retention_recovered", "", "", "", nil)
		}
		failed = err != nil
		stop()
		seconds, policyErr := s.DB.RuntimeValue(s.ctx, "history", "cleanup_seconds")
		delay := 30 * time.Second
		if policyErr == nil {
			delay = time.Duration(seconds) * time.Second
		}
		if err == nil && removed > 0 {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}
