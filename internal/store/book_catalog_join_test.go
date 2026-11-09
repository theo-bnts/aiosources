package store

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEditionWorkLookupsPreserveAliasPathsAndDeduplicateKeys(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,years)
SELECT 'book','OL'||n||'W',ARRAY[2024] FROM generate_series(100,112) n;
INSERT INTO aiosources.aio_works(media_type,tmdb_id,years) VALUES('movie','101',ARRAY[2024]);
INSERT INTO aiosources.ol_redirects(source,target)
SELECT 'OL'||n||'W','OL'||(n+1)||'W' FROM generate_series(100,108) n;
INSERT INTO aiosources.ol_redirects(source,target) VALUES('OL110W','OL111W'),('OL111W','OL110W');
INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized) VALUES
('OL200M',ARRAY['OL100W'],'Example first edition','example first edition'),
('OL201M',ARRAY['OL104W','OL106W','OL104W'],'Example shared edition','example shared edition'),
('OL202M',ARRAY['OL110W'],'Example cycle edition','example cycle edition'),
('OL203M',ARRAY['OL112W','OL999W'],'Example separate edition','example separate edition');`)
	if err != nil {
		t.Fatal(err)
	}
	read := func(query string) [][]any {
		t.Helper()
		rows, err := db.pool.Query(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		values, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]any, error) { return row.Values() })
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	projection := `SELECT e.id,w.openlibrary_id FROM aiosources.ol_editions e `
	order := ` ORDER BY e.id,w.openlibrary_id`
	want := read(projection + `JOIN aiosources.aio_works w ON w.media_type='book' AND e.work_keys && aiosources.ol_aliases(w.openlibrary_id)` + order)
	got := read(projection + bookEditionWorks + order)
	if len(want) == 0 || !reflect.DeepEqual(want, got) {
		t.Fatalf("indexed edition lookups changed redirect membership: got %v, want %v", got, want)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE aiosources.ol_editions SET languages=ARRAY['fr'],year=2001,cover=123;
UPDATE aiosources.ol_editions SET languages=ARRAY['en'],year=1999,cover=456 WHERE id='OL201M';
UPDATE aiosources.aio_works SET years='{}' WHERE media_type='book'`); err != nil {
		t.Fatal(err)
	}
	oldTitle := `(SELECT e.title FROM aiosources.ol_editions e WHERE w.media_type='book' AND e.work_keys && aiosources.ol_aliases(w.openlibrary_id) AND e.languages && ARRAY['fr'] ORDER BY e.id LIMIT 1)`
	oldCover := `(SELECT e.cover FROM aiosources.ol_editions e WHERE w.media_type='book' AND e.work_keys && aiosources.ol_aliases(w.openlibrary_id) AND e.cover>0 ORDER BY e.id LIMIT 1)`
	oldYears := `CASE WHEN w.media_type='book' AND cardinality(w.years)=0 THEN ARRAY(SELECT min(year) FROM aiosources.ol_editions e WHERE e.work_keys && aiosources.ol_aliases(w.openlibrary_id) AND year>0 HAVING min(year) IS NOT NULL) ELSE w.years END`
	want = read(`SELECT w.id,` + oldTitle + `,` + oldCover + `,` + oldYears + ` FROM aiosources.aio_works w ORDER BY w.id`)
	got = read(`SELECT w.id,` + bookEditionTitle("'fr'") + `,` + bookEditionCover + `,` + bookWorkYears + ` FROM aiosources.aio_works w ORDER BY w.id`)
	if !reflect.DeepEqual(want, got) {
		t.Fatal("edition lookup optimization changed title, cover, date or redirect resolution")
	}
}
