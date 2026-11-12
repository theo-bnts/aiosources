package store

import "testing"

func TestNewPublicPermissionsEnabledWhilePublicAccessStaysDisabled(t *testing.T) {
	ctx, db := sharingDatabase(t)
	access, err := db.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if access.Enabled || access.UseInstanceDebrid || access.UseInstanceAnnounces || !access.SearchEnabled || !access.TorznabEnabled || !access.StremioEnabled || !access.CatalogEnabled {
		t.Fatal("unsafe or inconsistent fresh access defaults", access)
	}
}
