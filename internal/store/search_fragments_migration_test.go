package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTorrentFragmentSearchMigrationPreservesExistingCatalogue(t *testing.T) {
	ctx, db := sharingDatabase(t)
	// Restore the exact pre-upgrade vector function from its immutable migration.
	start := strings.Index(searchHistorySchema, "CREATE FUNCTION aiosources.torrent_search_vector")
	end := strings.Index(searchHistorySchema[start:], "ALTER TABLE aiosources.torrents") + start
	legacy := strings.Replace(searchHistorySchema[start:end], "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	if _, err := db.pool.Exec(ctx, legacy+`
DROP INDEX aiosources.torrents_fragments;
DROP FUNCTION aiosources.torrent_search_text(TEXT,TEXT,JSONB);
DELETE FROM aiosources.schema_migrations WHERE version=36;`); err != nil {
		t.Fatal(err)
	}
	for i, fields := range []map[string]any{
		{"title": "ExistingCatalogue.2024.WEB", "info_hash": strings.Repeat("a", 40), "size": json.Number("9007199254740993")},
		{"title": strings.Repeat("界", 50000) + " Tail", "info_hash": "hash", "download_url": "hidden-only-value"},
		{"title": []string{"unmapped"}, "info_hash": nil},
		{},
	} {
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, "fixture", string(rune('a'+i)), string(encoded), nil)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.pool.QueryRow(ctx, `SELECT jsonb_build_object(
            'torrents',(SELECT jsonb_agg(to_jsonb(t) ORDER BY provider_id,source_id) FROM aiosources.torrents t),
            'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM aiosources.torrent_history h),
            'journal',(SELECT jsonb_agg(to_jsonb(j) ORDER BY sequence) FROM aiosources.catalog_journal j),
            'migrations',(SELECT jsonb_agg(to_jsonb(m) ORDER BY version) FROM aiosources.schema_migrations m WHERE version<36)
        )::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	// Schema changes and the index are transactional: an interrupted upgrade
	// must leave no partial schema or catalogue changes behind.
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, searchFragmentsSchema); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var untouched bool
	if err := db.pool.QueryRow(ctx, `SELECT to_regprocedure('aiosources.torrent_search_text(text,text,jsonb)') IS NULL AND to_regclass('aiosources.torrents_fragments') IS NULL`).Scan(&untouched); err != nil || !untouched || before != snapshot() {
		t.Fatalf("interrupted migration changed retained data or left schema behind: %v", err)
	}
	for range 2 {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if before != snapshot() {
			t.Fatal("fragment migration changed published rows, history, journal or earlier checksums")
		}
		var sameDocument bool
		if err := db.pool.QueryRow(ctx, `SELECT bool_and(search_document=aiosources.torrent_search_vector(provider_id,source_id,fields) AND octet_length(aiosources.torrent_search_text(provider_id,source_id,fields))<=131072) FROM aiosources.torrents`).Scan(&sameDocument); err != nil || !sameDocument {
			t.Fatalf("whole-word and fragment search documents diverged: %v", err)
		}
	}
}

func TestTorrentFragmentSearchUsesIndex(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields)
        SELECT 'fixture',n::text,jsonb_build_object('title','Ordinary.Release.'||n) FROM generate_series(1,10000) n;
        INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','special','{"title":"Astronomical.Release"}');
        ANALYZE aiosources.torrents;`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := db.pool.QueryRow(ctx, `EXPLAIN (ANALYZE,FORMAT JSON) SELECT count(*) FROM aiosources.torrents WHERE aiosources.torrent_search_text(provider_id,source_id,fields) ILIKE $1`, "%ronomi%").Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, `"Index Name": "torrents_fragments"`) {
		t.Fatalf("selective fragment search did not use its index: %s", plan)
	}
}
