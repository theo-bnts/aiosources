package aiosource

import (
	"context"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// AutomationPlan is shared by admission and the console, so the displayed next
// update follows the same persisted checkpoints as the actual worker.
func AutomationPlan(c model.AIOConfig, status model.AIOStatus, syncing, matching bool, now time.Time) model.AIOAutomation {
	p := model.AIOAutomation{Updates: "disabled", Identification: "disabled"}
	if c.AutoSync {
		switch {
		case c.TMDBSecret == "":
			p.Updates = "missing_token"
		default:
			next := now
			if status.LastSync != nil {
				next = status.LastSync.Add(time.Duration(c.SyncMinutes) * time.Minute)
			}
			if status.MetadataDue || MetadataRecoveryRequired(status, now) {
				next = now
			}
			if status.SyncRetryAt != nil {
				next = *status.SyncRetryAt
			}
			if status.ProviderRetryAt != nil && status.ProviderRetryAt.After(next) {
				next = *status.ProviderRetryAt
			}
			p.NextUpdate = &next
			p.Updates = "scheduled"
			if !next.After(now) {
				p.Updates = "due"
			} else if status.SyncRetryAt != nil || status.ProviderRetryAt != nil && status.ProviderRetryAt.After(now) {
				p.Updates = "retry"
			}
		}
	}
	if syncing {
		p.Updates, p.NextUpdate = "running", nil
	}
	if c.AutoMatch {
		switch {
		case status.Works == 0:
			p.Identification = "waiting_information"
		case status.MatchingPending > 0:
			p.Identification = "pending"
			next := now
			if status.LastMatch != nil {
				next = status.LastMatch.Add(time.Duration(c.MatchSeconds) * time.Second)
			}
			p.NextIdentification = &next
			if next.After(now) {
				p.Identification = "scheduled"
			}
		default:
			p.Identification = "up_to_date"
		}
	}
	if matching {
		p.Identification = "running"
		p.NextIdentification = nil
	}
	return p
}

func MetadataRecoveryRequired(status model.AIOStatus, now time.Time) bool {
	return status.RecoveryStartedAt != nil || status.ChangesThrough != nil && now.Sub(*status.ChangesThrough) > 14*24*time.Hour
}

func (s *Service) wakeAutomation() {
	select {
	case s.automationWake <- struct{}{}:
	default:
	}
}

// Keep local admission separate from provider cleanup: a slow debrid request
// must not hold up a saved information setting or newly imported torrents.
func (s *Service) automation() {
	defer s.wg.Done()
	timer := time.NewTimer(s.runAutomation())
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
		case <-s.automationWake:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(s.runAutomation())
	}
}

func (s *Service) runAutomation() time.Duration {
	poll := 30 * time.Second
	c, err := s.DB.AIOConfig(s.ctx)
	if err != nil {
		return poll
	}
	status, err := s.DB.AIOAutomationStatus(s.ctx)
	if err != nil {
		return poll
	}
	if c.AutoMatch && c.MatchSeconds > 0 {
		poll = min(poll, time.Duration(c.MatchSeconds)*time.Second)
	}
	if c.OpenLibraryEnabled && !s.BookState() {
		if due, e := s.DB.OpenLibraryDue(s.ctx, c.OpenLibraryMinutes); e == nil && due {
			_ = s.StartBooks("automatic")
		}
	}
	syncing, matching, _ := s.State()
	now := time.Now()
	p := AutomationPlan(c, status, syncing, matching, now)
	if p.Updates == "due" {
		_ = s.startJob(true, func(ctx context.Context) error { return s.syncMetadata(ctx, c, true) })
	}
	if p.Identification == "pending" {
		_ = s.startMatch("automatic")
	}
	for _, next := range []*time.Time{p.NextUpdate, p.NextIdentification} {
		if next != nil && next.After(now) {
			poll = min(poll, next.Sub(now))
		}
	}
	return max(poll, time.Millisecond)
}

func syncRetryDelay(attempt int) time.Duration {
	return model.RetryBackoff(attempt-1, time.Minute, 15*time.Minute)
}
