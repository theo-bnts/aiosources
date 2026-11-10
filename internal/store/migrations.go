package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Keep the migration lock stable while supported release workers are replaced.
const migrationLockNamespace int32 = 1768843109
const migrationLockID int32 = 1
const applicationSchema = "aiosources"
const minimumSchemaVersion = 89 // The first AIOSources 2.3.0 release.

//go:embed schema.sql
var initialSchema string

var errUpgradeRequired = errors.New("AIOSources 2.3 or later is required before upgrading to 2.4. Upgrade this installation to the latest 2.3 build first, then retry. Your existing data has not been changed")

var migrations = []struct {
	version int
	sql     string
}{
	{90, storageControlsSchema},
	{91, directCatalogSchema},
	{92, compoundMatchingSchema},
	{93, `COMMENT ON SCHEMA aiosources IS 'AIOSources application data';`},
	{94, localizedPostersSchema},
	{95, gamesSchema},
}

func schemaChecksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

// LatestSchemaVersion is the newest schema this binary can migrate and read.
func LatestSchemaVersion() int { return migrations[len(migrations)-1].version }
func (s *Store) Migrate(ctx context.Context) error {
	return s.migrateThrough(ctx, LatestSchemaVersion())
}

func (s *Store) migrateThrough(ctx context.Context, version int) error {
	if version < minimumSchemaVersion || version > LatestSchemaVersion() {
		return errUpgradeRequired
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin schema migration", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1,$2)", migrationLockNamespace, migrationLockID); err != nil {
		return databaseError("lock schema migration", err)
	}
	adopted, err := adoptApplicationSchema(ctx, tx)
	if err != nil {
		return err
	}
	var metadata, occupied, firstGeneration bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('aiosources.schema_migrations') IS NOT NULL,
 EXISTS(SELECT 1 FROM pg_class WHERE relnamespace=to_regnamespace('aiosources')),
 to_regclass('public.torrents') IS NOT NULL`).Scan(&metadata, &occupied, &firstGeneration); err != nil {
		return databaseError("inspect application schema", err)
	}
	if !metadata && (occupied || firstGeneration) {
		return errUpgradeRequired
	}
	if _, err = tx.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS aiosources;
CREATE TABLE IF NOT EXISTS aiosources.schema_migrations(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return databaseError("initialize schema migration", err)
	}
	var installed int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM aiosources.schema_migrations`).Scan(&installed); err != nil {
		return databaseError("read installed schema version", err)
	}
	if installed > LatestSchemaVersion() {
		return errors.New("the database was upgraded by a newer application version")
	}
	if metadata && installed < minimumSchemaVersion {
		return errUpgradeRequired
	}
	if !metadata {
		if _, err = tx.Exec(ctx, initialSchema); err != nil {
			return databaseError("initialize application database", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO aiosources.schema_migrations(version,checksum) VALUES($1,$2)`, minimumSchemaVersion, schemaChecksum(initialSchema)); err != nil {
			return databaseError("record schema baseline", err)
		}
		installed = minimumSchemaVersion
	} else if err = consolidateSchemaBaseline(ctx, tx, adopted); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version > version {
			break
		}
		checksum := schemaChecksum(m.sql)
		var previous string
		err = tx.QueryRow(ctx, `SELECT checksum FROM aiosources.schema_migrations WHERE version=$1`, m.version).Scan(&previous)
		if err == nil {
			if previous != checksum {
				if !adopted || previous != releasedUpgradeChecksums[m.version] {
					return errors.New("application schema migration checksum mismatch")
				}
				if _, err = tx.Exec(ctx, `UPDATE aiosources.schema_migrations SET checksum=$2 WHERE version=$1`, m.version, checksum); err != nil {
					return databaseError("preserve schema upgrade history", err)
				}
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return databaseError("read schema migration", err)
		}
		if m.version <= installed {
			return errors.New("application schema migration history is incomplete")
		}
		if _, err = tx.Exec(ctx, m.sql); err != nil {
			return databaseError("apply schema migration", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO aiosources.schema_migrations(version,checksum) VALUES($1,$2)`, m.version, checksum); err != nil {
			return databaseError("record schema migration", err)
		}
	}
	return databaseError("commit schema migration", tx.Commit(ctx))
}

// Verify the released 2.3 history once before replacing its obsolete entries by
// the baseline. Fingerprints retain integrity checking without shipping old SQL.
func consolidateSchemaBaseline(ctx context.Context, tx pgx.Tx, adopted bool) error {
	rows, err := tx.Query(ctx, `SELECT version,checksum FROM aiosources.schema_migrations WHERE version<=$1 ORDER BY version`, minimumSchemaVersion)
	if err != nil {
		return databaseError("read schema baseline", err)
	}
	defer rows.Close()
	var history strings.Builder
	count := 0
	baseline := false
	for rows.Next() {
		var version int
		var checksum string
		if err = rows.Scan(&version, &checksum); err != nil {
			return databaseError("read schema baseline", err)
		}
		count++
		if version != count && count != 1 {
			return errors.New("application schema migration history is incomplete")
		}
		if version == minimumSchemaVersion && checksum == schemaChecksum(initialSchema) {
			baseline = true
		}
		fmt.Fprintf(&history, "%d:%s\n", version, checksum)
	}
	if err = rows.Err(); err != nil {
		return databaseError("read schema baseline", err)
	}
	rows.Close()
	if baseline && count == 1 {
		return nil
	}
	expected := "4a927ac47f3ff42ad7f4a9c0c6637d8e1b567e9e6c02a06a400c76462cafdc5f"
	if adopted {
		expected = "e2ee592d791260240da387fcc2e113eabbbc13d69f596b3359e7632bbb72d244"
	}
	if count != minimumSchemaVersion || schemaChecksum(history.String()) != expected {
		return errors.New("application schema baseline checksum mismatch")
	}
	if _, err = tx.Exec(ctx, `DELETE FROM aiosources.schema_migrations WHERE version<$1`, minimumSchemaVersion); err != nil {
		return databaseError("consolidate schema baseline", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE aiosources.schema_migrations SET checksum=$2 WHERE version=$1`, minimumSchemaVersion, schemaChecksum(initialSchema)); err != nil {
		return databaseError("record schema baseline", err)
	}
	return nil
}
