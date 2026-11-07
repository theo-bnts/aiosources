package store

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestCatalogBookScopePreservesEditionAuthorAndRedirectCandidates(t *testing.T) {
	ctx, db := sharingDatabase(t)
	_, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_works(media_type,openlibrary_id,book_normalized,book_data,years)
SELECT 'book','OL'||n||'W','example orchard',jsonb_build_object('author_keys',ARRAY['OL400A']),ARRAY[2024] FROM generate_series(100,112) n;
INSERT INTO ingest.ol_redirects(source,target)
SELECT 'OL'||n||'W','OL'||(n+1)||'W' FROM generate_series(100,108) n;
INSERT INTO ingest.ol_redirects(source,target) VALUES('OL110W','OL111W'),('OL111W','OL110W'),('OL400A','OL401A'),('OL401A','OL402A'),('OL402A','OL401A');
INSERT INTO ingest.ol_authors(id,name,normalized) VALUES('OL400A','Old Author','old author'),('OL401A','Morgan Vale','morgan vale'),('OL402A','Other Author','other author');
INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,isbn) VALUES
('OL200M',ARRAY['OL100W'],'The Amber Orchard','the amber orchard',ARRAY['9780306406157']),
('OL201M',ARRAY['OL104W','OL106W','OL104W'],'The Silver Field','the silver field','{}'),
('OL202M',ARRAY['OL110W'],'Example cycle edition','example cycle edition','{}'),
('OL203M',ARRAY['OL112W','OL999W'],'Unpublished book','unpublished book','{}');`)
	if err != nil {
		t.Fatal(err)
	}
	for n := 100; n < 112; n++ {
		publishSharingRow(t, ctx, db, "example", strconv.Itoa(n), `{"title":"Example Orchard EPUB"}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM ingest.torrents t JOIN ingest.aio_works w ON w.openlibrary_id='OL'||t.source_id||'W';DELETE FROM ingest.aio_match_queue;`); err != nil {
		t.Fatal(err)
	}
	read := func(query string, args []any) [][]any {
		t.Helper()
		rows, err := db.pool.Query(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		values, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([]any, error) { return row.Values() })
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	for _, query := range []string{"example", "amber", "silver field", "old author", "morgan vale", "other author", "orchard morgan", "morgan silver field", "cycle", "unpublished", "OL200M", "OL104W", "9780306406157"} {
		args := []any{query, strings.ToLower(query)}
		terms := []string{}
		anchor := ""
		longest := 0
		for _, word := range strings.Fields(strings.ToLower(query)) {
			args = append(args, "%"+word+"%")
			bind := "$" + strconv.Itoa(len(args))
			terms = append(terms, "normalized LIKE "+bind)
			if len(word) > longest {
				longest, anchor = len(word), bind
			}
		}
		if len(terms) < 2 || longest < 3 {
			anchor = ""
		}
		matching := strings.Join(terms, " AND ")
		project := func(candidates string) string {
			return catalogBookScope + `SELECT id,max(precision),max(relevance) FROM (` + strings.TrimPrefix(candidates, " UNION ALL ") + `) candidates(id,precision,relevance)
 WHERE id IN (SELECT id FROM catalog_book_ids) GROUP BY id ORDER BY id`
		}
		want := read(project(bookCatalogCandidates("$1", "$2", matching, anchor)), args)
		got := read(project(catalogBookCandidates("$1", "$2", matching, anchor)), args)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("catalog scope changed candidates or ranking for %q: got %v, want %v", query, got, want)
		}
	}
}
