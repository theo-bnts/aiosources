package aiosource

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// One admission queue and server cooldown cover details, seasons and changes.
// The zero value is usable by isolated clients as well as the service.
type metadataRequestLimiter struct {
	once                  sync.Once
	gate                  chan struct{}
	mu                    sync.Mutex
	interval              time.Duration
	started, blockedUntil time.Time
}

func (l *metadataRequestLimiter) setRate(rate int) {
	if rate == 0 {
		rate = 35
	}
	l.mu.Lock()
	l.interval = time.Second / time.Duration(rate)
	l.mu.Unlock()
}

func (l *metadataRequestLimiter) wait(ctx context.Context) error {
	l.once.Do(func() { l.gate = make(chan struct{}, 1) })
	select {
	case l.gate <- struct{}{}:
		defer func() { <-l.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		gap := l.interval
		if gap == 0 {
			gap = time.Second / 35
		}
		now := time.Now()
		due := maxTime(l.started.Add(gap), l.blockedUntil)
		if !due.After(now) {
			l.started = now
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		timer := time.NewTimer(time.Until(due))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *metadataRequestLimiter) cooldown(delay time.Duration) {
	l.mu.Lock()
	l.blockedUntil = maxTime(l.blockedUntil, time.Now().Add(delay))
	l.mu.Unlock()
}

func metadataRetryAfter(value string, fallback time.Duration, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		if seconds <= int64((1<<63-1)/time.Second) {
			return time.Duration(seconds) * time.Second
		}
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return fallback
}
