package store

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestConsoleSimplificationUpgradePreservesCoreStateAndAccessOutcomes(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.migrateThrough(ctx, 39); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.health_settings SET stale_hours=72;
INSERT INTO ingest.security_audit(action,target_id) VALUES('security.mfa_enabled','administrator'),('share.created','opaque-share');
INSERT INTO ingest.secrets(name,ciphertext) VALUES('retained','\x0102');`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object('security',(SELECT jsonb_agg(to_jsonb(s)) FROM ingest.admin_security s),'secrets',(SELECT jsonb_agg(to_jsonb(s)) FROM ingest.secrets s),'settings',(SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM ingest.settings s))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, consoleSimplificationSchema); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var oldTable bool
	if err := db.pool.QueryRow(ctx, "SELECT to_regclass('ingest.security_audit') IS NOT NULL").Scan(&oldTable); err != nil || !oldTable || snapshot() != before {
		t.Fatal("interrupted simplification changed core state or retired schema")
	}
	for range 2 {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("simplification changed encrypted secrets, authentication or collection settings")
		}
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 20}})
	if err != nil || len(logs.Items) != 2 || logs.Items[0].Message == "Collection activity recorded" || logs.Items[1].Message == "Collection activity recorded" {
		t.Fatal("retired audit outcomes were lost, duplicated or made unreadable")
	}
	var retired bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regclass('ingest.security_audit') IS NULL AND to_regclass('ingest.health_settings') IS NULL AND to_regclass('ingest.health_daily_samples') IS NULL AND to_regclass('ingest.health_source_observations') IS NULL AND to_regclass('ingest.health_alert_states') IS NULL`).Scan(&retired); err != nil || !retired {
		t.Fatal("retired diagnostic/audit tables remain")
	}
}
