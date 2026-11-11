package store

import "strings"

// Search only discovers works represented in the local source catalog. Resolve
// their editions before applying text predicates: joining globally matching
// editions back to these few works can traverse millions of unrelated entries.
const catalogBookScope = `WITH catalog_matched_ids AS MATERIALIZED(
 SELECT DISTINCT work_id AS id FROM aiosources.aio_matches WHERE status='matched' AND work_id IS NOT NULL),
catalog_book_ids AS MATERIALIZED(
 SELECT w.id FROM catalog_matched_ids m JOIN aiosources.aio_works w ON w.id=m.id WHERE w.media_type='book'),
catalog_books AS MATERIALIZED(
 SELECT w.id,w.openlibrary_id,w.book_normalized,w.book_data,
 ARRAY(SELECT unnest(aiosources.ol_aliases(w.openlibrary_id))) AS aliases
 FROM aiosources.aio_works w JOIN catalog_book_ids b ON b.id=w.id),
catalog_book_editions AS MATERIALIZED(
 SELECT w.id AS work_id,e.id,e.normalized,e.isbn FROM catalog_books w JOIN LATERAL (
 WITH editions AS MATERIALIZED(SELECT e.id,e.normalized,e.isbn FROM aiosources.ol_editions e WHERE e.work_keys && w.aliases)
 SELECT * FROM editions) e ON TRUE),
catalog_book_authors AS MATERIALIZED(
 WITH RECURSIVE keys(work_id,id,seen) AS (
 SELECT w.id,key,ARRAY[key] FROM catalog_books w,
 jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]')) key
 UNION ALL SELECT k.work_id,r.target,k.seen||r.target FROM keys k JOIN aiosources.ol_redirects r ON r.source=k.id
 WHERE cardinality(k.seen)<8 AND NOT r.target=ANY(k.seen)),
 authors AS MATERIALIZED(SELECT id,normalized FROM aiosources.ol_authors WHERE id=ANY(ARRAY(SELECT DISTINCT id FROM keys)))
 SELECT DISTINCT k.work_id,a.id,a.normalized FROM keys k JOIN authors a ON a.id=k.id) `

func catalogBookCandidates(query, normalized, matching, anchor string) string {
	workMatching := strings.ReplaceAll(matching, "normalized ", "book_normalized ")
	// This input is identical for every candidate. An InitPlan resolves it once,
	// instead of repeating the recursive lookup for each locally known edition.
	identity := `(SELECT aiosources.ol_resolve(` + query + `))`
	sql := ` UNION ALL SELECT id,CASE WHEN book_normalized=` + normalized + ` THEN 4 ELSE 1 END,public.similarity(` + normalized + `,book_normalized)
 FROM catalog_books WHERE ` + workMatching + `
 UNION ALL SELECT id,5,1::real FROM catalog_books WHERE openlibrary_id<>'' AND openlibrary_id=` + identity + `
 UNION ALL SELECT work_id,5,1::real FROM catalog_book_editions WHERE isbn @> ARRAY[` + query + `::text] OR id=` + identity + `
 UNION ALL SELECT work_id,CASE WHEN normalized=` + normalized + ` THEN 4 ELSE 1 END,public.similarity(` + normalized + `,normalized)
 FROM catalog_book_editions WHERE ` + matching + `
 UNION ALL SELECT work_id,1,0.6::real FROM catalog_book_authors WHERE ` + matching
	if anchor != "" {
		// Preserve combined title/author ranking and the same longest-word
		// anchor used by full-catalog review, without a global edition scan.
		sql += ` UNION ALL SELECT id,2,public.similarity(` + normalized + `,normalized) FROM (
 WITH author_labels AS MATERIALIZED(
 SELECT w.id,COALESCE((SELECT string_agg(a.normalized,' ') FROM aiosources.ol_authors a WHERE a.id=ANY(
 ARRAY(SELECT aiosources.ol_resolve(key) FROM jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]')) key))), '') AS normalized
 FROM catalog_books w),
 aliases AS (
 SELECT id,book_normalized AS title FROM catalog_books w WHERE book_normalized LIKE ` + anchor + `
 OR EXISTS(SELECT 1 FROM catalog_book_authors a WHERE a.work_id=w.id AND a.normalized LIKE ` + anchor + `)
 UNION SELECT e.work_id,e.normalized FROM catalog_book_editions e WHERE e.normalized LIKE ` + anchor + `
 OR EXISTS(SELECT 1 FROM catalog_book_authors a WHERE a.work_id=e.work_id AND a.normalized LIKE ` + anchor + `))
 SELECT aliases.id,aliases.title||' '||a.normalized AS normalized FROM aliases JOIN author_labels a USING(id)
 ) combined_books WHERE ` + matching
	}
	return sql
}
