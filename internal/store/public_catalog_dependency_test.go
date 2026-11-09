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
