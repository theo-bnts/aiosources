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
		release, ok := s.beginBackgroundTick()
		if !ok {
			timer.Reset(10 * time.Second)
			continue
		}
		policyCtx, policyCancel := context.WithTimeout(s.ctx, 5*time.Second)
		policy, policyErr := s.DB.RuntimeValues(policyCtx, "history")
		policyCancel()
		pass := 10 * time.Second
		if policyErr == nil {
			pass = time.Duration(policy["pass_seconds"]) * time.Second
		}
		ctx, cancel := context.WithTimeout(s.ctx, pass)
		removed, err := s.DB.PruneHistory(ctx)
		if s.ctx.Err() != nil {
			cancel()
			release()
			return
		}
		cancel()
		activity, stop := context.WithTimeout(s.ctx, 5*time.Second)
		if err != nil && !failed {
			slog.Warn("History cleanup will be retried" + operationStorageDetail(err))
			_ = s.DB.RecordActivity(activity, "warn", "history.retention_failed", "", "", "", nil)
		} else if err == nil && failed {
			_ = s.DB.RecordActivity(activity, "info", "history.retention_recovered", "", "", "", nil)
		}
		failed = err != nil
		stop()
		release()
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
