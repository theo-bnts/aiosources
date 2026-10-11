package aiosource

import (
	"context"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type providerHistoryKey struct{}

type providerCallHistory interface {
	recordCall(context.Context, model.ProviderCall)
}

func (h *operationHistory) recordCall(ctx context.Context, call model.ProviderCall) {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = h.service.DB.RecordProviderCall(save, h.run.ID, call.Phase, call.HTTPStatus, call.DurationMS, call.ErrorCode)
}

type playbackCalls struct {
	mu    sync.Mutex
	calls []model.ProviderCall
}

func (h *playbackCalls) recordCall(_ context.Context, call model.ProviderCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) < 32 {
		h.calls = append(h.calls, call)
	}
}

func withProviderHistory(ctx context.Context, h *operationHistory) context.Context {
	return context.WithValue(ctx, providerHistoryKey{}, h)
}
func recordProviderCall(ctx context.Context, path string, status int, started time.Time, err error) {
	h, ok := ctx.Value(providerHistoryKey{}).(providerCallHistory)
	if !ok {
		return
	}
	phase := "provider_request"
	switch path {
	case "torrents/checkcached":
		phase = "provider_availability"
	case "v4.1/magnet/status":
		phase = "provider_inventory"
	case "v4/magnet/upload":
		phase = "provider_add"
	case "v4/magnet/files":
		phase = "provider_files"
	case "v4/magnet/delete":
		phase = "provider_cleanup"
	case "v4/link/unlock":
		phase = "provider_unlock"
	case "torrents/createtorrent":
		phase = "provider_add"
	case "torrents/requestdl":
		phase = "provider_playback"
	}
	code := providerFailureCode(err)
	h.recordCall(ctx, model.ProviderCall{Phase: phase, HTTPStatus: status, DurationMS: time.Since(started).Milliseconds(), ErrorCode: code})
}
