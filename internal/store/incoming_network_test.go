package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestIncomingTrustAndPublicLimitsPreserveExistingConnectionsAndGenerations(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	network, err := db.NetworkSettings(ctx)
	if err != nil || network.Incoming == nil || network.Incoming.Enabled {
		t.Fatal("upgrade trusted headers automatically", err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || access.Limits != model.DefaultPublicLimits() {
		t.Fatal("public defaults differ from 12, 120 and 50", err)
	}
	old := access
	access.Limits = model.PublicLimits{SetupPerMinute: 3, RequestsPerMinute: 10, Concurrent: 2}
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if err = db.SetPublicAccess(ctx, access); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale limit update was accepted", err)
	}
	saved, _ := db.PublicAccess(ctx)
	if saved.Generation != old.Generation || saved.TorznabGeneration != old.TorznabGeneration || saved.Enabled != old.Enabled || saved.Limits != access.Limits {
		t.Fatal("changing quotas revoked installations or changed exposure")
	}
	for _, limits := range []model.PublicLimits{{SetupPerMinute: -1, RequestsPerMinute: 10, Concurrent: 2}, {SetupPerMinute: 1, RequestsPerMinute: 1}, {SetupPerMinute: 1, RequestsPerMinute: 1, Concurrent: 1001}} {
		saved.Limits = limits
		if err := db.SetPublicAccess(ctx, saved); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid limits were accepted", err)
		}
	}
	saved.Limits = model.PublicLimits{} // Legacy callers must preserve configured quotas.
	if err = db.SetPublicAccess(ctx, saved); err != nil {
		t.Fatal(err)
	}
	latest, _ := db.PublicAccess(ctx)
	if latest.Limits != access.Limits {
		t.Fatal("omitted limits reset the configured policy")
	}
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_limits_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN IF NEW.kind='security.public_limits_changed' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER reject_limits_audit BEFORE INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.reject_limits_audit()`); err != nil {
		t.Fatal(err)
	}
	failed := latest
	failed.Limits.Concurrent++
	if err := db.SetPublicAccess(ctx, failed); err == nil {
		t.Fatal("quota change committed without its audit event")
	}
	afterFailure, _ := db.PublicAccess(ctx)
	if afterFailure != latest {
		t.Fatal("failed quota save changed live limits or link generations")
	}
	network.Incoming = &model.IncomingNetwork{Enabled: true, IPHeader: "CF-Connecting-IP", Format: "single", TrustedProxies: []string{"10.0.0.2", "2001:db8::/32"}}
	if err = db.UpdateNetworkSettings(ctx, network); err != nil {
		t.Fatal(err)
	}
	savedNetwork, _ := db.NetworkSettings(ctx)
	if !reflect.DeepEqual(network.Incoming, savedNetwork.Incoming) || savedNetwork.Default != network.Default {
		t.Fatal("incoming trust changed outgoing routing")
	}
	previousIncoming := savedNetwork.Incoming
	savedNetwork.Incoming = nil
	if err := db.UpdateNetworkSettings(ctx, savedNetwork); err != nil {
		t.Fatal(err)
	}
	savedNetwork, _ = db.NetworkSettings(ctx)
	if !reflect.DeepEqual(previousIncoming, savedNetwork.Incoming) {
		t.Fatal("an old outgoing editor removed incoming trust")
	}
	savedNetwork.Incoming.Enabled = false
	savedNetwork.Incoming.TrustedProxies = nil
	if err := db.UpdateNetworkSettings(ctx, savedNetwork); err != nil {
		t.Fatal(err)
	}
	savedNetwork, _ = db.NetworkSettings(ctx)
	if savedNetwork.Incoming.TrustedProxies == nil {
		t.Fatal("a disabled API configuration returned null instead of the address list expected by the editor")
	}
	var logs int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='security.public_limits_changed' AND message='Public request limits changed' AND data='{}'::jsonb`).Scan(&logs); err != nil || logs != 1 {
		t.Fatal("quota audit event missing, duplicated or contains private data", err)
	}
}

func TestIncomingNetworkMigrationPreservesEnabledVisitorLinksAndOutgoingRoutes(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	// Recreate the preceding schema without modifying any earlier migration.
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.network_settings DROP COLUMN incoming;
ALTER TABLE ingest.aio_public_access DROP COLUMN setup_per_minute, DROP COLUMN requests_per_minute, DROP COLUMN concurrent;
DROP FUNCTION ingest.access_activity_message(TEXT);
ALTER FUNCTION ingest.access_activity_message_before_public_limits(TEXT) RENAME TO access_activity_message;
DELETE FROM ingest.schema_migrations WHERE version=65;
UPDATE ingest.aio_public_access SET enabled=true,torznab_enabled=true,generation=42,torznab_generation=17;
UPDATE ingest.network_settings SET default_route='direct';`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || !access.Enabled || !access.TorznabEnabled || access.Generation != 42 || access.TorznabGeneration != 17 || access.Limits != model.DefaultPublicLimits() {
		t.Fatal("upgrade revoked or changed existing public access", err)
	}
	network, err := db.NetworkSettings(ctx)
	if err != nil || network.Default != model.NetworkDirect || network.Incoming.Enabled {
		t.Fatal("upgrade changed outgoing routing or trusted headers", err)
	}
}
