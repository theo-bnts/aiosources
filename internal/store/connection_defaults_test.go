package store

import (
	"context"
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
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

func TestConnectionDefaultsUpgradePreservesNamedAccessAndExplicitPolicies(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.migrateThrough(ctx, 57); err != nil {
		t.Fatal(err)
	}
	requests, concurrent := 17, 3
	grant, err := db.CreateShare(ctx, model.ShareInput{Format: "stremio", Name: "Existing connection", Enabled: true, Scope: "all", Fields: CatalogFields(), RequestsPerMinute: &requests, MaxConcurrentDownloads: &concurrent, StreamPreferences: &model.StreamPreferences{Mode: "all", CachedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.secrets(name,ciphertext) VALUES('aiosource_installation','obsolete credential'::bytea)`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := db.StremioConnection(ctx, grant.Share.ID, grant.Password)
	if err != nil || current.RequestsPerMinute != requests || current.MaxConcurrentDownloads != concurrent || !current.StreamPreferences.CachedOnly {
		t.Fatal("upgrade changed a saved policy or invalidated a named key", err)
	}
	var retained bool
	if err = db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.secrets WHERE name='aiosource_installation')`).Scan(&retained); err != nil || retained {
		t.Fatal("obsolete global key retained", err)
	}
	if _, err = db.CreateShare(context.Background(), model.ShareInput{Name: "Catalog", Scope: "all", Fields: CatalogFields(), RequestsPerMinute: new(0)}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("catalog limit policy widened", err)
	}
}
