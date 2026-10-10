// Package health measures database storage and local available space. It never
// deletes raw observations, catalogue journals, or semantic history.
package health

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

type Service struct {
	db       *store.Store
	stateDir string
	mu       sync.RWMutex
	started  bool
	closed   bool
	cancel   context.CancelFunc
	done     chan struct{}
	err      error
	report   *model.SystemHealth
}

func New(db *store.Store, stateDir string) (*Service, error) {
	if db == nil || stateDir == "" {
		return nil, errors.New("health service requires storage and a state directory")
	}
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, errors.New("health state directory is invalid")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return nil, errors.New("health state directory is unavailable")
	}
	return &Service{db: db, stateDir: absolute, done: make(chan struct{})}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return fmt.Errorf("%w: health service cannot be started again", model.ErrConflict)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.started = true
	go s.run(ctx)
	return nil
}

func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	defer func() {
		if recover() != nil {
			s.mu.Lock()
			s.err = errors.New("health monitor stopped unexpectedly")
			s.mu.Unlock()
		}
	}()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.measure(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) measure(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	disk := localDisk(s.stateDir)
	report, err := s.db.MeasureHealth(ctx, disk)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		s.mu.RLock()
		previous := s.report
		s.mu.RUnlock()
		report = &model.SystemHealth{Database: model.HealthDatabase{Categories: []model.HealthStorageCategory{}}}
		if previous != nil {
			*report = *previous
		}
		report.CheckedAt = time.Now().UTC()
		report.NextCheckAt = report.CheckedAt.Add(time.Minute)
		report.Status = "unavailable"
		report.Database.Available = false
		report.Disk = disk
	}
	s.mu.Lock()
	s.report = report
	s.mu.Unlock()
}

func localDisk(path string) model.HealthDisk {
	value := model.HealthDisk{Scope: "Application state-directory filesystem on this server; not the PostgreSQL server volume", MeasuredAt: time.Now().UTC()}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil || stat.Bsize <= 0 {
		return value
	}
	value.Available = true
	value.TotalBytes = stat.Blocks * uint64(stat.Bsize)
	value.FreeBytes = stat.Bfree * uint64(stat.Bsize)
	value.AvailableBytes = stat.Bavail * uint64(stat.Bsize)
	return value
}

// Report returns an immutable snapshot. Database and disk timestamps identify
// exactly when those values were measured; the report can retain stale evidence.
func (s *Service) Report() *model.SystemHealth { s.mu.RLock(); defer s.mu.RUnlock(); return s.report }
func (s *Service) Done() <-chan struct{}       { return s.done }
func (s *Service) Err() error                  { s.mu.RLock(); defer s.mu.RUnlock(); return s.err }
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.started && !s.closed {
		s.closed = true
		close(s.done)
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.closed = true
	s.mu.Unlock()
	select {
	case <-s.done:
		return s.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
