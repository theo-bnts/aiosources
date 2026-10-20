package httpapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRequestPolicyCachesConcurrentReadsAndInvalidatesSavedPolicies(t *testing.T) {
	loads := 0
	failed := false
	c := &requestPolicyCache{load: func(context.Context) (requestPolicy, error) {
		loads++
		if failed {
			return requestPolicy{}, errors.New("settings unavailable")
		}
		return requestPolicy{limits: model.DefaultPublicLimits()}, nil
	}}
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			policy, err := c.get(context.Background())
			if err != nil || policy.limits != model.DefaultPublicLimits() {
				t.Error("inconsistent policy")
			}
		})
	}
	wg.Wait()
	if loads != 1 {
		t.Fatal("anonymous requests read settings for every call", loads)
	}
	failed = true
	c.invalidate()
	if _, err := c.get(context.Background()); err == nil {
		t.Fatal("failed refresh reused previously trusted proxy settings")
	}
	if _, err := c.get(context.Background()); err == nil || loads != 2 {
		t.Fatal("failures are not bounded and cached")
	}
	failed = false
	c.invalidate()
	if _, err := c.get(context.Background()); err != nil || loads != 3 {
		t.Fatal("policy did not recover after invalidation")
	}
}
