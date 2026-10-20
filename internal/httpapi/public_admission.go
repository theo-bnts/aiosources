package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type publicRequest uint8

const (
	publicConfig publicRequest = iota
	publicSetup
	publicAddon
)

// All visitor admission state is bounded and volatile. Active requests retain
// their slot when a saved limit decreases; new requests wait for capacity.
type publicAdmission struct {
	mu        sync.Mutex
	windows   map[string]loginWindow
	inflight  int
	nextPrune time.Time
	policy    func(context.Context) (requestPolicy, error)
}

func (a *publicAdmission) reserve(r *http.Request, kind publicRequest) (func(), int, error) {
	if err := r.Context().Err(); err != nil {
		return nil, 0, err
	}
	policy := requestPolicy{limits: model.DefaultPublicLimits()}
	if a.policy != nil {
		var err error
		policy, err = a.policy(r.Context())
		if err != nil {
			return nil, 0, err
		}
	}
	if err := r.Context().Err(); err != nil {
		return nil, 0, err
	}
	address := policy.clients.ClientIP(r)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inflight >= policy.limits.Concurrent {
		return nil, 1, nil
	}
	if kind != publicConfig {
		now := time.Now()
		if a.windows == nil {
			a.windows = make(map[string]loginWindow)
		}
		if !now.Before(a.nextPrune) {
			for key, value := range a.windows {
				if !now.Before(value.until) {
					delete(a.windows, key)
				}
			}
			a.nextPrune = now.Add(time.Second)
		}
		limit := policy.limits.RequestsPerMinute
		key := address
		if kind == publicSetup {
			key += "/setup"
			limit = policy.limits.SetupPerMinute
		}
		value := a.windows[key]
		if !now.Before(value.until) {
			value = loginWindow{until: now.Add(time.Minute)}
		}
		if value.attempts >= limit {
			retry := max(1, int(time.Until(value.until).Seconds())+1)
			return nil, retry, nil
		}
		if len(a.windows) >= 8192 && value.attempts == 0 {
			return nil, 60, nil
		}
		value.attempts++
		a.windows[key] = value
	}
	a.inflight++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.inflight--
			a.mu.Unlock()
		})
	}, 0, nil
}

func (a *publicAdmission) enter(w http.ResponseWriter, r *http.Request, kind publicRequest) (func(), bool) {
	release, retry, err := a.reserve(r, kind)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Public access settings are unavailable. Try again shortly.")
		return nil, false
	}
	if release == nil {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, http.StatusTooManyRequests, "Too many public requests. Try again shortly.")
		return nil, false
	}
	return release, true
}
