package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const applicationSchema = "aiosources"

// These identifiers are retained only to upgrade existing installations. The
// application itself always uses applicationSchema after a successful migration.
const legacySchema = "ingest"
const legacySchemaVersion = 92
const legacySchemaGuard = "application-data-moved-to-aiosources"

func legacyMigrationSQL(sql string) string {
	return strings.NewReplacer(
		applicationSchema+".", legacySchema+".",
		"'"+applicationSchema+"'", "'"+legacySchema+"'",
		applicationSchema+"_run_changes", legacySchema+"_run_changes",
	).Replace(sql)
}

func legacyMigrationChecksum(sql string) string {
	sum := sha256.Sum256([]byte(legacyMigrationSQL(sql)))
	return hex.EncodeToString(sum[:])
}

// adoptApplicationSchema moves only catalog metadata, never torrent contents or
// indexes. Everything, including stored function bodies and migration checksums,
// is committed in the same migration transaction or rolled back together.
func adoptApplicationSchema(ctx context.Context, tx pgx.Tx) (bool, error) {
	var legacy, current bool
	if err := tx.QueryRow(ctx, `SELECT to_regnamespace($1) IS NOT NULL,to_regnamespace($2) IS NOT NULL`, legacySchema, applicationSchema).Scan(&legacy, &current); err != nil {
		return false, err
	}
	if !legacy {
		return false, nil
	}
	var metadata bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, legacySchema+".schema_migrations").Scan(&metadata); err != nil {
		return false, err
	}
	if !metadata {
		return false, errors.New("a legacy-named database schema has no application migration metadata")
	}
	if current {
		var guard bool
		if err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(version=1 AND checksum=$1) FROM ingest.schema_migrations)
 AND NOT EXISTS(SELECT 1 FROM pg_class WHERE relnamespace=$2::regnamespace AND relkind IN ('r','p','v','m','S') AND relname<>'schema_migrations')
 AND NOT EXISTS(SELECT 1 FROM pg_proc WHERE pronamespace=$2::regnamespace)`, legacySchemaGuard, legacySchema).Scan(&guard); err != nil {
			return false, err
		}
		if !guard {
			return false, errors.New("both application database namespaces contain data; refusing to merge them")
		}
		return false, nil
	}
	var installed int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM ingest.schema_migrations`).Scan(&installed); err != nil {
		return false, err
	}
	if installed < 1 || installed > legacySchemaVersion {
		return false, errors.New("the legacy application database version is not supported by this upgrade")
	}
	if _, err := tx.Exec(ctx, `ALTER SCHEMA ingest RENAME TO aiosources`); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT pg_get_functiondef(p.oid)
 FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
 WHERE p.pronamespace='aiosources'::regnamespace AND p.prokind IN ('f','p') AND l.lanname IN ('sql','plpgsql')
 ORDER BY p.oid`)
	if err != nil {
		return false, err
	}
	definitions := []string{}
	for rows.Next() {
		var definition string
		if err = rows.Scan(&definition); err != nil {
			rows.Close()
			return false, err
		}
		definitions = append(definitions, definition)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	rewrite := strings.NewReplacer(
		legacySchema+".", applicationSchema+".",
		`"`+legacySchema+`".`, `"`+applicationSchema+`".`,
		"'"+legacySchema+"'", "'"+applicationSchema+"'",
		legacySchema+"_run_changes", applicationSchema+"_run_changes",
	)
	for _, definition := range definitions {
		updated := rewrite.Replace(definition)
		if updated != definition {
			if _, err := tx.Exec(ctx, updated); err != nil {
				return false, err
			}
		}
	}
	// Older binaries otherwise create an empty, apparently fresh database after
	// the rename. This metadata-only guard makes their checksum check fail closed.
	if _, err := tx.Exec(ctx, `CREATE SCHEMA ingest;
 CREATE TABLE ingest.schema_migrations(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT now());
 INSERT INTO ingest.schema_migrations(version,checksum) VALUES(1,'application-data-moved-to-aiosources')`); err != nil {
		return false, err
	}
	return true, nil
}

// The distributed source lease must keep its original key across upgrades so
// a previous worker and its replacement cannot collect the same source at once.
const providerLeaseDomain = "scraper." + legacySchema + ".provider.v1\x00"
