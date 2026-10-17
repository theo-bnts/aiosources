package aiosource

import (
	"context"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func testPrivateInstallation(t testing.TB, ctx context.Context, s *Service) (string, error) {
	t.Helper()
	grant, err := s.DB.CreateShare(ctx, model.ShareInput{Format: "stremio", Name: "Test installation", Enabled: true, Scope: "all", Fields: store.CatalogFields(), StreamPreferences: &model.StreamPreferences{Mode: "all"}})
	if err != nil {
		t.Fatal(err)
	}
	return "s1." + grant.Share.ID + "." + grant.Password, nil
}

func rotateTestInstallation(t testing.TB, ctx context.Context, s *Service, token string) (string, error) {
	t.Helper()
	id, _, err := PrivateConnectionKey(token)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.DB.Share(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.DB.RotateShare(ctx, id, current.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return "s1." + grant.Share.ID + "." + grant.Password, nil
}

func setTestInstallationPreferences(t testing.TB, ctx context.Context, s *Service, token string, p model.StreamPreferences) {
	t.Helper()
	id, _, err := PrivateConnectionKey(token)
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.DB.Share(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	scope := "all"
	if len(p.Sources) > 0 {
		scope = "selected"
	}
	sources := p.Sources
	p.Sources = nil
	_, err = s.DB.UpdateShare(ctx, id, model.ShareInput{Format: "stremio", Name: current.Name, Enabled: current.Enabled, Scope: scope, SourceIDs: sources, Fields: current.Fields, Revision: current.Revision, RequestsPerMinute: &current.RequestsPerMinute, MaxConcurrentDownloads: &current.MaxConcurrentDownloads, ExpiresAt: current.ExpiresAt, StreamPreferences: &p})
	if err != nil {
		t.Fatal(err)
	}
}

func testPrivateToken(t testing.TB, ctx context.Context, s *Service) string {
	t.Helper()
	token, _ := testPrivateInstallation(t, ctx, s)
	return token
}
