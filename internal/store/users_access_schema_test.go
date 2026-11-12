package store

import (
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestAccessUpgradeRemovesFactorsAndPreservesSessionsAndPermissions(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 96); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE aiosources.admin_security SET password_hash=decode('010203','hex'),active_secret=decode('040506','hex'),recovery_hashes=ARRAY['obsolete-factor-hash'];
 INSERT INTO aiosources.admin_sessions(id,token_hash,user_agent,expires_at) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',decode(repeat('42',32),'hex'),'Other browser',now()+interval '1 hour');
 UPDATE aiosources.aio_public_access SET revision=3,enabled=false,search_enabled=false,torznab_enabled=true,stremio_enabled=false,catalog_enabled=false;
 INSERT INTO aiosources.trusted_users(id,email,password_hash,permissions) VALUES('preserved-user','reader@example.invalid','opaque-hash','{"search_enabled":true,"catalog_enabled":false,"expose_sources":false}')`); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err = db.pool.QueryRow(ctx, `SELECT array_agg(column_name ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='aiosources' AND table_name='admin_security'`).Scan(&names); err != nil || strings.Join(names, ",") != "singleton,password_hash" {
		t.Fatal("retired factors remain", names, err)
	}
	var password []byte
	if err = db.pool.QueryRow(ctx, `SELECT password_hash FROM aiosources.admin_security`).Scan(&password); err != nil || string(password) != string([]byte{1, 2, 3}) {
		t.Fatal("password changed", err)
	}
	sessions, err := db.ListAdminSessions(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(sessions) != 1 || !sessions[0].Current || time.Until(sessions[0].ExpiresAt) < 59*time.Minute {
		t.Fatal("session lost", err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || access.Enabled || access.SearchEnabled || !access.TorznabEnabled || access.StremioEnabled || access.CatalogEnabled || access.UseInstanceDebrid || access.UseInstanceAnnounces {
		t.Fatal("existing access choices changed", access, err)
	}
	var permissions string
	if err = db.pool.QueryRow(ctx, `SELECT permissions::text FROM aiosources.trusted_users WHERE id='preserved-user'`).Scan(&permissions); err != nil || strings.Contains(permissions, "expose_sources") || !strings.Contains(permissions, `"search_enabled": true`) {
		t.Fatal("trusted permissions lost", err)
	}
	policy, err := db.RuntimeSection(ctx, "security")
	if _, exists := policy.Values["enrollment_minutes"]; err != nil || exists {
		t.Fatal("retired setup setting remains", err)
	}
	// Re-running migrations must not change persisted choices or valid sessions.
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedCatalogTracksChangesWithoutPublicAccess(t *testing.T) {
	ctx, db := sharingDatabase(t)
	user, err := db.SaveTrustedUser(ctx, model.TrustedUser{Email: "reader@example.invalid", Enabled: true, TrustedPermissions: model.TrustedPermissions{CatalogEnabled: true}}, "opaque-hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('example','one','{"title":"Example title"}')`); err != nil {
		t.Fatal(err)
	}
	grant := model.PublicCatalogGrant{Trusted: &model.TrustedGrant{UserID: user.ID, Generation: user.Generation}}
	first, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{})
	if err != nil || len(first.Items) != 1 || first.Checkpoint == "" {
		t.Fatal("trusted snapshot unavailable", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE aiosources.torrents SET fields='{"title":"Updated example"}' WHERE provider_id='example'`); err != nil {
		t.Fatal(err)
	}
	changes, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Checkpoint: first.Checkpoint})
	if err != nil || changes.Mode != "incremental" || len(changes.Items) != 1 || changes.Items[0].Fields["title"] != "Updated example" {
		t.Fatal("trusted-only sync lost a change", changes, err)
	}
	user.Enabled = false
	if _, err = db.SaveTrustedUser(ctx, user, ""); err != nil {
		t.Fatal(err)
	}
	var tracking bool
	if err = db.pool.QueryRow(ctx, `SELECT enabled FROM aiosources.catalog_tracking`).Scan(&tracking); err != nil || tracking {
		t.Fatal("revoked trusted catalog retained tracking", err)
	}
}
