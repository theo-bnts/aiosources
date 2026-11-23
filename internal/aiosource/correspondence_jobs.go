package aiosource

import (
	"context"
	"errors"

	"github.com/theo-bnts/aiosources/internal/model"
)

const correspondenceBatchSize = 50
const correspondenceChunkSize = 10

func (s *Service) drainCorrespondences(ctx context.Context, history *operationHistory) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		scopes, err := s.DB.ClaimCorrespondenceScopes(ctx, correspondenceBatchSize)
		if err != nil || len(scopes) == 0 {
			return err
		}
		for start := 0; start < len(scopes); start += correspondenceChunkSize {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunk := scopes[start:min(start+correspondenceChunkSize, len(scopes))]
			var groups, enriched int
			err := retryIdentificationTransaction(ctx, func() error {
				var err error
				groups, _, enriched, err = s.DB.ProcessCorrespondenceBatch(ctx, chunk, BuildReleaseCorrespondences)
				return err
			})
			if err != nil {
				return err
			}
			if history != nil {
				if groups > 0 {
					history.run.Results["correspondence_groups"] += int64(groups)
				}
				if enriched > 0 {
					history.run.Results["correspondence_enriched"] += int64(enriched)
				}
				history.checkpoint(ctx, "correspondences")
			}
		}
	}
}

func (s *Service) StartCorrespondenceRecompute(ctx context.Context) error {
	if err := s.DB.QueueAllReleaseCorrespondences(ctx); err != nil {
		return err
	}
	err := s.StartMatch()
	if errors.Is(err, model.ErrBusy) {
		s.wakeAutomation()
		return nil
	}
	return err
}

func (s *Service) UnlinkCorrespondenceMember(ctx context.Context, groupID int64, occurrenceID string) (int64, error) {
	return s.DB.UnlinkReleaseCorrespondenceMember(ctx, groupID, occurrenceID, BuildReleaseCorrespondences)
}

func (s *Service) RestoreCorrespondenceExclusion(ctx context.Context, groupID int64, occurrenceID string) (int64, error) {
	return s.DB.RestoreReleaseCorrespondenceExclusion(ctx, groupID, occurrenceID, BuildReleaseCorrespondences)
}
