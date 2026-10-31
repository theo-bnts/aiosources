package aiosource

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicSharingCredentialsAreStatelessScopedAndIndependentlyRevocable(t *testing.T) {
	ctx, s, conn := testService(t)
	const audience = "https://catalog.example"
	if _, _, err := s.CreatePublicCatalog(ctx, audience, model.PublicCatalogOptions{}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("disabled setup accepted", err)
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	options := model.PublicCatalogOptions{Sources: []string{"beta", "alpha"}, Fields: []string{"size", "title"}, CatalogEntities: model.CatalogEntities{Languages: true}}
	configuration, password, err := s.CreatePublicCatalog(ctx, audience, options)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.OpenPublicCatalog(ctx, audience, configuration, password)
	if err != nil || len(grant.Sources) != 2 || grant.Sources[0] != "alpha" || len(grant.Fields) != 2 || grant.Fields[0] != "title" || !grant.CatalogEntities.Languages {
		t.Fatal("configuration did not round trip", err)
	}
	changed, _ := EncodePublicCatalogOptions(model.PublicCatalogOptions{})
	for _, sample := range []struct{ audience, configuration, password string }{
		{"https://other.example", configuration, password},
		{audience, changed, password},
		{audience, configuration, password + "!"},
		{audience, configuration, configuration},
	} {
		if _, err = s.OpenPublicCatalog(ctx, sample.audience, sample.configuration, sample.password); !errors.Is(err, ErrAuth) {
			t.Fatal("credential binding bypassed", err)
		}
	}
	if _, err = s.OpenPublicInstallation(ctx, audience, password); !errors.Is(err, ErrAuth) {
		t.Fatal("catalog password accepted as a playback credential", err)
	}
	if _, err = s.OpenPublicTorznab(ctx, audience, password); !errors.Is(err, ErrAuth) {
		t.Fatal("catalog password accepted as a Torznab credential", err)
	}
	var rows int
	if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.shares)+(SELECT count(*) FROM ingest.share_requests)+(SELECT count(*) FROM ingest.secrets)`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("visitor configuration persisted", rows, err)
	}
	// Other connection switches do not invalidate catalog credentials.
	access, _ = s.DB.PublicAccess(ctx)
	access.StremioEnabled = !access.StremioEnabled
	access.TorznabEnabled = true
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenPublicCatalog(ctx, audience, configuration, password); err != nil {
		t.Fatal("unrelated gate revoked sharing", err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.CatalogEnabled = false
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.CatalogEnabled = true
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OpenPublicCatalog(ctx, audience, configuration, password); !errors.Is(err, ErrAuth) {
		t.Fatal("re-enabling restored an old password", err)
	}
}

func TestPublicSharingOptionsRejectCredentialFieldsAndAmbiguousEncodings(t *testing.T) {
	for _, raw := range []string{
		`{"announces":{"alpha":"https://private.invalid/announce"}}`,
		`{"services":{"torbox":"private-key"}}`,
		`{"fields":["download_url"]}`,
		`{"sources":["https://private.invalid/?apikey=never-export"]}`,
		`{"sources":["one","one"]}`,
		`{"fields":["title","title"]}`,
		`{"sources":["beta","alpha"]}`,
		`{} {}`,
	} {
		if _, err := DecodePublicCatalogOptions(base64.RawURLEncoding.EncodeToString([]byte(raw))); err == nil {
			t.Fatal("unsafe or noncanonical options accepted")
		}
	}
	if _, err := DecodePublicCatalogOptions(strings.Repeat("a", MaxPublicCatalogSize+1)); err == nil {
		t.Fatal("oversized options accepted")
	}
}
