package store

import (
	"strings"

	"github.com/theo-bnts/aiosources/internal/books"
)

// Work dates are often absent from the export. A known edition can supply
// the first locally known publication year without rewriting every work.
const bookWorkYears = `CASE WHEN w.media_type='book' AND cardinality(w.years)=0 THEN
 ARRAY(SELECT min(year) FROM ingest.ol_editions e WHERE e.work_keys && ingest.ol_aliases(w.openlibrary_id) AND year>0 HAVING min(year) IS NOT NULL)
 ELSE w.years END`

func bookLanguageSQL(locale string) string { return `ARRAY[split_part(` + locale + `,'-',1)]` }

func bookEditionTitle(locale string) string {
	return `(SELECT e.title FROM ingest.ol_editions e WHERE w.media_type='book' AND e.work_keys && ingest.ol_aliases(w.openlibrary_id) AND e.languages && ` + bookLanguageSQL(locale) + ` ORDER BY e.id LIMIT 1)`
}

func expandBookCategories(cats []int) []int {
	out := append([]int{}, cats...)
	for _, c := range cats {
		if c == 7000 {
			out = append(out, 7020, 7030)
		}
		if c == 3000 {
			out = append(out, 3030)
		}
	}
	return out
}

// A Torznab book title identifies the work, which may have a different title
// in the source release. Locally known editions supply those translations.
func bookTitleSearchSQL(query string, bind func(any) string) string {
	raw := torrentTextSearchSQLFor("t", query, bind)
	work, edition := []string{}, []string{}
	for _, word := range strings.Fields(books.Normalize(query)) {
		value := bind("%" + escapeSearchFragment.Replace(word) + "%")
		work = append(work, `w.book_normalized LIKE `+value+` ESCAPE '\'`)
		edition = append(edition, `e.normalized LIKE `+value+` ESCAPE '\'`)
	}
	if len(work) == 0 {
		return raw
	}
	return `(` + raw + ` OR (` + strings.Join(work, " AND ") + `) OR EXISTS(
 SELECT 1 FROM ingest.ol_editions e WHERE e.work_keys && ingest.ol_aliases(w.openlibrary_id)
 AND ` + strings.Join(edition, " AND ") + `))`
}

// Both discovery and manual review search the same local work, edition,
// ISBN and author indexes. No search sends titles to a metadata API.
func bookCatalogCandidates(query, normalized, matching string, anchors ...string) string {
	combined := ""
	if len(anchors) > 0 && anchors[0] != "" {
		combined = bookCombinedCandidates(normalized, matching, anchors[0])
	}
	sql := ` UNION ALL SELECT id,CASE WHEN book_normalized=` + normalized + ` THEN 4 ELSE 1 END,public.similarity(` + normalized + `,book_normalized) FROM ingest.aio_works WHERE media_type='book' AND ` + strings.ReplaceAll(matching, "normalized ", "book_normalized ") + `
 UNION ALL SELECT id,5 AS precision,1::real AS relevance FROM ingest.aio_works WHERE openlibrary_id<>'' AND openlibrary_id=ingest.ol_resolve(` + query + `)
 UNION ALL SELECT w.id,5 AS precision,1::real AS relevance FROM ingest.ol_editions e JOIN ingest.aio_works w ON e.work_keys && ingest.ol_aliases(w.openlibrary_id) WHERE e.isbn @> ARRAY[` + query + `::text] OR e.id=ingest.ol_resolve(` + query + `)
 UNION ALL SELECT w.id,CASE WHEN e.normalized=` + normalized + ` THEN 4 ELSE 1 END,public.similarity(` + normalized + `,e.normalized)
 FROM ingest.ol_editions e JOIN ingest.aio_works w ON e.work_keys && ingest.ol_aliases(w.openlibrary_id) WHERE ` + strings.ReplaceAll(matching, "normalized ", "e.normalized ") + `
 UNION ALL SELECT w.id,1 AS precision,0.6::real AS relevance FROM ingest.aio_works w WHERE w.media_type='book' AND (w.book_data->'author_keys') ?| ARRAY(SELECT unnest(ingest.ol_aliases(id)) FROM ingest.ol_authors WHERE ` + matching + `)` + combined
	if len(anchors) > 1 && anchors[1] != "" {
		sql = strings.ReplaceAll(sql, "ingest.aio_works", anchors[1])
	}
	return sql
}

// Anchor the combined search on its longest word before joining authors and
// editions. Every word may occur in a title or author without concatenating
// or scanning the complete bibliographic catalog.
func bookCombinedCandidates(normalized, matching, anchor string) string {
	return ` UNION ALL SELECT id,2 AS precision,public.similarity(` + normalized + `,normalized) FROM (
 WITH author_keys AS MATERIALIZED(SELECT ARRAY(SELECT unnest(ingest.ol_aliases(id)) FROM ingest.ol_authors WHERE normalized LIKE ` + anchor + `) AS ids),
 author_works AS MATERIALIZED(SELECT id,openlibrary_id,book_normalized FROM ingest.aio_works w WHERE media_type='book' AND (book_data->'author_keys') ?| (SELECT ids FROM author_keys)),
 aliases AS (
 SELECT id,book_normalized AS title FROM ingest.aio_works WHERE media_type='book' AND book_normalized LIKE ` + anchor + `
 UNION SELECT id,book_normalized FROM author_works
 UNION SELECT w.id,e.normalized FROM ingest.ol_editions e JOIN ingest.aio_works w ON e.work_keys && ingest.ol_aliases(w.openlibrary_id) WHERE e.normalized LIKE ` + anchor + `
 UNION SELECT w.id,e.normalized FROM author_works w JOIN ingest.ol_editions e ON e.work_keys && ingest.ol_aliases(w.openlibrary_id)
 ) SELECT aliases.id,aliases.title||' '||COALESCE((SELECT string_agg(a.normalized,' ') FROM ingest.ol_authors a WHERE a.id IN(SELECT ingest.ol_resolve(jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]'))))),'') AS normalized
 FROM aliases JOIN ingest.aio_works w ON w.id=aliases.id
 ) combined_books WHERE ` + matching
}
