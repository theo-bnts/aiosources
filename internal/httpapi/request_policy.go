package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/inbound"
	"github.com/theo-bnts/aiosources/internal/model"
)

type requestPolicy struct {
	clients inbound.Resolver
	limits  model.PublicLimits
}

// A short, bounded cache prevents anonymous requests (including rejected ones)
// from querying settings tables on every call. Failed reads fail admission
// closed. Administrator saves invalidate it immediately on this process.
type requestPolicyCache struct {
	mu    sync.Mutex
	until time.Time
	value requestPolicy
	err   error
	load  func(context.Context) (requestPolicy, error)
}

func (c *requestPolicyCache) get(ctx context.Context) (requestPolicy, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.until) {
		return c.value, c.err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c.value, c.err = c.load(ctx)
	ttl := time.Second
	if c.err != nil {
		ttl = 250 * time.Millisecond
	}
	c.until = time.Now().Add(ttl)
	return c.value, c.err
}

func (c *requestPolicyCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.until = time.Time{}
	c.mu.Unlock()
}

func (s *server) initRequestPolicy() {
	c := &requestPolicyCache{load: func(ctx context.Context) (requestPolicy, error) {
		network, err := s.options.Store.NetworkSettings(ctx)
		if err != nil {
			return requestPolicy{}, err
		}
		resolver, err := inbound.New(network.Incoming)
		if err != nil {
			return requestPolicy{}, err
		}
		access, err := s.options.Store.PublicAccess(ctx)
		return requestPolicy{clients: resolver, limits: access.Limits}, err
	}}
	s.policies = c
	s.public = &publicAdmission{policy: c.get}
	s.auth.clientAddress = func(r *http.Request) string {
		policy, err := c.get(r.Context())
		if err != nil {
			return inbound.Direct(r)
		}
		return policy.clients.ClientIP(r)
	}
}
