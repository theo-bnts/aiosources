package store

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
)

const (
	restoreBackupSchema = "aiosources_restore_prev"
	pgDumpMagic         = "PGDMP"
)

var (
	// ErrBackupToolUnavailable indicates pg_dump or pg_restore is missing or
	// incompatible with the connected PostgreSQL server major version.
	ErrBackupToolUnavailable = errors.New("PostgreSQL backup tools are unavailable")
	// ErrInvalidDatabaseArchive indicates the supplied file is not a valid
	// PostgreSQL custom archive containing an AIOSources schema.
	ErrInvalidDatabaseArchive = errors.New("invalid AIOSources database backup archive")
)

// BackupError pairs a sentinel error with a safe, user-facing explanation that
// never discloses database credentials, hostnames or raw SQL errors.
type BackupError struct {
	Sentinel error
	Message  string
}

func (e *BackupError) Error() string { return e.Message }
func (e *BackupError) Unwrap() error { return e.Sentinel }

// RestoreCallbacks coordinates application-level validation, authentication
// synchronization and progress reporting around a full schema restore.
type RestoreCallbacks struct {
	OnPhase         func(phase string)
	OnSchemaSwapped func(swapped bool)
	VerifyVault     func(ctx context.Context) error
	SyncAuth        func(ctx context.Context) error
}

var pgToolVersionRE = regexp.MustCompile(`\(PostgreSQL\)\s+(\d+)(?:\.(\d+))?`)

// buildPostgresEnv converts the configured DSN into libpq environment variables
// so credentials never appear in child command-line arguments and pgx-only pool
// query parameters never confuse libpq.
func buildPostgresEnv(databaseURL string, cfg *pgx.ConnConfig) []string {
	env := []string{
		"PGCONNECT_TIMEOUT=15",
		"PGPASSFILE=/dev/null",
		"PGSERVICEFILE=/dev/null",
		"PGAPPNAME=aiosources-database-backup",
		"LC_ALL=C",
		"LANG=C",
	}
	if cfg != nil {
		if cfg.Host != "" {
			env = append(env, "PGHOST="+cfg.Host)
		}
		if cfg.Port != 0 {
			env = append(env, "PGPORT="+strconv.Itoa(int(cfg.Port)))
		}
		if cfg.Database != "" {
			env = append(env, "PGDATABASE="+cfg.Database)
		}
		if cfg.User != "" {
			env = append(env, "PGUSER="+cfg.User)
		}
		if cfg.Password != "" {
			env = append(env, "PGPASSWORD="+cfg.Password)
		}
	}
	sslEnvMap := map[string]string{
		"sslmode":         "PGSSLMODE",
		"sslrootcert":     "PGSSLROOTCERT",
		"sslcert":         "PGSSLCERT",
		"sslkey":          "PGSSLKEY",
		"sslnegotiation":  "PGSSLNEGOTIATION",
		"channel_binding": "PGCHANNELBINDING",
	}
	if parsed, err := url.Parse(databaseURL); err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		for param, envKey := range sslEnvMap {
			if value := strings.TrimSpace(query.Get(param)); value != "" {
				env = append(env, envKey+"="+value)
			}
		}
	} else {
		for _, field := range strings.Fields(databaseURL) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			if envKey, known := sslEnvMap[key]; known {
				value = strings.Trim(value, `"'`)
				if value != "" {
					env = append(env, envKey+"="+value)
				}
			}
		}
	}
	return env
}

// commandEnv constructs a minimal, clean environment for PostgreSQL child tools,
// deliberately omitting inherited PG* variables, DATABASE_URL, ADMIN_PASSWORD
// and MASTER_KEY.
func commandEnv(dbEnv []string, tempDir string) []string {
	env := make([]string, 0, len(dbEnv)+8)
	hasHome := false
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch key {
		case "PATH", "SSL_CERT_FILE", "SSL_CERT_DIR", "TZ":
			env = append(env, entry)
		case "HOME":
			hasHome = true
			env = append(env, entry)
		}
	}
	if !hasHome {
		env = append(env, "HOME=/tmp")
	}
	if tempDir != "" {
		env = append(env, "TMPDIR="+tempDir)
	}
	env = append(env, dbEnv...)
	return env
}

func (s *Store) serverMajorVersion(ctx context.Context) (int, error) {
	var raw string
	if err := s.pool.QueryRow(ctx, `SHOW server_version_num`).Scan(&raw); err != nil {
		return 0, databaseError("read PostgreSQL server version", err)
	}
	num, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || num < 10000 {
		return 0, errors.New("could not determine PostgreSQL server version")
	}
	return num / 10000, nil
}

type pgToolInfo struct {
	path    string
	version string
	major   int
}

func probePostgresTool(ctx context.Context, path string) (pgToolInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, "--version")
	cmd.Env = commandEnv([]string{"LC_ALL=C", "LANG=C"}, "")
	out, err := cmd.Output()
	if err != nil {
		return pgToolInfo{}, err
	}
	matches := pgToolVersionRE.FindStringSubmatch(string(out))
	if len(matches) < 2 {
		return pgToolInfo{}, errors.New("unrecognized PostgreSQL client version")
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil || major <= 0 {
		return pgToolInfo{}, errors.New("invalid PostgreSQL client version")
	}
	version := "PostgreSQL " + matches[1]
	if len(matches) >= 3 && matches[2] != "" {
		version += "." + matches[2]
	}
	return pgToolInfo{path: path, version: version, major: major}, nil
}

func candidateToolPaths(name string) []string {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if p == "" {
			return
		}
		cleaned := filepath.Clean(p)
		if !seen[cleaned] {
			seen[cleaned] = true
			paths = append(paths, cleaned)
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		add(found)
	}
	for _, fixed := range []string{"/usr/bin/" + name, "/usr/local/bin/" + name} {
		if info, err := os.Stat(fixed); err == nil && !info.IsDir() {
			add(fixed)
		}
	}
	for _, pattern := range []string{
		"/usr/lib/postgresql/*/bin/" + name,
		"/opt/homebrew/opt/postgresql@*/bin/" + name,
		"/usr/local/opt/postgresql@*/bin/" + name,
	} {
		matches, _ := filepath.Glob(pattern)
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, match := range matches {
			add(match)
		}
	}
	return paths
}

func locatePostgresTool(ctx context.Context, name string, minMajor int) (pgToolInfo, error) {
	var best pgToolInfo
	for _, candidate := range candidateToolPaths(name) {
		info, err := probePostgresTool(ctx, candidate)
		if err != nil {
			continue
		}
		if info.major >= minMajor {
			return info, nil
		}
		if info.major > best.major {
			best = info
		}
	}
	if best.major > 0 {
		return pgToolInfo{}, &BackupError{
			Sentinel: ErrBackupToolUnavailable,
			Message:  fmt.Sprintf("Installed %s (%s) is older than the PostgreSQL %d server.", name, best.version, minMajor),
		}
	}
	return pgToolInfo{}, &BackupError{
		Sentinel: ErrBackupToolUnavailable,
		Message:  fmt.Sprintf("%s is not installed on this server.", name),
	}
}

// DatabaseBackupTools inspects pg_dump and pg_restore availability against the
// connected PostgreSQL server version.
func (s *Store) DatabaseBackupTools(ctx context.Context) model.DatabaseBackupTools {
	serverMajor, err := s.serverMajorVersion(ctx)
	if err != nil {
		return model.DatabaseBackupTools{Reason: "Could not verify PostgreSQL server version."}
	}
	dumpTool, dumpErr := locatePostgresTool(ctx, "pg_dump", serverMajor)
	restoreTool, restoreErr := locatePostgresTool(ctx, "pg_restore", serverMajor)
	result := model.DatabaseBackupTools{
		ExportAvailable: dumpErr == nil,
		ImportAvailable: restoreErr == nil,
	}
	if dumpErr == nil {
		result.Version = dumpTool.version
	} else if restoreErr == nil {
		result.Version = restoreTool.version
	}
	switch {
	case dumpErr != nil && restoreErr != nil:
		result.Reason = dumpErr.Error()
	case dumpErr != nil:
		result.Reason = dumpErr.Error()
	case restoreErr != nil:
		result.Reason = restoreErr.Error()
	}
	return result
}

type boundedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if remaining := b.max - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return strings.TrimSpace(b.buf.String()) }

// ExportDatabase streams a compressed PostgreSQL custom-format dump (-Fc) of
// the aiosources schema directly to dst without staging to disk. onHeader is
// invoked only after pg_dump successfully produces its PGDMP signature so HTTP
// callers can still return JSON errors on startup/connection failures.
func (s *Store) ExportDatabase(ctx context.Context, dst io.Writer, onHeader func() error) error {
	if s.Restoring() {
		return model.ErrBusy
	}
	serverMajor, err := s.serverMajorVersion(ctx)
	if err != nil {
		return err
	}
	dumpTool, err := locatePostgresTool(ctx, "pg_dump", serverMajor)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(
		ctx,
		dumpTool.path,
		"--schema="+applicationSchema,
		"--format=custom",
		"--compress=1",
		"--no-owner",
		"--no-privileges",
		"--lock-wait-timeout=30000",
	)
	cmd.Env = commandEnv(s.connEnv, "")
	cmd.WaitDelay = 5 * time.Second
	stderr := &boundedBuffer{max: 16 << 10}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("could not open pg_dump stream")
	}
	if err := cmd.Start(); err != nil {
		return &BackupError{Sentinel: ErrBackupToolUnavailable, Message: "Could not start pg_dump."}
	}

	var magic [5]byte
	if _, err := io.ReadFull(stdout, magic[:]); err != nil {
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("Database export failed before streaming started", "stderr_bytes", len(stderr.String()))
		return errors.New("database export could not start")
	}
	if string(magic[:]) != pgDumpMagic {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("pg_dump produced an unexpected output format")
	}
	if onHeader != nil {
		if err := onHeader(); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if _, err := dst.Write(magic[:]); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	buf := make([]byte, 64<<10)
	_, copyErr := io.CopyBuffer(dst, stdout, buf)
	waitErr := cmd.Wait()
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("Database export finished with pg_dump error", "stderr_bytes", len(stderr.String()))
		return errors.New("database export did not complete cleanly")
	}
	return nil
}

// HasPGDumpMagic verifies the 5-byte PostgreSQL custom archive signature.
func HasPGDumpMagic(header []byte) bool {
	return len(header) >= len(pgDumpMagic) && string(header[:len(pgDumpMagic)]) == pgDumpMagic
}

// ValidateDatabaseArchive inspects a staged PostgreSQL custom archive with
// pg_restore --list before modifying the live database.
func (s *Store) ValidateDatabaseArchive(ctx context.Context, archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return &BackupError{Sentinel: ErrInvalidDatabaseArchive, Message: "Could not read the uploaded backup file."}
	}
	var magic [5]byte
	_, readErr := io.ReadFull(file, magic[:])
	_ = file.Close()
	if readErr != nil || !HasPGDumpMagic(magic[:]) {
		return &BackupError{Sentinel: ErrInvalidDatabaseArchive, Message: "The selected file is not a PostgreSQL custom backup archive (.dump)."}
	}
	serverMajor, err := s.serverMajorVersion(ctx)
	if err != nil {
		return err
	}
	restoreTool, err := locatePostgresTool(ctx, "pg_restore", serverMajor)
	if err != nil {
		return err
	}
	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(listCtx, restoreTool.path, "--list", archivePath)
	cmd.Env = commandEnv([]string{"LC_ALL=C", "LANG=C"}, "")
	cmd.WaitDelay = 5 * time.Second
	var stdout bytes.Buffer
	stderr := &boundedBuffer{max: 8 << 10}
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &BackupError{
			Sentinel: ErrInvalidDatabaseArchive,
			Message:  "The backup archive is unreadable, corrupted, or was created by an unsupported PostgreSQL version.",
		}
	}
	hasMigrationsTable, hasMigrationsData := parseBackupTOC(&stdout)
	if !hasMigrationsTable || !hasMigrationsData {
		return &BackupError{
			Sentinel: ErrInvalidDatabaseArchive,
			Message:  "This archive does not contain a valid AIOSources database schema (aiosources.schema_migrations).",
		}
	}
	return nil
}

func parseBackupTOC(r io.Reader) (hasMigrationsTable, hasMigrationsData bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		// Standard pg_restore --list entry:
		//   <dumpId>; <tableOid> <oid> <desc> <namespace> <tag> <owner>
		// Multi-word descriptions such as "TABLE DATA" are matched explicitly.
		parts := strings.Fields(line)
		if len(parts) < 6 {
			continue
		}
		if parts[3] == "TABLE" && parts[4] == applicationSchema && parts[5] == "schema_migrations" {
			hasMigrationsTable = true
		}
		if len(parts) >= 7 && parts[3] == "TABLE" && parts[4] == "DATA" && parts[5] == applicationSchema && parts[6] == "schema_migrations" {
			hasMigrationsData = true
		}
	}
	return hasMigrationsTable, hasMigrationsData
}

// recoverInterruptedDatabaseRestore runs inside Migrate() on startup so a hard
// process crash mid-restore automatically reinstates the previous schema.
func recoverInterruptedDatabaseRestore(ctx context.Context, tx pgx.Tx) error {
	var backupMigrations, currentMigrations bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('aiosources_restore_prev.schema_migrations') IS NOT NULL,
 to_regclass('aiosources.schema_migrations') IS NOT NULL`).Scan(&backupMigrations, &currentMigrations); err != nil {
		return databaseError("inspect interrupted database restore", err)
	}
	if !backupMigrations || currentMigrations {
		return nil
	}
	if _, err := tx.Exec(ctx, `DROP SCHEMA IF EXISTS aiosources CASCADE;
ALTER SCHEMA aiosources_restore_prev RENAME TO aiosources`); err != nil {
		return databaseError("recover interrupted database restore", err)
	}
	slog.Warn("Recovered previous AIOSources database schema after an interrupted restore")
	return nil
}

// RestoreDatabase replaces the aiosources schema from a validated PostgreSQL
// custom archive. The existing schema is renamed aside first so any restore,
// migration, vault or authentication failure rolls back cleanly without data loss.
func (s *Store) RestoreDatabase(ctx context.Context, archivePath, tempDir string, callbacks RestoreCallbacks) error {
	reportPhase := func(phase string) {
		if callbacks.OnPhase != nil {
			callbacks.OnPhase(phase)
		}
	}
	setSwapped := func(swapped bool) {
		if callbacks.OnSchemaSwapped != nil {
			callbacks.OnSchemaSwapped(swapped)
		}
	}

	reportPhase("validating")
	if err := s.ValidateDatabaseArchive(ctx, archivePath); err != nil {
		return err
	}
	serverMajor, err := s.serverMajorVersion(ctx)
	if err != nil {
		return err
	}
	restoreTool, err := locatePostgresTool(ctx, "pg_restore", serverMajor)
	if err != nil {
		return err
	}
	if !s.restoring.CompareAndSwap(false, true) {
		return model.ErrBusy
	}
	defer s.restoring.Store(false)

	adminConfig := s.pool.Config().ConnConfig.Copy()
	adminConfig.RuntimeParams["application_name"] = "aiosources-database-restore"
	adminConn, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		return databaseError("connect for database restore", err)
	}
	defer closeConnection(adminConn)

	// Guard against active imports or concurrent optional index changes.
	var runningRuns, indexing bool
	if err := adminConn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.runs WHERE status='running'),
 EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a USING(pid) WHERE l.locktype='advisory' AND l.classid=1768843109 AND l.objid=5 AND l.granted AND a.datname=current_database() AND a.application_name='aiosources-storage-indexes')`).Scan(&runningRuns, &indexing); err != nil {
		return databaseError("check restore preconditions", err)
	}
	if runningRuns || indexing {
		return model.ErrBusy
	}

	// Ensure pg_trgm is installed before restoring GIN trigram indexes without
	// requiring superuser privileges when it already exists.
	var hasTrgm bool
	if err := adminConn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_trgm')`).Scan(&hasTrgm); err != nil {
		return databaseError("check pg_trgm extension", err)
	}
	if !hasTrgm {
		if _, err := adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`); err != nil {
			return databaseError("install pg_trgm extension", err)
		}
	}

	swapped := false
	rollbackSchema := func() {
		if !swapped {
			return
		}
		s.ResetPools()
		rbCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rbConn, connErr := pgx.ConnectConfig(rbCtx, adminConfig)
		if connErr != nil {
			slog.Error("Could not reconnect to roll back failed database restore")
			return
		}
		defer closeConnection(rbConn)
		_, _ = rbConn.Exec(rbCtx, `SET lock_timeout='15s'`)
		if _, err := rbConn.Exec(rbCtx, `DROP SCHEMA IF EXISTS aiosources CASCADE;
ALTER SCHEMA aiosources_restore_prev RENAME TO aiosources`); err != nil {
			slog.Error("Database restore rollback failed; previous schema remains in aiosources_restore_prev")
			return
		}
		swapped = false
		s.ResetPools()
	}

	setSwapped(true)
	defer func() {
		if swapped {
			rollbackSchema()
		}
		setSwapped(false)
	}()

	// Drop pooled connections before renaming the schema so no idle session
	// retains stale OIDs or open table references.
	s.ResetPools()

	tx, err := adminConn.Begin(ctx)
	if err != nil {
		return databaseError("begin schema swap", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='15s'`); err != nil {
		rollback(tx)
		return databaseError("configure schema swap timeout", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, migrationLockNamespace, migrationLockID); err != nil {
		rollback(tx)
		return databaseError("lock schema for restore", err)
	}
	if _, err := tx.Exec(ctx, `DROP SCHEMA IF EXISTS `+restoreBackupSchema+` CASCADE;
ALTER SCHEMA aiosources RENAME TO `+restoreBackupSchema+`;
CREATE SCHEMA aiosources;
COMMENT ON SCHEMA aiosources IS 'AIOSources application data'`); err != nil {
		rollback(tx)
		return databaseError("prepare target schema for restore", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return databaseError("commit schema swap", err)
	}
	swapped = true

	reportPhase("restoring")
	if err := s.runPGRestore(ctx, restoreTool.path, archivePath, tempDir); err != nil {
		return err
	}

	// Discard any pooled connections before running migrations and validation
	// against the freshly restored schema OIDs.
	s.ResetPools()

	reportPhase("migrating")
	if err := s.Migrate(ctx); err != nil {
		return &BackupError{
			Sentinel: model.ErrInvalid,
			Message:  "The backup schema is not compatible with this AIOSources version: " + err.Error() + ". The previous database has been restored.",
		}
	}

	reportPhase("verifying")
	if callbacks.VerifyVault != nil {
		if err := callbacks.VerifyVault(ctx); err != nil {
			return err
		}
	}

	reportPhase("finalizing")
	if callbacks.SyncAuth != nil {
		if err := callbacks.SyncAuth(ctx); err != nil {
			return err
		}
	}

	// Commit point: drop the preserved previous schema once migration, vault and
	// authentication checks have all succeeded.
	commitCtx, commitCancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer commitCancel()
	if _, err := adminConn.Exec(commitCtx, `DROP SCHEMA IF EXISTS `+restoreBackupSchema+` CASCADE`); err != nil {
		slog.Warn("Restored database committed, but cleanup of previous schema failed")
	}
	swapped = false
	s.ResetPools()

	// Refresh planner statistics across restored application tables without
	// failing the completed restore if ANALYZE is interrupted.
	analyzeCtx, analyzeCancel := context.WithTimeout(commitCtx, 60*time.Second)
	defer analyzeCancel()
	_, _ = adminConn.Exec(analyzeCtx, `DO $$
DECLARE r record;
BEGIN
 FOR r IN SELECT tablename FROM pg_tables WHERE schemaname='aiosources' LOOP
  EXECUTE format('ANALYZE aiosources.%I', r.tablename);
 END LOOP;
END $$`)

	_ = s.RecordActivity(commitCtx, "info", "storage.database_imported", "", "", "", nil)
	return nil
}

func (s *Store) runPGRestore(ctx context.Context, restorePath, archivePath, tempDir string) error {
	dbName := s.databaseName
	if dbName == "" && s.pool != nil {
		dbName = s.pool.Config().ConnConfig.User
	}
	if dbName == "" {
		dbName = "postgres"
	}
	args := []string{
		"--no-owner",
		"--no-privileges",
		"--exit-on-error",
		"--schema=" + applicationSchema,
		"--jobs=2",
		"--dbname=" + dbName,
		archivePath,
	}
	cmd := exec.CommandContext(ctx, restorePath, args...)
	cmd.Env = commandEnv(slices.Clone(s.connEnv), tempDir)
	cmd.WaitDelay = 5 * time.Second
	stderr := &boundedBuffer{max: 16 << 10}
	cmd.Stderr = stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("pg_restore failed during database import", "stderr_bytes", len(stderr.String()))
		return &BackupError{
			Sentinel: model.ErrInvalid,
			Message:  "PostgreSQL could not restore the backup archive. Check that the dump is complete and matches PostgreSQL compatibility; the previous database has been restored.",
		}
	}
	return nil
}
