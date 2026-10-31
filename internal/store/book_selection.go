package store

import (
	"context"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

// Build the optional local-title safety net once per bulk import, not once per
// edition. This reads published records only and retains bibliographic keys.
func (s *Store) BookImportSelection(ctx context.Context, config model.AIOConfig) (books.ImportSelection, error) {
	selection := books.ImportSelection{RelevantOnly: config.OpenLibraryRelevantOnly, Languages: config.OpenLibraryLanguages}
	if !selection.RelevantOnly {
		return selection, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+aioRecordColumns+` FROM ingest.torrents t`)
	if err != nil {
		return selection, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanAIORecord(rows)
		if err != nil {
			return selection, err
		}
		selection.Include(books.ParseRecord(record))
	}
	if err = rows.Err(); err != nil {
		return selection, err
	}
	rows.Close()
	// A manual book choice is explicit local evidence even when an opaque source
	// filename has no book extension or category that the parser can recognize.
	rows, err = s.pool.Query(ctx, `SELECT openlibrary_id FROM ingest.aio_match_reviews WHERE active AND media_type='book' AND openlibrary_id<>''
UNION SELECT ingest.ol_resolve(openlibrary_id) FROM ingest.aio_match_reviews WHERE active AND media_type='book' AND openlibrary_id<>''`)
	if err != nil {
		return selection, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return selection, err
		}
		if selection.Identifiers == nil {
			selection.Identifiers = map[string]bool{}
		}
		selection.Identifiers[id] = true
	}
	return selection, rows.Err()
}
