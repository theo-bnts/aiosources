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
		case status.ChangesThrough != nil && now.Sub(*status.ChangesThrough) > 14*24*time.Hour:
			p.Updates = "blocked"
		default:
			next := now
			if status.LastSync != nil {
				next = status.LastSync.Add(time.Duration(c.SyncMinutes) * time.Minute)
			}
			if status.MetadataDue {
				next = now
			}
			if status.SyncRetryAt != nil {
				next = *status.SyncRetryAt
			}
			p.NextUpdate = &next
			p.Updates = "scheduled"
			if !next.After(now) {
				p.Updates = "due"
			} else if status.SyncRetryAt != nil {
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
		default:
			p.Identification = "up_to_date"
		}
	}
	if matching {
		p.Identification = "running"
	}
	return p
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.runAutomation()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-s.automationWake:
		}
		s.runAutomation()
	}
}

func (s *Service) runAutomation() {
	c, err := s.DB.AIOConfig(s.ctx)
	if err != nil {
		return
	}
	status, err := s.DB.AIOAutomationStatus(s.ctx)
	if err != nil {
		return
	}
	syncing, matching, _ := s.State()
	p := AutomationPlan(c, status, syncing, matching, time.Now())
	if p.Updates == "due" {
		_ = s.startJob(true, func(ctx context.Context) error { return s.syncMetadata(ctx, c, true) })
	}
	if p.Identification == "pending" {
		_ = s.startMatch("automatic")
	}
}

func syncRetryDelay(attempt int) time.Duration {
	return min(time.Minute*time.Duration(1<<min(max(attempt-1, 0), 4)), 15*time.Minute)
}
