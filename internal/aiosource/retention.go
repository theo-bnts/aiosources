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
		policy, policyErr := s.DB.RuntimeValues(s.ctx, "history")
		pass := 10 * time.Second
		if policyErr == nil {
			pass = time.Duration(policy["pass_seconds"]) * time.Second
		}
		ctx, cancel := context.WithTimeout(s.ctx, pass)
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
		delay := 30 * time.Second
		if policyErr == nil {
			delay = time.Duration(policy["cleanup_seconds"]) * time.Second
		}
		if err == nil && removed > 0 {
			delay = time.Second
		}
		timer.Reset(delay)
	}
}
