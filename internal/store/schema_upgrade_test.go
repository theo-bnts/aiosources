package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func releasedSchemaDatabase(t *testing.T, through int) (context.Context, *Store) {
	t.Helper()
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `CREATE SCHEMA ingest;
 CREATE TABLE ingest.schema_migrations(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > through {
			break
		}
		if _, err = tx.Exec(ctx, legacyMigrationSQL(m.sql)); err != nil {
			t.Fatalf("install released schema %d: %v", m.version, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ingest.schema_migrations(version,checksum) VALUES($1,$2)`, m.version, legacyMigrationChecksum(m.sql)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

type schemaTableState struct {
	OID, File uint32
	Rows      string
}

func schemaState(t *testing.T, ctx context.Context, db *Store, namespace string) map[string]schemaTableState {
	t.Helper()
	rows, err := db.pool.Query(ctx, `SELECT relname,oid,relfilenode FROM pg_class
 WHERE relnamespace=$1::regnamespace AND relkind='r' AND relname<>'schema_migrations' ORDER BY relname`, namespace)
	if err != nil {
		t.Fatal(err)
	}
	state := map[string]schemaTableState{}
	for rows.Next() {
		var name string
		var entry schemaTableState
		if err := rows.Scan(&name, &entry.OID, &entry.File); err != nil {
			t.Fatal(err)
		}
		state[name] = entry
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name, entry := range state {
		query := `SELECT COALESCE(jsonb_agg(j ORDER BY j::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) AS j FROM ` + pgx.Identifier{namespace, name}.Sanitize() + ` t) items`
		if err := db.pool.QueryRow(ctx, query).Scan(&entry.Rows); err != nil {
			t.Fatal(err)
		}
		state[name] = entry
	}
	return state
}

func TestNamespaceUpgradeKeepsRecordsCheckpointsSecretsAndIndexes(t *testing.T) {
	ctx, db := releasedSchemaDatabase(t, legacySchemaVersion)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config,cursor)
 VALUES('saved-run','fixture','Fixture','full','paused','','{"version":1,"id":"fixture","adapter":"json"}','{"page":12}');
 INSERT INTO ingest.staged_torrents(run_id,provider_id,source_id,fields)
 VALUES('saved-run','fixture','staged','{"title":"Pending Example","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}');
 INSERT INTO ingest.torrents(provider_id,source_id,fields)
 VALUES('fixture','published','{"title":"Example Film 2026","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","private_note":"do-not-rewrite.ingest.literal"}');
 INSERT INTO ingest.secrets(name,ciphertext) VALUES('preserved-secret',decode('0102030405','hex'));
 INSERT INTO ingest.aio_works(media_type,tmdb_id,years,display_title) VALUES('movie','101',ARRAY[2026],'Example Film');
 INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','Verified fixture'
 FROM ingest.torrents t CROSS JOIN ingest.aio_works w;
 INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files)
 VALUES('torbox',repeat('a',40),true,now(),'[{"path":"Example.mkv","size":100}]');
 INSERT INTO ingest.events(run_id,kind,message,created_at) VALUES('saved-run','checkpoint','Saved checkpoint',now()-interval '10 days');`); err != nil {
		t.Fatal(err)
	}
	before := schemaState(t, ctx, db, legacySchema)
	indexes := map[uint32]bool{}
	rows, err := db.pool.Query(ctx, `SELECT i.indexrelid,i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid WHERE c.relnamespace='ingest'::regnamespace AND c.relname<>'schema_migrations'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var oid uint32
		var valid bool
		if err := rows.Scan(&oid, &valid); err != nil {
			t.Fatal(err)
		}
		indexes[oid] = valid
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := schemaState(t, ctx, db, applicationSchema); !reflect.DeepEqual(before, after) {
		for name, state := range before {
			if after[name] != state {
				t.Errorf("upgrading changed the data or physical identity of %s", name)
			}
		}
	}
	for oid, valid := range indexes {
		var after bool
		if err := db.pool.QueryRow(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid=$1`, oid).Scan(&after); err != nil || after != valid {
			t.Fatalf("existing index was replaced or invalidated: %v", err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("migration is not idempotent:", err)
	}
	var guard string
	if err := db.pool.QueryRow(ctx, `SELECT checksum FROM ingest.schema_migrations WHERE version=1`).Scan(&guard); err != nil || guard != legacySchemaGuard || guard == legacyMigrationChecksum(initialSchema) {
		t.Fatal("previous binaries could initialize an empty replacement database", err)
	}
	run, err := db.GetRun(ctx, "saved-run")
	if err != nil || string(run.Cursor) != `{"page": 12}` {
		var cursor map[string]int
		if err != nil || json.Unmarshal(run.Cursor, &cursor) != nil || cursor["page"] != 12 {
			t.Fatal("saved collection checkpoint is unreadable", err)
		}
	}
	changes, stop, err := db.SubscribeChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-changes
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.runs SET pages=pages+1 WHERE id='saved-run';
 INSERT INTO aiosources.events(run_id,kind,message) VALUES('saved-run','paused','Saved checkpoint')`); err != nil {
		t.Fatal("stored triggers did not adopt the application namespace:", err)
	}
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("live updates still use the previous notification channel")
	}
	if _, err := db.PruneHistory(ctx); err != nil {
		t.Fatal("cleanup authorization did not adopt the application namespace:", err)
	}
}

func TestNamespaceUpgradeRollsBackWhenMigrationHistoryIsInvalid(t *testing.T) {
	ctx, db := releasedSchemaDatabase(t, legacySchemaVersion)
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.schema_migrations SET checksum='invalid' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("invalid migration history was accepted")
	}
	var previous, current bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regclass('ingest.runs') IS NOT NULL,to_regnamespace('aiosources') IS NOT NULL`).Scan(&previous, &current); err != nil || !previous || current {
		t.Fatal("failed upgrade did not roll back its namespace changes", err)
	}
}

func TestNamespaceUpgradeRejectsAmbiguousDatabaseNamespaces(t *testing.T) {
	ctx, db := releasedSchemaDatabase(t, 1)
	if _, err := db.pool.Exec(ctx, `CREATE SCHEMA aiosources`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err == nil {
		t.Fatal("two existing namespaces were merged without authorization")
	}
}

func TestNewInstallUsesOnlyTheApplicationNamespace(t *testing.T) {
	ctx, db := sharingDatabase(t)
	var previous, current bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regnamespace('ingest') IS NOT NULL,to_regclass('aiosources.runs') IS NOT NULL`).Scan(&previous, &current); err != nil || previous || !current {
		t.Fatal("fresh installation retained the previous namespace", err)
	}
}

func TestNamespaceUpgradeKeepsSearchExtensionsInTheApplicationSchema(t *testing.T) {
	ctx, db := releasedSchemaDatabase(t, legacySchemaVersion)
	// An original role with the same name as its schema installs extensions
	// there through PostgreSQL's default "$user", public search path.
	if _, err := db.pool.Exec(ctx, `ALTER EXTENSION pg_trgm SET SCHEMA ingest`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("upgrade with application-owned search extensions:", err)
	}
	var similar bool
	if err := db.pool.QueryRow(ctx, `SELECT similarity('synthetic title','synthetic.title')>0 AND 'synthetic'::text % 'synthetic'`).Scan(&similar); err != nil || !similar {
		t.Fatal("search functions or operators became unreachable:", err)
	}
	var namespace string
	if err := db.pool.QueryRow(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE e.extname='pg_trgm'`).Scan(&namespace); err != nil || namespace != applicationSchema {
		t.Fatal("search extension did not follow its application schema:", err)
	}
}

func TestApplicationConnectionsPreserveConfiguredSearchPaths(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	before, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close(ctx)
	var original string
	if err := before.QueryRow(ctx, `SHOW search_path`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, pool := range []*pgxpool.Pool{db.pool, db.collectionPool} {
		var current string
		if err := pool.QueryRow(ctx, `SHOW search_path`).Scan(&current); err != nil || !strings.HasPrefix(current, applicationSchema+",") || strings.TrimPrefix(current, applicationSchema+",") != original {
			t.Fatal("a connection replaced the deployment search path:", err)
		}
	}
}

func TestNamespaceUpgradeAlsoMigratesEarlierReleasedDatabases(t *testing.T) {
	for _, version := range []int{1, 17, 50, 75} {
		t.Run(fmt.Sprintf("schema-%d", version), func(t *testing.T) {
			ctx, db := releasedSchemaDatabase(t, version)
			if err := db.Migrate(ctx); err != nil {
				t.Fatalf("upgrade from released schema %d: %v", version, err)
			}
			if err := db.Migrate(ctx); err != nil {
				t.Fatal("repeated migration changed its result:", err)
			}
		})
	}
}
