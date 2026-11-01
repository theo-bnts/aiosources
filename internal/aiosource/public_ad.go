package aiosource

import (
	"context"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Public checks belong to the visitor's request. Only provider-wide evidence
// may be saved; credentials, account inventory and cleanup jobs stay ephemeral.
func (s *Service) checkPublicAD(ctx context.Context, key string, hashes []string) ([]model.AIOAvailability, error) {
	if err := validCheckHashes(hashes); err != nil {
		return nil, err
	}
	items := []model.AIOAvailability{}
	for _, hash := range hashes {
		var item model.AIOAvailability
		err := s.withHash(ctx, fingerprint(key), hash, func() error {
			var err error
			item, err = s.publicADObservation(ctx, key, hash)
			return err
		})
		if !item.CheckedAt.IsZero() {
			items = append(items, item)
		}
		if err != nil {
			return items, err
		}
	}
	return items, nil
}

func (s *Service) publicADObservation(ctx context.Context, key, hash string) (result model.AIOAvailability, checkErr error) {
	inventory, err := s.Providers.ADInventory(ctx, key)
	if err != nil {
		return result, err
	}
	result.Hash = hash
	var item adMagnet
	if existing := existingAD(inventory, hash); existing != nil {
		if *existing.Status < 0 || *existing.Status > 4 {
			return result, ErrInvalidResponse
		}
		item = *existing
		ready := *item.Status == 4
		item.Ready = &ready
	} else {
		// A bare hash never borrows a source's announce or download URL.
		item, err = s.Providers.ADAdd(ctx, key, hash, hash)
		defer func() {
			if cleanupErr := s.cleanupCachedOnlyMagnet(ctx, key, hash, item, inventory, false); cleanupErr != nil && checkErr == nil {
				checkErr = cleanupErr
			}
		}()
		if err != nil {
			return result, err
		}
	}
	result.Cached = *item.Ready
	if result.Cached {
		result.Files, err = s.Providers.ADFiles(ctx, key, item.ID)
		if err != nil {
			return model.AIOAvailability{}, err
		}
		result.Shareable = createdADMagnet(hash, item, inventory) && len(result.Files) > 0
	}
	result.CheckedAt = time.Now()
	return result, nil
}
