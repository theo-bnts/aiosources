package store

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestEditionWorkLookupsPreserveAliasPathsAndDeduplicateKeys(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,years)
SELECT 'book','OL'||n||'W',ARRAY[2024] FROM generate_series(100,112) n;
INSERT INTO ingest.aio_works(media_type,tmdb_id,years) VALUES('movie','101',ARRAY[2024]);
INSERT INTO ingest.ol_redirects(source,target)
SELECT 'OL'||n||'W','OL'||(n+1)||'W' FROM generate_series(100,108) n;
INSERT INTO ingest.ol_redirects(source,target) VALUES('OL110W','OL111W'),('OL111W','OL110W');
INSERT INTO ingest.ol_editions(id,work_keys,title,normalized) VALUES
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
	projection := `SELECT e.id,w.openlibrary_id FROM ingest.ol_editions e `
	order := ` ORDER BY e.id,w.openlibrary_id`
	want := read(projection + `JOIN ingest.aio_works w ON w.media_type='book' AND e.work_keys && ingest.ol_aliases(w.openlibrary_id)` + order)
	got := read(projection + bookEditionWorks + order)
	if len(want) == 0 || !reflect.DeepEqual(want, got) {
		t.Fatalf("indexed edition lookups changed redirect membership: got %v, want %v", got, want)
	}
}
