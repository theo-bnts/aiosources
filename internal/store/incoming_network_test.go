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
	for _, limits := range []model.PublicLimits{{SetupPerMinute: -1, RequestsPerMinute: 10, Concurrent: 2}, {SetupPerMinute: 1, RequestsPerMinute: 1, Concurrent: -1}, {SetupPerMinute: 1, RequestsPerMinute: 1, Concurrent: 1001}} {
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
	if _, err = db.pool.Exec(ctx, `CREATE FUNCTION aiosources.reject_network_audit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
 BEGIN IF NEW.kind='network.settings_changed' THEN RAISE EXCEPTION 'synthetic audit failure'; END IF; RETURN NEW; END; $$;
 CREATE TRIGGER reject_network_audit BEFORE INSERT ON aiosources.activity_logs FOR EACH ROW EXECUTE FUNCTION aiosources.reject_network_audit()`); err != nil {
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
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.activity_logs WHERE kind='network.settings_changed' AND service='network' AND data='{}'::jsonb`).Scan(&logs); err != nil || logs != 4 {
		t.Fatal("network audit events missing, duplicated or contain unrelated data", logs, err)
	}
}

func TestPublicLimitsCanBeDisabledIndividuallyWithoutChangingOtherNetworkSettings(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	for _, limits := range []model.PublicLimits{
		{RequestsPerMinute: 120, Concurrent: 50},
		{SetupPerMinute: 12, Concurrent: 50},
		{SetupPerMinute: 12, RequestsPerMinute: 120},
		{},
	} {
		before, err := db.NetworkSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		updated := before
		updated.PublicLimits = &limits
		if err = db.UpdateNetworkSettings(ctx, updated); err != nil {
			t.Fatal("disabled request limit could not be saved", err)
		}
		after, err := db.NetworkSettings(ctx)
		if err != nil || *after.PublicLimits != limits || !reflect.DeepEqual(after.Incoming, before.Incoming) || after.Default != before.Default {
			t.Fatal("disabled limits were changed or affected unrelated network settings", err)
		}
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.network_settings SET public_limits=jsonb_set(public_limits,'{requests_per_minute}','-1') WHERE singleton`); err == nil {
		t.Fatal("database constraint accepted a negative request limit")
	}
}
