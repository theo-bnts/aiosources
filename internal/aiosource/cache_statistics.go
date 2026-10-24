package aiosource

import (
	"context"
	"github.com/theo-bnts/aiosources/internal/model"
	"time"
)

func (s *Service) CacheStatistics(ctx context.Context) ([]model.SharedCacheStats, error) {
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	after := time.Now().Add(-time.Duration(config.CacheMinutes) * time.Minute)
	items, err := s.DB.SharedAvailabilityStats(ctx, after)
	if err != nil {
		return nil, err
	}
	for _, provider := range model.DebridProviders() {
		secret := config.DebridSecret(provider.ID)
		if secret == "" {
			continue
		}
		key, err := s.key(ctx, secret)
		if err != nil {
			return nil, err
		}
		checks, err := s.DB.AvailabilityCheckStats(ctx, provider.ID, fingerprint(key), after)
		if err != nil {
			return nil, err
		}
		found := false
		for i := range items {
			if items[i].Service == provider.ID {
				items[i].Checks = &checks
				found = true
				break
			}
		}
		if !found {
			items = append(items, model.SharedCacheStats{Service: provider.ID, Checks: &checks})
		}
	}
	return items, nil
}
