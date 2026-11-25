package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestDatabaseExportRestoreRollbackAndCrashRecovery(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	tools := db.DatabaseBackupTools(ctx)
	if !tools.ExportAvailable || !tools.ImportAvailable {
		t.Skipf("matching PostgreSQL client tools unavailable: %s", tools.Reason)
	}

	// Seed representative data across settings, torrents, works, matches and secrets.
	if err := db.SetSetting(ctx, "backup.test.marker", "before-mutation"); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecret(ctx, "backup-secret", []byte("ciphertext-v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields)
 VALUES('fixture','t-1','{"title":"Backup.Sample.2026.1080p","info_hash":"0123456789abcdef0123456789abcdef01234567"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	var workID int64
	if err := db.pool.QueryRow(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years)
 VALUES('movie','777001','Backup Sample',ARRAY[2026]) RETURNING id`).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
 SELECT t.provider_id,t.source_id,t.fields_fingerprint,$1,w.revision,'matched','fixture'
 FROM aiosources.torrents t JOIN aiosources.aio_works w ON w.id=$1
 WHERE t.provider_id='fixture' AND t.source_id='t-1'`, workID); err != nil {
		t.Fatal(err)
	}

	// 1. Stream custom-format export directly via pg_dump.
	var dump bytes.Buffer
	headerSeen := false
	if err := db.ExportDatabase(ctx, &dump, func() error {
		headerSeen = true
		return nil
	}); err != nil {
		t.Fatalf("export database: %v", err)
	}
	if !headerSeen || !HasPGDumpMagic(dump.Bytes()) {
		t.Fatalf("expected PGDMP header callback and signature, got %d bytes", dump.Len())
	}

	tempDir := t.TempDir()
	archivePath := filepath.Join(tempDir, "valid.dump")
	if err := os.WriteFile(archivePath, dump.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateDatabaseArchive(ctx, archivePath); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}

	// 2. Reject invalid non-PGDMP files and truncated dumps before touching PostgreSQL.
	badMagicPath := filepath.Join(tempDir, "not-a-dump.dump")
	if err := os.WriteFile(badMagicPath, []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateDatabaseArchive(ctx, badMagicPath); !errors.Is(err, ErrInvalidDatabaseArchive) {
		t.Fatalf("expected ErrInvalidDatabaseArchive for plain text, got %v", err)
	}
	truncatedPath := filepath.Join(tempDir, "truncated.dump")
	if err := os.WriteFile(truncatedPath, dump.Bytes()[:64], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.ValidateDatabaseArchive(ctx, truncatedPath); !errors.Is(err, ErrInvalidDatabaseArchive) {
		t.Fatalf("expected ErrInvalidDatabaseArchive for truncated dump, got %v", err)
	}

	// 3. Mutate the live database after export.
	if err := db.SetSetting(ctx, "backup.test.marker", "after-mutation"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM aiosources.aio_matches; DELETE FROM aiosources.torrents`); err != nil {
		t.Fatal(err)
	}

	// 4. Verify rollback keeps the mutated live state intact if vault verification fails.
	vaultFail := errors.New("simulated master key mismatch")
	err = db.RestoreDatabase(ctx, archivePath, tempDir, RestoreCallbacks{
		VerifyVault: func(context.Context) error { return vaultFail },
	})
	if !errors.Is(err, vaultFail) {
		t.Fatalf("expected vault failure error, got %v", err)
	}
	marker, err := db.Setting(ctx, "backup.test.marker")
	if err != nil || marker != "after-mutation" {
		t.Fatalf("expected rollback to keep live setting after-mutation, got %q %v", marker, err)
	}
	var torrentCount int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.torrents`).Scan(&torrentCount); err != nil || torrentCount != 0 {
		t.Fatalf("expected rollback to preserve live torrent count 0, got %d %v", torrentCount, err)
	}

	// 5. Full restore succeeds, restores original rows, cleans up backup schema and resets pools cleanly.
	var phases []string
	var swappedTransitions []bool
	err = db.RestoreDatabase(ctx, archivePath, tempDir, RestoreCallbacks{
		OnPhase:         func(phase string) { phases = append(phases, phase) },
		OnSchemaSwapped: func(swapped bool) { swappedTransitions = append(swappedTransitions, swapped) },
		VerifyVault:     func(context.Context) error { return nil },
		SyncAuth:        func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("restore database: %v", err)
	}
	if len(phases) == 0 || phases[len(phases)-1] != "finalizing" {
		t.Fatalf("unexpected restore phases: %v", phases)
	}
	if len(swappedTransitions) != 2 || !swappedTransitions[0] || swappedTransitions[1] {
		t.Fatalf("unexpected schema swap transitions: %v", swappedTransitions)
	}
	marker, err = db.Setting(ctx, "backup.test.marker")
	if err != nil || marker != "before-mutation" {
		t.Fatalf("expected restored setting before-mutation, got %q %v", marker, err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.torrents WHERE source_id='t-1'`).Scan(&torrentCount); err != nil || torrentCount != 1 {
		t.Fatalf("expected restored torrent t-1, got %d %v", torrentCount, err)
	}
	var matchCount int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.aio_matches WHERE status='matched'`).Scan(&matchCount); err != nil || matchCount != 1 {
		t.Fatalf("expected restored match, got %d %v", matchCount, err)
	}
	secret, err := db.Secret(ctx, "backup-secret")
	if err != nil || string(secret) != "ciphertext-v1" {
		t.Fatalf("expected restored encrypted secret, got %q %v", string(secret), err)
	}
	var leftoverBackup bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL`, restoreBackupSchema).Scan(&leftoverBackup); err != nil || leftoverBackup {
		t.Fatalf("expected %s to be dropped after commit, got %v %v", restoreBackupSchema, leftoverBackup, err)
	}
	var importLogs int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM aiosources.activity_logs WHERE kind='storage.database_imported'`).Scan(&importLogs); err != nil || importLogs != 1 {
		t.Fatalf("expected storage.database_imported activity entry, got %d %v", importLogs, err)
	}

	// Verify repeated queries through the reset pools hit no cached-plan OID errors.
	if _, err := db.StorageOverview(ctx); err != nil {
		t.Fatalf("post-restore storage overview: %v", err)
	}
	if _, err := db.ListTorrents(ctx, model.ListOptions{Limit: 10}); err != nil {
		t.Fatalf("post-restore torrent list: %v", err)
	}

	// 6. Simulate a hard crash mid-restore (aiosources missing, aiosources_restore_prev intact)
	// and verify startup Migrate() recovers the previous schema automatically.
	if _, err := db.pool.Exec(ctx, `ALTER SCHEMA aiosources RENAME TO aiosources_restore_prev; CREATE SCHEMA aiosources`); err != nil {
		t.Fatal(err)
	}
	db.ResetPools()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("crash recovery via Migrate failed: %v", err)
	}
	marker, err = db.Setting(ctx, "backup.test.marker")
	if err != nil || marker != "before-mutation" {
		t.Fatalf("expected crash recovery to reinstate before-mutation, got %q %v", marker, err)
	}
}

func TestParseBackupTOCAndBuildPostgresEnv(t *testing.T) {
	toc := strings.NewReader(`
; Archive created at 2026-11-25
7; 2615 16465 SCHEMA - aiosources ingest
221; 1259 16466 TABLE aiosources schema_migrations ingest
3901; 0 16466 TABLE DATA aiosources schema_migrations ingest
`)
	hasTable, hasData := parseBackupTOC(toc)
	if !hasTable || !hasData {
		t.Fatalf("expected valid TOC detection, got table=%v data=%v", hasTable, hasData)
	}
	foreignTOC := strings.NewReader(`
221; 1259 16466 TABLE public users postgres
3901; 0 16466 TABLE DATA public users postgres
`)
	hasTable, hasData = parseBackupTOC(foreignTOC)
	if hasTable || hasData {
		t.Fatalf("expected foreign TOC rejection, got table=%v data=%v", hasTable, hasData)
	}

	env := buildPostgresEnv("postgresql://user:secret@db.internal:5433/aios?sslmode=require&pool_max_conns=32", nil)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "PGSSLMODE=require") {
		t.Fatalf("expected PGSSLMODE=require in %v", env)
	}
	if strings.Contains(joined, "pool_max_conns") {
		t.Fatalf("pgx pool params must not be passed to libpq: %v", env)
	}
}
