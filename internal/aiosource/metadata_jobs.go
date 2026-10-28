package aiosource

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/theo-bnts/aiosources/internal/store"
)

type metadataProgress struct {
	saved, deferred, unavailable int
	firstDeferred                error
}

func (s *Service) nextMetadataJobs(ctx context.Context, recovering bool) ([]store.AIOJob, error) {
	if recovering {
		if err := s.DB.QueueAIOMetadataRecovery(ctx); err != nil {
			return nil, metadataStorageError(err)
		}
	}
	jobs, err := s.DB.AIOMetadataJobs(ctx)
	return jobs, metadataStorageError(err)
}

// HTTP requests overlap, while one publisher commits small batches. Neither
// in-flight requests nor results can grow with the size of the catalog.
func (s *Service) processMetadataJobs(ctx context.Context, key string, jobs []store.AIOJob, history *operationHistory, progress *metadataProgress, autoMatch bool) error {
	policy, err := s.DB.RuntimeValues(ctx, "metadata")
	if err != nil {
		return metadataStorageError(err)
	}
	workerCount := min(policy["workers"], len(jobs))
	work, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	type result struct {
		job         store.AIOJob
		publication *store.AIOMetadataPublication
		err         error
	}
	input := make(chan store.AIOJob, len(jobs))
	for _, job := range jobs {
		input <- job
	}
	close(input)
	output := make(chan result, workerCount)
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range input {
				if work.Err() != nil {
					return
				}
				publication, err := s.loadDetails(work, key, job)
				select {
				case output <- result{job, publication, err}:
				case <-work.Done():
					return
				}
			}
		}()
	}
	batch := []store.AIOMetadataPublication{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.DB.SaveAIOMetadataBatch(ctx, batch); err != nil {
			return metadataStorageError(err)
		}
		progress.saved += len(batch)
		history.run.Processed += int64(len(batch))
		batch = batch[:0]
		history.checkpoint(ctx, "details")
		if autoMatch {
			// Identification owns its own worker and cancellation, so a large
			// local backlog never blocks fetching the next information batch.
			s.queueAutomaticMatching(ctx)
		}
		return nil
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for remaining := len(jobs); remaining > 0; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		case result := <-output:
			remaining--
			if result.err != nil {
				var provider *metadataError
				if errors.As(result.err, &provider) && provider.retryAt.After(time.Now()) {
					if err := flush(); err != nil {
						return err
					}
					return result.err
				}
				if errors.Is(result.err, ErrAuth) || errors.Is(result.err, errMetadataStorage) {
					return result.err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := s.DB.FinishAIOMetadata(ctx, result.job, false); err != nil {
					return metadataStorageError(err)
				}
				history.run.Processed++
				history.run.Errors++
				progress.deferred++
				if progress.firstDeferred == nil {
					progress.firstDeferred = result.err
				}
				if !errors.Is(result.err, ErrInvalidResponse) {
					progress.unavailable++
				}
				// Preserve the queue during an outage rather than issuing a
				// timeout for every remaining entry in a million-row catalog.
				if progress.unavailable >= 3 || progress.deferred >= 50 {
					if err := flush(); err != nil {
						return err
					}
					return result.err
				}
			} else {
				progress.unavailable = 0
				if result.publication == nil {
					if err := s.DB.DeleteAIOMetadataJob(ctx, result.job); err != nil {
						return metadataStorageError(err)
					}
					progress.saved++
					history.run.Processed++
				} else {
					batch = append(batch, *result.publication)
				}
			}
			if len(batch) >= policy["publication_batch"] || remaining == 0 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	// Deletions and isolated invalid responses may leave no publication batch.
	history.checkpoint(ctx, "details")
	if autoMatch {
		s.queueAutomaticMatching(ctx)
	}
	return nil
}

func (s *Service) queueAutomaticMatching(ctx context.Context) {
	_, running, _ := s.State()
	if running || ctx.Err() != nil {
		return
	}
	s.wakeAutomation()
}
