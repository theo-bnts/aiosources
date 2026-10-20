package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestIncomingTrustAndPublicLimitsPreserveExistingConnectionsAndGenerations(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	network, err := db.NetworkSettings(ctx)
	if err != nil || network.Incoming == nil || network.Incoming.Enabled || network.PublicLimits == nil || *network.PublicLimits != model.DefaultPublicLimits() {
		t.Fatal("network defaults changed", err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	network.PublicLimits = &model.PublicLimits{SetupPerMinute: 3, RequestsPerMinute: 10, Concurrent: 2}
	if err = db.UpdateNetworkSettings(ctx, network); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateNetworkSettings(ctx, network); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale quota update was accepted", err)
	}
	after, _ := db.PublicAccess(ctx)
	if after != access {
		t.Fatal("changing quotas changed public exposure or installation generations")
	}
	// An unrelated exposure form, opened before the quota update, owns no limits.
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	saved, _ := db.NetworkSettings(ctx)
	if *saved.PublicLimits != *network.PublicLimits {
		t.Fatal("public access save overwrote network quotas")
	}
	for _, limits := range []model.PublicLimits{{SetupPerMinute: -1, RequestsPerMinute: 10, Concurrent: 2}, {SetupPerMinute: 1, RequestsPerMinute: 1}, {SetupPerMinute: 1, RequestsPerMinute: 1, Concurrent: 1001}} {
		invalid := saved
		invalid.PublicLimits = &limits
		if err := db.UpdateNetworkSettings(ctx, invalid); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid limits were accepted", err)
		}
	}
	// Omitted fields in an older outgoing-only client preserve incoming policies.
	saved.Incoming = &model.IncomingNetwork{Enabled: true, IPHeader: "CF-Connecting-IP", Format: "single", TrustedProxies: []string{"10.0.0.2", "2001:db8::/32"}}
	if err = db.UpdateNetworkSettings(ctx, saved); err != nil {
		t.Fatal(err)
	}
	previous := saved.Incoming
	saved, _ = db.NetworkSettings(ctx)
	saved.Incoming = nil
	saved.PublicLimits = nil
	if err = db.UpdateNetworkSettings(ctx, saved); err != nil {
		t.Fatal(err)
	}
	saved, _ = db.NetworkSettings(ctx)
	if !reflect.DeepEqual(previous, saved.Incoming) || *saved.PublicLimits != *network.PublicLimits || saved.Default != network.Default {
		t.Fatal("unrelated save changed incoming trust, quotas or outgoing routing")
	}
	saved.Incoming.Enabled = false
	saved.Incoming.TrustedProxies = nil
	if err = db.UpdateNetworkSettings(ctx, saved); err != nil {
		t.Fatal(err)
	}
	saved, _ = db.NetworkSettings(ctx)
	if saved.Incoming.TrustedProxies == nil {
		t.Fatal("disabled policy returned null instead of an address list")
	}
	if _, err = db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_network_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
 BEGIN IF NEW.kind='network.settings_changed' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END; $$;
 CREATE TRIGGER reject_network_audit BEFORE INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.reject_network_audit()`); err != nil {
		t.Fatal(err)
	}
	failed := saved
	limits := *saved.PublicLimits
	limits.Concurrent++
	failed.PublicLimits = &limits
	if err = db.UpdateNetworkSettings(ctx, failed); err == nil {
		t.Fatal("quota update committed without its network audit event")
	}
	afterFailure, _ := db.NetworkSettings(ctx)
	if !reflect.DeepEqual(saved, afterFailure) {
		t.Fatal("failed quota save changed the live policy")
	}
	var logs int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='network.settings_changed' AND service='network' AND data='{}'::jsonb`).Scan(&logs); err != nil || logs != 4 {
		t.Fatal("network audit events missing, duplicated or contain unrelated data", logs, err)
	}
}

func TestIncomingNetworkMigrationPreservesEnabledVisitorLinksAndOutgoingRoutes(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 64); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_public_access SET enabled=true,torznab_enabled=true,generation=42,torznab_generation=17;
 UPDATE ingest.network_settings SET default_route='direct';`); err != nil {
		t.Fatal(err)
	}
	if err = db.migrateThrough(ctx, 65); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.aio_public_access SET setup_per_minute=19, requests_per_minute=321,concurrent=61;`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, networkLimitsSchema); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var quota int
	if err = db.pool.QueryRow(ctx, `SELECT requests_per_minute FROM ingest.aio_public_access WHERE singleton`).Scan(&quota); err != nil || quota != 321 {
		t.Fatal("interrupted migration lost saved quotas", err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || !access.Enabled || !access.TorznabEnabled || access.Generation != 42 || access.TorznabGeneration != 17 {
		t.Fatal("upgrade revoked or changed existing public access", err)
	}
	network, err := db.NetworkSettings(ctx)
	want := model.PublicLimits{SetupPerMinute: 19, RequestsPerMinute: 321, Concurrent: 61}
	if err != nil || network.Default != model.NetworkDirect || network.Incoming.Enabled || network.PublicLimits == nil || *network.PublicLimits != want {
		t.Fatal("upgrade changed routing, trusted headers or saved quotas", err)
	}
	revision := network.Revision
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	network, _ = db.NetworkSettings(ctx)
	if network.Revision != revision {
		t.Fatal("repeated startup rewrote settings")
	}
}
