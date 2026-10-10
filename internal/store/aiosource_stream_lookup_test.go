package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNativeMALLookupUsesTheSameAssertionsAsTheRecordDecoder(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, fields := range []string{
		`{"mal_id":"21"}`, `{"mal_id":21}`, `{"mal":"21"}`,
		`{"external_ids":{"mal":"21"}}`, `{"external_ids":{"mal_id":21}}`,
		`{"attributes":{"malid":["21"]}}`, `{"attributes":{"mal":["21"]}}`,
		`{"attributes":{"malid":"21"}}`, `{"attributes":{"mal":21}}`,
		`{"mal_id":"","external_ids":{"mal":"21"}}`,
		`{"mal_id":"00021","attributes":{"malid":["21"]}}`,
		`{"mal_id":["21","21"]}`, `{"mal_id":[["21"]]}`,
		`{"mal_id":"21","external_ids":{"mal":"22"}}`,
		`{"mal_id":"21","attributes":{"malid":["21","22"]}}`,
		`{"mal_id":"21","attributes":{"malid":["garbage"]}}`,
		`{"mal_id":null}`, `{"mal_id":0}`, `{"mal_id":"21.0"}`,
		`{"mal_id":"0","external_ids":{"mal":"21"}}`,
		`{"mal_id":null,"external_ids":{"mal":"21"}}`,
		`{"mal_id":{},"external_ids":{"mal":"21"}}`,
		`{"mal_id":[null,"21"]}`, `{"mal_id":["00021","21"]}`,
	} {
		t.Run(fields, func(t *testing.T) {
			native, err := scanAIORecord(aioFakeRow{fields})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.torrents`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','1',$1)`, json.RawMessage(fields)); err != nil {
				t.Fatal(err)
			}
			rows, err := db.AIOStreams(ctx, "mal", "21", "anime")
			want := 0
			if native.MAL == "21" {
				want = 1
			}
			if err != nil || len(rows) != want {
				t.Fatalf("lookup returned %d rows, want %d from decoded assertion %q: %v", len(rows), want, native.MAL, err)
			}
		})
	}
}

func TestNativeMALIndexUpgradePreservesPublishedDataAndQueueVersions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
VALUES('example','1','{"mal_id":"21","title":"Example.Anime - 03"}');
UPDATE ingest.aio_match_queue SET version=9;
DROP INDEX ingest.aio_torrents_native_mal;
DROP FUNCTION ingest.aio_native_mal_candidates(JSONB);
DELETE FROM ingest.schema_migrations WHERE version=35;`); err != nil {
		t.Fatal(err)
	}
	before, err := db.AIOTorrent(ctx, "example", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := db.AIOTorrent(ctx, "example", "1")
	if err != nil || before.Fingerprint != after.Fingerprint || after.MAL != "21" {
		t.Fatal("native fields changed during index upgrade", before, after, err)
	}
	var version int
	if err := db.pool.QueryRow(ctx, `SELECT version FROM ingest.aio_match_queue WHERE provider_id='example' AND source_id='1'`).Scan(&version); err != nil || version != 9 {
		t.Fatal("upgrade changed the existing queue version", version, err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal("upgrade is not idempotent", err)
	}
}

func BenchmarkNativeMALLookup(b *testing.B) {
	ctx, db := sharingDatabase(b)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields)
SELECT 'example',n::text,jsonb_build_object('title','Example.Anime - 03','mal_id',(n+100)::text)
FROM generate_series(1,100000) n;
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','target','{"title":"Example.Anime - 03","mal_id":"21"}');
ANALYZE ingest.torrents;`); err != nil {
		b.Fatal(err)
	}
	b.Run("previous", func(b *testing.B) {
		for b.Loop() {
			rows, err := db.pool.Query(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t WHERE regexp_replace(COALESCE(t.fields->>'mal_id',t.fields#>>'{external_ids,mal}',t.fields#>>'{external_ids,mal_id}',t.fields#>>'{attributes,malid,0}'),'^0+','')=$1 ORDER BY t.provider_id,t.source_id`, "21")
			out, err := collectAIORecords(rows, err, false)
			if err != nil || len(out) != 1 {
				b.Fatal("previous lookup fixture", out, err)
			}
		}
	})
	b.Run("indexed", func(b *testing.B) {
		for b.Loop() {
			out, err := db.AIOStreams(ctx, "mal", "21", "anime")
			if err != nil || len(out) != 1 {
				b.Fatal("indexed lookup fixture", out, err)
			}
		}
	})
}

func TestStremioLookupReturnsNewestPublishedReleasesFirst(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for i, date := range []string{"2021-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2024-01-01T00:00:00Z"} {
		fields := fmt.Sprintf(`{"mal_id":"21","title":"Example.Anime - 03","info_hash":"%s","published_at":"%s"}`, strings.Repeat("a", 40), date)
		if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example',$1,$2)`, fmt.Sprint(i), json.RawMessage(fields)); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.AIOStreams(ctx, "mal", "21", "anime")
	if err != nil || len(rows) != 3 || rows[0].SourceID != "1" || rows[1].SourceID != "2" || rows[2].SourceID != "0" {
		t.Fatalf("release order %+v %v", rows, err)
	}
}
