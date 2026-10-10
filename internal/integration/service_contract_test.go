package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/health"
	"github.com/theo-bnts/aiosources/internal/jobs"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

type backgroundService interface {
	Start(context.Context) error
	Close(context.Context) error
	Done() <-chan struct{}
}

func TestBackgroundServiceLifecycleContract(t *testing.T) {
	for _, name := range []string{"collections", "health"} {
		for _, mode := range []string{"closed before start", "cancelled start", "running"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				ctx, _, db := lifecycleDatabase(t)
				registry, err := providers.New(t.TempDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer registry.Close()
				var service backgroundService
				switch name {
				case "collections":
					service = jobs.New(db, registry, nil, 1)
				case "health":
					service, err = health.New(db, t.TempDir())

				}
				if err != nil {
					t.Fatal(err)
				}
				defer service.Close(ctx)
				if mode == "cancelled start" {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					if err := service.Start(cancelled); !errors.Is(err, context.Canceled) {
						t.Fatalf("cancelled start: %v", err)
					}
					select {
					case <-service.Done():
						t.Fatal("a cancelled start permanently closed an unused service")
					default:
					}
				}
				if mode != "closed before start" {
					if err := service.Start(ctx); err != nil {
						t.Fatal(err)
					}
					if err := service.Start(ctx); !errors.Is(err, model.ErrConflict) {
						t.Fatalf("duplicate start: %v", err)
					}
				}
				for range 2 {
					if err := service.Close(ctx); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case <-service.Done():
				default:
					t.Fatal("Close returned before Done closed")
				}
				if err := service.Start(ctx); !errors.Is(err, model.ErrConflict) {
					t.Fatalf("a stopped service restarted: %v", err)
				}
			})
		}
	}
}
