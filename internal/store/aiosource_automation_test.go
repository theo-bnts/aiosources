package store

import "testing"

func TestInformationAutomationDefaultsPreserveSavedManualSettings(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	c, err := db.AIOConfig(ctx)
	if err != nil || !c.AutoSync || !c.AutoMatch || !c.AutoCache || c.SyncMinutes != 15 {
		t.Fatalf("new installation automation: %+v %v", c, err)
	}
	c.AutoSync, c.AutoMatch, c.AutoCache = false, false, false
	if err := db.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err := db.AIOConfig(ctx)
	if err != nil || saved.AutoSync || saved.AutoMatch || saved.AutoCache || saved.Revision != c.Revision+1 {
		t.Fatalf("saved manual settings were overridden: %+v %v", saved, err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE aiosources.settings SET value=(value::jsonb-'auto_cache')::text WHERE key='aiosource.config.v2'`); err != nil {
		t.Fatal(err)
	}
	upgraded, err := db.AIOConfig(ctx)
	if err != nil || !upgraded.AutoCache || upgraded.AutoSync || upgraded.AutoMatch {
		t.Fatal("older settings did not acquire the new default independently", err)
	}
}
