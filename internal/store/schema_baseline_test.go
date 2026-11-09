package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

// Released fingerprints verify the real 2.3 metadata ledger. The obsolete SQL
// before 2.3 is not part of the application or these test installations.
func released23Database(t *testing.T, version int, namespace string) (context.Context, *Store) {
	t.Helper()
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	rewrite := func(sql string) string {
		return strings.NewReplacer(applicationSchema+".", namespace+".", "'"+applicationSchema+"'", "'"+namespace+"'", applicationSchema+"_run_changes", namespace+"_run_changes").Replace(sql)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{namespace}.Sanitize()+`;CREATE TABLE `+pgx.Identifier{namespace, "schema_migrations"}.Sanitize()+`(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now());`+rewrite(initialSchema)); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > version {
			break
		}
		if _, err = tx.Exec(ctx, rewrite(m.sql)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile("testdata/schema-2.3-checksums.json")
	if err != nil {
		t.Fatal(err)
	}
	var checksums []struct {
		Version             int
		Canonical, Previous string
	}
	if err = json.Unmarshal(data, &checksums); err != nil {
		t.Fatal(err)
	}
	for _, m := range checksums {
		if m.Version > version {
			break
		}
		checksum := m.Canonical
		if namespace != applicationSchema {
			checksum = m.Previous
		}
		if _, err = tx.Exec(ctx, `INSERT INTO `+pgx.Identifier{namespace, "schema_migrations"}.Sanitize()+`(version,checksum) VALUES($1,$2)`, m.Version, checksum); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

func baselineTableState(t *testing.T, ctx context.Context, db *Store, namespace string) map[string]string {
	t.Helper()
	rows, err := db.pool.Query(ctx, `SELECT relname,oid,relfilenode FROM pg_class WHERE relnamespace=$1::regnamespace AND relkind='r' AND relname<>'schema_migrations' ORDER BY relname`, namespace)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]string{}
	for rows.Next() {
		var name string
		var oid, file uint32
		if err = rows.Scan(&name, &oid, &file); err != nil {
			t.Fatal(err)
		}
		state[name] = fmt.Sprintf("%d:%d:", oid, file)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, prefix := range state {
		var data string
		if err = db.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(j ORDER BY j::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t)-'localized_posters'-'storage_options'`+map[bool]string{true: "-'options'"}[name == "storage_settings"]+` AS j FROM `+pgx.Identifier{namespace, name}.Sanitize()+` t) items`).Scan(&data); err != nil {
			t.Fatal(err)
		}
		state[name] = prefix + data
	}
	return state
}

func TestUpgradesFromEvery23SchemaPreservePublishedDataAndCheckpoints(t *testing.T) {
	for _, version := range []int{89, 90, 91, 92, 93} {
		for _, namespace := range []string{applicationSchema, legacySchema} {
			if version == 93 && namespace == legacySchema {
				continue
			}
			t.Run(fmt.Sprintf("%s-%d", namespace, version), func(t *testing.T) {
				ctx, db := released23Database(t, version, namespace)
				if _, err := db.pool.Exec(ctx, strings.ReplaceAll(`INSERT INTO aiosources.runs(id,provider_id,provider_name,mode,status,revision,config,cursor) VALUES('saved','fixture','Example','full','paused','','{"version":1,"id":"fixture","adapter":"json"}','{"page":12}');
INSERT INTO aiosources.staged_torrents(run_id,provider_id,source_id,fields) VALUES('saved','fixture','pending','{"title":"Pending example"}');
INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','published','{"title":"Example film"}');
INSERT INTO aiosources.secrets(name,ciphertext) VALUES('example-secret',decode('01020304','hex'));`, applicationSchema+".", namespace+".")); err != nil {
					t.Fatal(err)
				}
				before := baselineTableState(t, ctx, db, namespace)
				if err := db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				after := baselineTableState(t, ctx, db, applicationSchema)
				for name, state := range before {
					if after[name] != state {
						t.Errorf("upgrade changed protected table %s", name)
					}
				}
				if err := db.Migrate(ctx); err != nil {
					t.Fatal("upgrade is not idempotent", err)
				}
				var count, minVersion int
				if err := db.pool.QueryRow(ctx, `SELECT count(*),min(version) FROM aiosources.schema_migrations`).Scan(&count, &minVersion); err != nil || count != LatestSchemaVersion()-minimumSchemaVersion+1 || minVersion != minimumSchemaVersion {
					t.Fatal("obsolete migration ledger was retained", count, minVersion, err)
				}
				if _, err := db.PruneHistory(ctx); err != nil {
					t.Fatal("current stored functions are unreadable", err)
				}
			})
		}
	}
}

func TestPre23DatabasesAreRejectedBeforeAnyChanges(t *testing.T) {
	for _, namespace := range []string{legacySchema, applicationSchema} {
		for _, version := range []int{1, 17, 75, 88} {
			t.Run(fmt.Sprintf("%s-%d", namespace, version), func(t *testing.T) {
				ctx, endpoint := testutil.NewDatabase(t)
				db, err := Open(ctx, endpoint)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err = db.pool.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{namespace}.Sanitize()+`;CREATE TABLE `+pgx.Identifier{namespace, "schema_migrations"}.Sanitize()+`(version INT PRIMARY KEY,checksum TEXT NOT NULL);CREATE TABLE `+pgx.Identifier{namespace, "saved_data"}.Sanitize()+`(value TEXT);INSERT INTO `+pgx.Identifier{namespace, "saved_data"}.Sanitize()+` VALUES('preserved')`); err != nil {
					t.Fatal(err)
				}
				if _, err = db.pool.Exec(ctx, `INSERT INTO `+pgx.Identifier{namespace, "schema_migrations"}.Sanitize()+` VALUES($1,'older-release')`, version); err != nil {
					t.Fatal(err)
				}
				before := baselineTableState(t, ctx, db, namespace)
				if err = db.Migrate(ctx); !errors.Is(err, errUpgradeRequired) {
					t.Fatal("missing actionable upgrade error", err)
				}
				if after := baselineTableState(t, ctx, db, namespace); !reflect.DeepEqual(before, after) {
					t.Fatal("unsupported data was modified")
				}
				if namespace == legacySchema {
					var current bool
					if err = db.pool.QueryRow(ctx, `SELECT to_regnamespace('aiosources') IS NOT NULL`).Scan(&current); err != nil || current {
						t.Fatal("unsupported database was renamed", err)
					}
				}
			})
		}
	}
}

func TestBaselineRejectsTamperedHistoryAtomically(t *testing.T) {
	ctx, db := released23Database(t, 92, legacySchema)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.schema_migrations SET checksum='modified' WHERE version=17`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("invalid history accepted")
	}
	var previous, current bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regclass('ingest.runs') IS NOT NULL,to_regnamespace('aiosources') IS NOT NULL`).Scan(&previous, &current); err != nil || !previous || current {
		t.Fatal("failed upgrade did not roll back", err)
	}
}

func TestNewBaselineSeedsIndependentCatalogIdentitiesAndPolicies(t *testing.T) {
	ids := map[string]bool{}
	for range 2 {
		ctx, db := sharingDatabase(t)
		var id string
		if err := db.pool.QueryRow(ctx, `SELECT instance_id FROM aiosources.catalog_identity`).Scan(&id); err != nil || ids[id] {
			t.Fatal("initial catalog identity is not independent", err)
		}
		ids[id] = true
		for _, section := range []string{"history", "security", "metadata", "connections", "outgoing"} {
			policy, err := db.RuntimeSection(ctx, section)
			if err != nil || !policy.Valid(section) {
				t.Fatal("initial policy missing", section, err)
			}
		}
		var old bool
		if err := db.pool.QueryRow(ctx, `SELECT to_regnamespace('ingest') IS NOT NULL`).Scan(&old); err != nil || old {
			t.Fatal("fresh database retained the previous namespace", err)
		}
	}
}

func TestApplicationConnectionsPreserveConfiguredSearchPaths(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	original, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close(ctx)
	var before string
	if err = original.QueryRow(ctx, `SHOW search_path`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, pool := range []*pgxpool.Pool{db.pool, db.collectionPool} {
		var after string
		if err = pool.QueryRow(ctx, `SHOW search_path`).Scan(&after); err != nil || after != applicationSchema+","+before {
			t.Fatal("connection search path changed", err)
		}
	}
}

func TestSupportedUpgradeKeepsSearchExtensionsAndRejectsFutureVersions(t *testing.T) {
	ctx, db := released23Database(t, 92, legacySchema)
	if _, err := db.pool.Exec(ctx, `ALTER EXTENSION pg_trgm SET SCHEMA ingest`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var ok bool
	if err := db.pool.QueryRow(ctx, `SELECT similarity('example','example')=1 AND 'example'::text % 'example'`).Scan(&ok); err != nil || !ok {
		t.Fatal("search extension disappeared", err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.schema_migrations(version,checksum) VALUES($1,'future')`, LatestSchemaVersion()+1); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("future database accepted")
	}
}
