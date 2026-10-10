package aiosource

import (
	"context"
	"errors"
	"sync"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Overlap slow upstream responses without increasing the shared provider rate.
// Each hash retains its own fresh ownership check and durable cleanup record.
func (s *Service) checkADResource(ctx context.Context, key string, hashes []string, generation int64, history *operationHistory) error {
	work, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	input := make(chan string)
	type result struct {
		availability model.AIOAvailability
		err          error
	}
	output := make(chan result, 4)
	workers.Add(1)
	go func() {
		defer workers.Done()
		defer close(input)
		for _, hash := range hashes {
			select {
			case input <- hash:
			case <-work.Done():
				return
			}
		}
	}()
	for range min(4, len(hashes)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for hash := range input {
				availability, err := s.adCheck(work, key, hash)
				select {
				case output <- result{availability, err}:
				case <-work.Done():
					return
				}
			}
		}()
	}
	var failure error
	for range hashes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-output:
			if result.err != nil {
				if err := ctx.Err(); err != nil {
					return err
				}
				history.run.Errors++
				failure = result.err
				if errors.Is(failure, ErrAuth) {
					return failure
				}
				continue
			}
			result.availability.Generation = generation
			if err := s.DB.SaveAIOAvailability(ctx, "alldebrid", fingerprint(key), result.availability); err != nil {
				return err
			}
			history.run.Processed++
			if result.availability.Cached {
				history.run.Results["cached"]++
			} else {
				history.run.Results["uncached"]++
			}
			if history.run.Processed%100 == 0 {
				history.checkpoint(ctx, "availability")
			}
		}
	}
	return failure
}
