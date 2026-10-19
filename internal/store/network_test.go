package store

import (
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestNetworkSettingsRevisionsSecretsDeletionAndAtomicLogs(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	settings, err := db.NetworkSettings(ctx)
	if err != nil || settings.Revision != 1 || settings.Default != model.NetworkSystem || len(settings.Proxies) != 0 {
		t.Fatal("Existing installations must keep their system connection by default")
	}
	settings.Proxies = []model.NetworkProxy{{ID: "warp", Name: "WARP", URL: "socks5://127.0.0.1:40000", Enabled: false, UsernameRef: "proxy-user", PasswordRef: "proxy-password"}}
	if err := db.UpdateNetworkSettings(ctx, settings); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("Network settings referenced missing credentials")
	}
	for _, name := range []string{"proxy-user", "proxy-password"} {
		if err := db.PutSecret(ctx, name, []byte("synthetic-encrypted-value")); err != nil {
			t.Fatal(err)
		}
	}
	settings.Overrides["source:sample"] = "warp"
	if err := db.UpdateNetworkSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateNetworkSettings(ctx, settings); !errors.Is(err, model.ErrConflict) {
		t.Fatal("A stale administrator overwrote network settings")
	}
	if err := db.DeleteSecret(ctx, "proxy-password"); !errors.Is(err, model.ErrConflict) {
		t.Fatal("A configured proxy lost its authentication secret")
	}
	saved, err := db.NetworkSettings(ctx)
	if err != nil || saved.Revision != 2 || saved.Proxies[0].Enabled || saved.Overrides["source:sample"] != "warp" {
		t.Fatal("Saving changed activation or scoped routing")
	}
	var logs int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind='network.settings_changed' AND service='network' AND message='Network connections changed'`).Scan(&logs); err != nil || logs != 1 {
		t.Fatal("Failed settings writes changed logs or successful writes were not recorded")
	}
	if err := db.DeleteSourceData(ctx, "sample", nil); err != nil {
		t.Fatal(err)
	}
	saved, err = db.NetworkSettings(ctx)
	if err != nil || len(saved.Overrides) != 0 || saved.Revision != 3 || len(saved.Proxies) != 1 {
		t.Fatal("Deleting a source retained its route or removed a shared proxy")
	}
	saved.Proxies = nil
	if err := db.UpdateNetworkSettings(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSecret(ctx, "proxy-password"); err != nil {
		t.Fatal("An unused proxy secret could not be explicitly removed")
	}
}
