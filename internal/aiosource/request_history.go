package aiosource

import (
	"context"
	"log/slog"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func newStremioHistory(mediaType, id string) model.StremioRequest {
	item := model.StremioRequest{CreatedAt: time.Now(), MediaType: mediaType, Resource: "Invalid resource", Outcome: "invalid", Results: []model.StremioResult{}}
	if mediaType != "movie" && mediaType != "series" && mediaType != "anime" {
		item.MediaType = "invalid"
	}
	if r, err := ParseID(id, mediaType); err == nil {
		item.Resource = r.Kind + ":" + r.ID
		if r.Season != nil {
			item.Resource += ":" + itoa64(int64(*r.Season))
		}
		if r.Episode != nil {
			item.Resource += ":" + itoa64(int64(*r.Episode))
		}
		item.Outcome = "empty"
	}
	return item
}

func (s *Service) saveStremioHistory(item model.StremioRequest) {
	save, cancel := context.WithTimeout(s.ctx, time.Second)
	defer cancel()
	if err := s.DB.RecordStremioRequest(save, item); err != nil && s.ctx.Err() == nil {
		slog.Warn("Could not save Stremio request history")
	}
}

// Called only after installation-token authorization. Rejected resources are
// normalized or replaced by a fixed label, never persisted as request URLs.
func (s *Service) RecordRejectedStream(mediaType, id string, busy bool) {
	item := newStremioHistory(mediaType, id)
	item.Outcome = "invalid"
	if busy {
		item.Outcome = "busy"
	}
	s.saveStremioHistory(item)
}
