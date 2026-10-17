package httpapi

import (
	"context"
	"testing"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func testPrivateInstallation(t testing.TB, ctx context.Context, s *aiosource.Service) (string, error) {
	t.Helper()
	grant, err := s.DB.CreateShare(ctx, model.ShareInput{Format: "stremio", Name: "Test installation", Enabled: true, Scope: "all", Fields: store.CatalogFields(), StreamPreferences: &model.StreamPreferences{Mode: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	return "s1." + grant.Share.ID + "." + grant.Password, nil
}
