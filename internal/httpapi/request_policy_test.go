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

func TestCancelledVisitorCannotPoisonSharedRequestPolicy(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	loads := 0
	c := &requestPolicyCache{load: func(ctx context.Context) (requestPolicy, error) {
		loads++
		close(started)
		<-finish
		if err := ctx.Err(); err != nil {
			return requestPolicy{}, err
		}
		return requestPolicy{limits: model.DefaultPublicLimits()}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _, _ = c.get(ctx); close(done) }()
	<-started
	cancel()
	close(finish)
	<-done
	policy, err := c.get(context.Background())
	if err != nil || policy.limits != model.DefaultPublicLimits() || loads != 1 {
		t.Fatal("one visitor cancellation made healthy requests unavailable", err, loads)
	}
}
