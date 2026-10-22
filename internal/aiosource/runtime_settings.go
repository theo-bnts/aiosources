package aiosource

import (
	"context"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Capacities may shrink while work is active. Existing work finishes; queued
// work observes the new limit without replacing channels or losing permits.
type capacityGate struct {
	mu     sync.Mutex
	active int
	wake   chan struct{}
}

func (g *capacityGate) try(limit int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= limit {
		return false
	}
	g.active++
	return true
}

func (g *capacityGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active--
	if g.wake != nil {
		close(g.wake)
		g.wake = nil
	}
}

func (g *capacityGate) count() int { g.mu.Lock(); defer g.mu.Unlock(); return g.active }

func (s *Service) acquireCapacity(ctx context.Context, gate *capacityGate, key string, wait bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit, err := s.DB.RuntimeValue(ctx, "debrid", key)
		if err != nil {
			return err
		}
		gate.mu.Lock()
		if gate.active < limit {
			gate.active++
			gate.mu.Unlock()
			return nil
		}
		if gate.wake == nil {
			gate.wake = make(chan struct{})
		}
		wake := gate.wake
		gate.mu.Unlock()
		if !wait {
			return model.ErrBusy
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Service) outgoingTimeout(ctx context.Context, key string) (time.Duration, error) {
	seconds, err := s.DB.RuntimeValue(ctx, "outgoing", key)
	return time.Duration(seconds) * time.Second, err
}
