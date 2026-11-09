package store

import "testing"

func TestPublicSourceExposureRevocationDisablesSharingUntilExplicitlyEnabled(t *testing.T) {
	ctx, db := sharingDatabase(t)
	access, _ := db.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	before := access.CatalogGeneration
	access.ExposeSources = false
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	if access.CatalogEnabled || access.CatalogGeneration <= before {
		t.Fatal("catalog permission remained active")
	}
	access.ExposeSources = true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	if access.CatalogEnabled {
		t.Fatal("source exposure silently enabled sharing again")
	}
}

func TestPublicCatalogDependencyUpgradeNormalizesOldDisabledSwitches(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `ALTER TABLE aiosources.aio_public_access DROP CONSTRAINT aio_public_catalog_requires_sources;
DELETE FROM aiosources.schema_migrations WHERE version=86;
UPDATE aiosources.aio_public_access SET expose_sources=FALSE,catalog_enabled=TRUE;`); err != nil {
		t.Fatal(err)
	}
	before, _ := db.PublicAccess(ctx)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := db.PublicAccess(ctx)
	if after.CatalogEnabled || after.Revision != before.Revision+1 || after.CatalogGeneration != before.CatalogGeneration+1 {
		t.Fatal("old invalid combination survived the upgrade")
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := db.PublicAccess(ctx)
	if unchanged != after {
		t.Fatal("dependency migration repeated")
	}
}
