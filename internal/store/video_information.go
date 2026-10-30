package store

import "context"

// Video maintenance removes its dependent history, reviews and observations
// while preserving the book catalog and its independent data.
func (s *Store) ResetVideoInformation(ctx context.Context) error {
	return s.resetInformation(ctx, "videos")
}
