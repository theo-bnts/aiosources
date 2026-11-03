package store

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

func matchedBookKind(record model.AIORecord, work *model.AIOWork) string {
	if work != nil && work.Type != "book" {
		return ""
	}
	kind := books.ParseRecord(record).Kind
	if kind == "" && work != nil && work.Type == "book" {
		return "book"
	}
	return kind
}

// Discover small bibliographic evidence rows before loading covers, years and
// aliases. Author/title fragments count toward the limit only when the same
// book corroborates the remaining author words. Unrelated short titles must
// not hide a reliable match behind an artificial candidate overflow.
const bookCandidateEvidenceSQL = `WITH keys AS MATERIALIZED (
 SELECT openlibrary_id AS key,book_normalized AS normalized,'{}'::text[] AS author_keys,TRUE AS reference,FALSE AS part
 FROM ingest.aio_works WHERE $3<>'' AND openlibrary_id<>'' AND openlibrary_id=ingest.ol_resolve($3)
 UNION ALL SELECT unnest(work_keys),normalized,author_keys,TRUE,FALSE FROM ingest.ol_editions WHERE $3<>'' AND id=ingest.ol_resolve($3)
 UNION ALL SELECT unnest(work_keys),normalized,author_keys,TRUE,FALSE FROM ingest.ol_editions WHERE $2<>'' AND isbn @> ARRAY[$2::text]
 UNION ALL SELECT openlibrary_id,book_normalized,'{}'::text[],FALSE,FALSE FROM ingest.aio_works
 WHERE $2='' AND $3='' AND media_type='book' AND (book_normalized=ANY($5::text[]) OR
 (NOT $4 AND book_normalized OPERATOR(public.%) $1 AND cardinality(string_to_array(book_normalized,' '))=ANY($7::int[])))
 UNION ALL SELECT unnest(work_keys),normalized,author_keys,FALSE,FALSE FROM ingest.ol_editions
 WHERE $2='' AND $3='' AND (normalized=ANY($5::text[]) OR
 (NOT $4 AND normalized OPERATOR(public.%) $1 AND cardinality(string_to_array(normalized,' '))=ANY($7::int[])))
 UNION ALL SELECT openlibrary_id,book_normalized,'{}'::text[],FALSE,TRUE FROM ingest.aio_works
 WHERE $2='' AND $3='' AND media_type='book' AND book_normalized=ANY($6::text[])
 UNION ALL SELECT unnest(work_keys),normalized,author_keys,FALSE,TRUE FROM ingest.ol_editions
 WHERE $2='' AND $3='' AND normalized=ANY($6::text[])
), resolved AS MATERIALIZED (
 SELECT DISTINCT ingest.ol_resolve(key) AS key,normalized,author_keys,reference,part FROM keys
), candidates AS MATERIALIZED (
 SELECT w.id,k.normalized,k.reference,k.part,ARRAY(
 SELECT DISTINCT key FROM (
 SELECT jsonb_array_elements_text(COALESCE(w.book_data->'author_keys','[]')) AS key
 UNION ALL SELECT unnest(k.author_keys)) author_keys ORDER BY key) AS author_keys
 FROM resolved k JOIN ingest.aio_works w ON w.media_type='book' AND w.openlibrary_id<>'' AND w.openlibrary_id=k.key
 WHERE NOT w.adult
 AND ($2='' OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.work_keys && ARRAY(SELECT unnest(ingest.ol_aliases(w.openlibrary_id))) AND e.isbn @> ARRAY[$2::text]))
 AND ($3='' OR w.openlibrary_id=ingest.ol_resolve($3) OR EXISTS(SELECT 1 FROM ingest.ol_editions e WHERE e.id=ingest.ol_resolve($3) AND e.work_keys && ARRAY(SELECT unnest(ingest.ol_aliases(w.openlibrary_id)))))
)
SELECT c.id,c.normalized,c.reference,c.part,ARRAY(
 WITH authors AS MATERIALIZED(
 SELECT a.name,a.aliases FROM ingest.ol_authors a WHERE a.id=ANY(ARRAY(SELECT ingest.ol_resolve(key) FROM unnest(c.author_keys) key)))
 SELECT name FROM authors UNION ALL (SELECT DISTINCT unnest(aliases) FROM authors LIMIT 200))
FROM candidates c ORDER BY c.id,c.part,c.normalized`

func bookCandidateTitles(f model.AIOCandidateFilter) (titles, parts []string, words []int) {
	titles, parts, words = []string{}, []string{}, []int{}
	for _, title := range append(slices.Clone(f.Titles), f.Title) {
		if !slices.Contains(titles, title) {
			titles = append(titles, title)
		}
		count := len(strings.Fields(title))
		if !slices.Contains(words, count) {
			words = append(words, count)
		}
	}
	for _, part := range f.BookTitleParts {
		if !slices.Contains(parts, part.Title) {
			parts = append(parts, part.Title)
		}
	}
	return
}

func discoverBookCandidates(ctx context.Context, tx pgx.Tx, f model.AIOCandidateFilter) ([]int64, bool, error) {
	titles, parts, words := bookCandidateTitles(f)
	rows, err := tx.Query(ctx, bookCandidateEvidenceSQL, f.Title, f.ISBN, f.OpenLibrary, f.ExactOnly, titles, parts, words)
	if err != nil {
		return nil, false, databaseError("find book candidate evidence", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var title string
		var reference, part bool
		var authors []string
		if err = rows.Scan(&id, &title, &reference, &part, &authors); err != nil {
			rows.Close()
			return nil, false, databaseError("read book candidate evidence", err)
		}
		if slices.Contains(ids, id) {
			continue
		}
		if f.BookAuthor != "" && !slices.ContainsFunc(authors, func(author string) bool {
			return books.AuthorSignature(author) == books.AuthorSignature(f.BookAuthor)
		}) {
			continue
		}
		if part {
			corroborated := slices.ContainsFunc(f.BookTitleParts, func(pair model.BookTitlePart) bool {
				return pair.Title == title && slices.ContainsFunc(authors, func(author string) bool {
					return books.Normalize(author) == pair.Author
				})
			})
			if !corroborated {
				continue
			}
		} else if !reference && f.BookMinimumScore > 0 {
			score := 0.0
			for _, input := range titles {
				score = max(score, books.TitleScore(input, title))
			}
			if score < f.BookMinimumScore {
				continue
			}
		}
		ids = append(ids, id)
		if len(ids) > aioCandidateLimit {
			break
		}
	}
	rows.Close()
	return ids, len(ids) > aioCandidateLimit, databaseError("find book candidate evidence", rows.Err())
}

func searchBookCandidates(ctx context.Context, tx pgx.Tx, f model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
	ids, overflow, err := discoverBookCandidates(ctx, tx, f)
	if err != nil || len(ids) == 0 {
		return []model.AIOWork{}, overflow, err
	}
	rows, err := tx.Query(ctx, `SELECT `+aioWorkColumns+` FROM ingest.aio_works w WHERE w.id=ANY($1::bigint[]) ORDER BY w.id`, ids)
	if err != nil {
		return nil, false, databaseError("find book candidates", err)
	}
	out := []model.AIOWork{}
	for rows.Next() {
		w, e := scanAIOWork(rows)
		if e != nil {
			rows.Close()
			return nil, false, e
		}
		out = append(out, w)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, false, databaseError("find book candidates", rows.Err())
	}
	titles, parts, _ := bookCandidateTitles(f)
	titles = append(titles, parts...)
	for i := range out {
		w := &out[i]
		w.Book.YearVerified = f.Year != nil && slices.Contains(w.Years, *f.Year)
		// Select this work's editions before ordering, and establish year
		// evidence before limiting display aliases for very popular works.
		aliases, err := tx.Query(ctx, `WITH editions AS MATERIALIZED(
 SELECT e.id,e.title,e.normalized,e.year,e.author_keys FROM ingest.ol_editions e
 WHERE e.work_keys && ARRAY(SELECT unnest(ingest.ol_aliases($1)))
 AND (e.normalized=ANY($2::text[]) OR e.normalized OPERATOR(public.%) $3 OR ($4::int IS NOT NULL AND e.year=$4)))
SELECT e.title,e.year,ARRAY(SELECT name FROM ingest.ol_authors WHERE id=ANY(ARRAY(SELECT ingest.ol_resolve(key) FROM unnest(e.author_keys) key))),
 COALESCE(bool_or(e.year=$4) OVER(),FALSE)
FROM editions e ORDER BY (e.normalized=ANY($2::text[])) DESC,e.id LIMIT 101`, w.OpenLibrary, titles, f.Title, f.Year)
		if err != nil {
			return nil, false, databaseError("find book edition evidence", err)
		}
		for aliases.Next() {
			var title string
			var year int
			var authors []string
			var verified bool
			if err = aliases.Scan(&title, &year, &authors, &verified); err != nil {
				aliases.Close()
				return nil, false, err
			}
			if !slices.Contains(w.Titles, title) {
				w.Titles = append(w.Titles, title)
			}
			w.Book.YearVerified = w.Book.YearVerified || verified
			for _, author := range authors {
				if !slices.Contains(w.Book.Authors, author) {
					w.Book.Authors = append(w.Book.Authors, author)
				}
			}
		}
		aliases.Close()
		if aliases.Err() != nil {
			return nil, false, aliases.Err()
		}
	}
	return out, overflow, nil
}
