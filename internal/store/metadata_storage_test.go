package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMetadataStorageEstimatesBreakDownEveryTypeWithoutScanningRows(t *testing.T) {
	ctx, db := sharingDatabase(t)

	// Populate representative works across all 5 metadata catalog types plus
	// dedicated per-catalog tables so both direct and pg_stats-apportioned
	// allocations are exercised.
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years)
 SELECT 'movie',n::text,'Synthetic Video '||n,ARRAY[2024] FROM generate_series(1,120) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years)
 SELECT 'game',n::text,'Synthetic Game '||n,ARRAY[2023] FROM generate_series(1,60) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,software_key,display_title,years,software_normalized)
 SELECT 'software','winget:vendor.app'||n,'Synthetic App '||n,'{}','synthetic app '||n FROM generate_series(1,80) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.sw_catalog(software_key,name,normalized,vendor,vendor_norm,part,description,sw_generation)
 SELECT 'winget:vendor.app'||n,'Synthetic App '||n,'synthetic app '||n,'Vendor','vendor','a',repeat('desc ',40),1 FROM generate_series(1,80) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,musicbrainz_id,display_title,years,music_normalized)
 SELECT 'music','00000000-0000-4000-8000-'||lpad(n::text,12,'0'),'Synthetic Album '||n,ARRAY[2022],'synthetic album '||n FROM generate_series(1,40) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,display_title,years,book_normalized)
 SELECT 'book','OL'||n||'W','Synthetic Book '||n,ARRAY[2021],'synthetic book '||n FROM generate_series(1,50) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized)
 SELECT 'OL'||n||'M',ARRAY['OL'||n||'W'],'Synthetic Book '||n,'synthetic book '||n FROM generate_series(1,50) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized)
 SELECT id,display_title,lower(display_title) FROM aiosources.aio_works`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `ANALYZE aiosources.aio_works`); err != nil {
		t.Fatal(err)
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)

	readCounts := func() []int64 {
		t.Helper()
		var counts []int64
		if err := tx.QueryRow(ctx, `SELECT ARRAY[
 pg_stat_get_xact_tuples_returned('aiosources.aio_works'::regclass),
 pg_stat_get_xact_tuples_returned('aiosources.aio_titles'::regclass),
 pg_stat_get_xact_tuples_returned('aiosources.sw_catalog'::regclass),
 pg_stat_get_xact_tuples_returned('aiosources.ol_editions'::regclass)
]`).Scan(&counts); err != nil {
			t.Fatal(err)
		}
		return counts
	}

	before := readCounts()
	estimates, err := metadataStorageEstimates(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	after := readCounts()
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("metadata storage estimates scanned catalog rows: %v -> %v", before, after)
		}
	}
	if len(estimates) != 5 {
		t.Fatalf("expected 5 metadata types, got %d: %+v", len(estimates), estimates)
	}

	byType := map[string]model.MetadataStorageEstimate{}
	for i, item := range estimates {
		if item.Bytes != item.TableBytes+item.IndexBytes {
			t.Fatalf("data+indexes mismatch for %s: %+v", item.Type, item)
		}
		if item.Bytes <= 0 || !item.Estimated {
			t.Fatalf("expected positive estimated storage for populated catalog %s: %+v", item.Type, item)
		}
		if i > 0 && estimates[i-1].Bytes < item.Bytes {
			t.Fatalf("metadata storage not sorted largest first: %+v", estimates)
		}
		byType[item.Type] = item
	}
	for _, want := range []string{"videos", "games", "software", "music", "books"} {
		if _, ok := byType[want]; !ok {
			t.Fatalf("missing metadata type %q in %+v", want, estimates)
		}
	}

	// Verify MeasureHealth surfaces MetadataStorage cleanly alongside Categories.
	report, err := db.MeasureHealth(ctx, model.HealthDisk{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.MetadataStorage) != 5 {
		t.Fatalf("MeasureHealth returned %d metadata storage entries, want 5: %+v", len(report.MetadataStorage), report.MetadataStorage)
	}
	// Ensure no synthetic titles leak anywhere in the estimate payload.
	if strings.Contains(fmt.Sprintf("%+v", report.MetadataStorage), "Synthetic") {
		t.Fatalf("metadata storage leaked record titles: %+v", report.MetadataStorage)
	}
}
