package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestDebridServicesUpgradePreservesConfigurationAndHistory(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.migrateThrough(ctx, 74); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.settings(key,value) VALUES('aiosource.config.v2','{"revision":4,"alldebrid_secret":"existing-ad","torbox_secret":"existing-tb"}');
INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',true,now(),'[]');
INSERT INTO ingest.operation_runs(id,kind,service,trigger,lock_key,status,finished_at) VALUES('old','cache','torbox','manual',1,'succeeded',now());`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var v string
		if err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object('settings',(SELECT jsonb_agg(to_jsonb(s)) FROM ingest.settings s),'history',(SELECT jsonb_agg(to_jsonb(r)) FROM ingest.operation_runs r),'cache',(SELECT jsonb_agg(to_jsonb(c)-'catalog_import_source') FROM ingest.aio_shared_availability c))::text`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := snapshot()
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, debridServicesSchema); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != before {
		t.Fatal("rolled-back migration changed data")
	}
	for range 2 {
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("upgrade changed existing credentials or history")
		}
	}
	c, err := db.AIOConfig(ctx)
	if err != nil || c.AllDebridSecret != "existing-ad" || c.TorBoxSecret != "existing-tb" || c.Revision != 4 {
		t.Fatal("existing credential references changed")
	}
	generation, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, service := range model.DebridServices() {
		if err := db.SaveSharedAvailability(ctx, service, model.AIOAvailability{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.mkv", Size: 100}}}); err != nil {
			t.Fatalf("new service cache rejected: %s %v", service, err)
		}
		if err := db.RecordPlaybackFailure(ctx, service, "add", "account_limit"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnavailableAccountItemInvalidatesOnlyItsOlderEvidence(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	generation, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash, observed := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(-time.Minute)
	for _, account := range []string{"older", "newer", "other"} {
		at := observed.Add(-time.Second)
		if account == "newer" {
			at = observed.Add(time.Second)
		}
		if err := db.SaveAIOAvailability(ctx, "realdebrid", account, model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: at, Generation: generation}); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"older", "newer"} {
		if err := db.ForgetAIOAvailability(ctx, "realdebrid", account, hash, observed); err != nil {
			t.Fatal(err)
		}
	}
	for _, account := range []string{"older", "newer", "other"} {
		items, err := db.AIOAvailability(ctx, "realdebrid", account, []string{hash}, observed.Add(-time.Hour))
		if err != nil || (len(items) == 1) != (account != "older") {
			t.Fatalf("account isolation or observation ordering failed: %s %v", account, err)
		}
	}
}
