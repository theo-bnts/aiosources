package store

import (
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPrivateConnectionsHaveNoImplicitPerAccessLimits(t *testing.T) {
	for _, format := range []string{"stremio", "torznab"} {
		t.Run(format, func(t *testing.T) {
			ctx, db := sharingDatabase(t)
			input := model.ShareInput{Format: format, Name: "Example connection", Scope: "all", Enabled: true, Fields: CatalogFields()}
			if format == "stremio" {
				input.StreamPreferences = &model.StreamPreferences{Mode: "all"}
			}
			grant, err := db.CreateShare(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if grant.Share.RequestsPerMinute != 0 || grant.Share.MaxConcurrentDownloads != 0 {
				t.Fatal("unexpected implicit connection limit")
			}
			var active []*CatalogDownload
			defer func() {
				for _, request := range active {
					_ = request.Close()
				}
			}()
			for range 4 {
				request, err := db.beginShareRequest(ctx, grant.Share.ID, grant.Password, format)
				if err != nil {
					t.Fatal("old concurrency default still enforced", err)
				}
				active = append(active, request)
			}
			for range 125 {
				request, err := db.beginShareRequest(ctx, grant.Share.ID, grant.Password, format)
				if err != nil {
					t.Fatal("old rate default still enforced", err)
				}
				if err = request.Close(); err != nil {
					t.Fatal(err)
				}
			}
			input.Revision = grant.Share.Revision
			one, zero := 1, 0
			input.RequestsPerMinute = &zero
			input.MaxConcurrentDownloads = &one
			updated, err := db.UpdateShare(ctx, grant.Share.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			var quota *CatalogQuotaError
			if _, err = db.beginShareRequest(ctx, grant.Share.ID, grant.Password, format); !errors.As(err, &quota) || !quota.Concurrent || quota.RetryAfter < 1 {
				t.Fatal("explicit concurrency limit ignored", err)
			}
			for _, request := range active {
				if err = request.Validate(ctx); !errors.Is(err, ErrCatalogAuthentication) {
					t.Fatal("permission change failed to invalidate an admitted request", err)
				}
				_ = request.Close()
			}
			input.Revision = updated.Revision
			input.RequestsPerMinute = &one
			input.MaxConcurrentDownloads = &zero
			if _, err = db.UpdateShare(ctx, grant.Share.ID, input); err != nil {
				t.Fatal(err)
			}
			quota = nil
			if _, err = db.beginShareRequest(ctx, grant.Share.ID, grant.Password, format); !errors.As(err, &quota) || quota.Concurrent || quota.RetryAfter < 1 {
				t.Fatal("explicit rate limit ignored or unlimited concurrency treated as zero capacity", err)
			}
		})
	}
}
