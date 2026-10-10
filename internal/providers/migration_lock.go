package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Acquire the registry lock with cancellation for offline source migrations.
func (r *Registry) acquireMigrationLock(ctx context.Context) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for !r.mu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	if r.root == nil {
		r.mu.Unlock()
		return fmt.Errorf("provider registry is closed")
	}
	for {
		err := syscall.Flock(int(r.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			r.mu.Unlock()
			return fmt.Errorf("cannot lock source migration")
		}
		select {
		case <-ctx.Done():
			r.mu.Unlock()
			return ctx.Err()
		case <-ticker.C:
		}
	}
	info, err := r.root.Lstat(".registry.lock")
	opened, openedErr := r.lock.Stat()
	if err != nil || openedErr != nil || !privateRegular(info) || !os.SameFile(info, opened) {
		r.release()
		return fmt.Errorf("source migration lock changed")
	}
	return nil
}
