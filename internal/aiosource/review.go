package aiosource

import (
	"context"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (s *Service) ReviewCandidates(ctx context.Context, query, kind string) ([]model.AIOWork, error) {
	query = strings.TrimSpace(query)
	return s.DB.AIOReviewCandidates(ctx, query, Normalize(query), kind)
}

func (s *Service) SaveReview(ctx context.Context, occurrence string, input model.AIOReviewInput) error {
	if err := s.DB.SaveAIOReview(ctx, occurrence, input); err != nil {
		return err
	}
	s.wakeAutomation()
	return nil
}
