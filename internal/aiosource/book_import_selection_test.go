package aiosource

import (
	"errors"
	"github.com/theo-bnts/aiosources/internal/model"
	"strings"
	"testing"
	"time"
)

func TestSelectedBookChangesImportAnEditionThatBecomesDigitalAfterExclusion(t *testing.T) {
	ctx, s, conn := testService(t)
	f := selectedBookFixture(t, s)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.OpenLibraryRelevantOnly = true
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var excluded bool
	if err = conn.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM ingest.ol_editions WHERE id='OL900M')`).Scan(&excluded); err != nil || !excluded {
		t.Fatal("print-only edition was not initially excluded", err)
	}
	f.mu.Lock()
	initialExports := f.dumpCalls
	f.documents["OL900M"] = bookAPIRecord("OL900M", 2, map[string]any{"title": "Unrelated Printed Work", "physical_format": "EPUB", "works": []any{map[string]any{"key": "/works/OL800W"}}})
	f.documents["OL800W"] = f.dumpDocuments["OL800W"]
	f.rows = append([]bookAPIEvent{{ID: "103", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/books/OL900M", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil || state.Works != 2 || state.Editions != 3 || state.Pending != 0 {
		t.Fatal("change feed did not admit the newly digital edition and its book", state, err)
	}
	f.mu.Lock()
	exports := f.dumpCalls
	f.mu.Unlock()
	if exports != initialExports {
		t.Fatal("newly digital edition required another catalog export", initialExports, exports)
	}
}

func TestSelectedBookChangesRemoveExcludedEditionsAndDiscoverNewDependencies(t *testing.T) {
	ctx, s, conn := testService(t)
	f := selectedBookFixture(t, s)
	c, _ := s.DB.AIOConfig(ctx)
	c.OpenLibraryRelevantOnly = true
	c.OpenLibraryLanguages = []string{"fr"}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.documents["OL301M"] = bookAPIRecord("OL301M", 2, map[string]any{"title": "Synthetic Translation", "physical_format": "EPUB", "languages": []any{map[string]any{"key": "/languages/ger"}}, "works": []any{map[string]any{"key": "/works/OL100W"}}})
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/books/OL301M", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, _ := s.DB.OpenLibraryStatus(ctx)
	if state.Works != 0 || state.Editions != 0 {
		t.Fatal("newly excluded edition retained its book", state)
	}
	f.mu.Lock()
	f.documents["OL222M"] = bookAPIRecord("OL222M", 1, map[string]any{"title": "The Azure Almanac", "physical_format": "EPUB", "languages": []any{map[string]any{"key": "/languages/fre"}}, "works": []any{map[string]any{"key": "/works/OL500W"}}, "authors": []any{map[string]any{"key": "/authors/OL600A"}}})
	f.documents["OL500W"] = f.dumpDocuments["OL500W"]
	f.documents["OL600A"] = f.dumpDocuments["OL600A"]
	f.rows = append([]bookAPIEvent{{ID: "102", Timestamp: f.time.Add(90 * time.Minute).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/books/OL222M", "revision": 1}, {"key": "/works/OL999W", "revision": 1}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, _ = s.DB.OpenLibraryStatus(ctx)
	if state.Works != 1 || state.Editions != 1 || state.Pending != 0 {
		t.Fatal("selected edition did not retrieve its missing book/author", state)
	}
	f.mu.Lock()
	unneeded := f.requests["/works/OL999W.json"]
	f.mu.Unlock()
	if unneeded != 0 {
		t.Fatal("unrelated work was fetched")
	}
}

func TestBookSelectionRejectsInvalidLanguagesWithoutChangingTheSavedScope(t *testing.T) {
	ctx, s, _ := testService(t)
	c, _ := s.DB.AIOConfig(ctx)
	original := c.Revision
	c.OpenLibraryLanguages = []string{"und"}
	if err := s.SaveConfig(ctx, c); !errors.Is(err, model.ErrInvalid) {
		t.Fatal(err)
	}
	current, _ := s.DB.AIOConfig(ctx)
	if current.Revision != original || len(current.OpenLibraryLanguages) != 0 {
		t.Fatal("rejected language altered saved selection")
	}
}

func selectedBookFixture(t *testing.T, s *Service) *bookAPIFixture {
	t.Helper()
	f := newBookAPIFixture(t, s)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dumpDocuments["OL300M"]["physical_format"] = "EPUB"
	f.dumpDocuments["OL300M"]["languages"] = []any{map[string]any{"key": "/languages/eng"}}
	f.dumpDocuments["OL301M"] = bookAPIRecord("OL301M", 1, map[string]any{"title": "Le Verger Ambre", "physical_format": "Electronic resource", "languages": []any{map[string]any{"key": "/languages/fre"}}, "works": []any{map[string]any{"key": "/works/OL100W"}}, "authors": []any{map[string]any{"key": "/authors/OL200A"}}})
	f.dumpDocuments["OL500W"] = bookAPIRecord("OL500W", 1, map[string]any{"title": "The Azure Almanac", "authors": []any{map[string]any{"author": map[string]any{"key": "/authors/OL600A"}}}})
	f.dumpDocuments["OL600A"] = bookAPIRecord("OL600A", 1, map[string]any{"name": "Synthetic Second Author"})
	f.dumpDocuments["OL700M"] = bookAPIRecord("OL700M", 1, map[string]any{"title": "The Azure Almanac", "physical_format": "Paperback", "works": []any{map[string]any{"key": "/works/OL500W"}}, "authors": []any{map[string]any{"key": "/authors/OL600A"}}})
	f.dumpDocuments["OL800W"] = bookAPIRecord("OL800W", 1, map[string]any{"title": "Unrelated Printed Work"})
	f.dumpDocuments["OL900M"] = bookAPIRecord("OL900M", 1, map[string]any{"title": "Unrelated Printed Work", "physical_format": "Hardcover", "works": []any{map[string]any{"key": "/works/OL800W"}}})
	s.BookExportURL = strings.Replace(s.BookExportURL, "/ol_dump_", "/ol_dump_editions_", 1)
	return f
}

func TestSelectedBookImportKeepsDigitalEditionsTranslationsAndLocalTitles(t *testing.T) {
	ctx, s, conn := testService(t)
	f := selectedBookFixture(t, s)
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.OpenLibraryRelevantOnly = true
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','book','{"title":"The.Azure.Almanac.EPUB"}')`); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil || !state.Complete || state.Works != 2 || state.Editions != 3 || state.Authors != 2 || state.Filtered < 2 {
		t.Fatal("incorrect selected catalog", state, err)
	}
	f.mu.Lock()
	calls := f.dumpCalls
	f.mu.Unlock()
	if calls != 4 {
		t.Fatal("selected import did not use the four scoped exports", calls)
	}
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls = f.dumpCalls
	f.mu.Unlock()
	if calls != 4 {
		t.Fatal("completed catalog was downloaded again", calls)
	}
	c, err = s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.OpenLibraryLanguages = []string{"fr"}
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.OpenLibraryStatus(ctx)
	if before.Works != 2 || before.Complete {
		t.Fatal("filter change deleted existing catalog before replacement", before)
	}
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, err = s.DB.OpenLibraryStatus(ctx)
	if err != nil || !state.Complete || state.Works != 1 || state.Editions != 1 || state.Authors != 1 {
		t.Fatal("language selection lost a translation or retained excluded books", state, err)
	}
	var language []string
	if err = conn.QueryRow(ctx, `SELECT languages FROM ingest.ol_editions`).Scan(&language); err != nil || len(language) != 1 || language[0] != "fr" {
		t.Fatal(language, err)
	}
}

func TestSelectedBookImportResumesItsPartAndLocksFilterChanges(t *testing.T) {
	ctx, s, _ := testService(t)
	f := selectedBookFixture(t, s)
	c, _ := s.DB.AIOConfig(ctx)
	c.OpenLibraryRelevantOnly = true
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.brokenDumpPart = "works"
	f.mu.Unlock()
	if err := s.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("truncated selected export was accepted")
	}
	state, _ := s.DB.OpenLibraryStatus(ctx)
	if state.Complete || state.ExportPhase != 3 || state.Editions != 2 {
		t.Fatal("completed edition/redirect progress lost", state)
	}
	release, locked, err := s.DB.AIOLock(ctx, "openlibrary")
	if err != nil || !locked {
		t.Fatal(err)
	}
	c, _ = s.DB.AIOConfig(ctx)
	c.OpenLibraryLanguages = []string{"fr"}
	if err = s.SaveConfig(ctx, c); !errors.Is(err, model.ErrBusy) {
		t.Fatal("scope changed while import was running", err)
	}
	release()
	f.mu.Lock()
	editionCalls := f.requests[strings.TrimPrefix(s.BookExportURL, s.BookAPIURL)]
	f.brokenDumpPart = ""
	f.mu.Unlock()
	if err = s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	state, _ = s.DB.OpenLibraryStatus(ctx)
	if !state.Complete || state.Works != 1 || state.Editions != 2 {
		t.Fatal(state)
	}
	f.mu.Lock()
	repeated := f.requests[strings.TrimPrefix(s.BookExportURL, s.BookAPIURL)]
	f.mu.Unlock()
	if repeated != editionCalls {
		t.Fatal("completed editions were downloaded again", editionCalls, repeated)
	}
}

func TestSelectedBookImportAcceptsAnEmptyLanguageSelectionResult(t *testing.T) {
	ctx, s, _ := testService(t)
	selectedBookFixture(t, s)
	c, _ := s.DB.AIOConfig(ctx)
	c.OpenLibraryRelevantOnly = true
	c.OpenLibraryLanguages = []string{"ja"}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal("valid empty selection was treated as a broken export", err)
	}
	state, _ := s.DB.OpenLibraryStatus(ctx)
	if !state.Complete || state.Works != 0 || state.Editions != 0 || state.Authors != 0 {
		t.Fatal(state)
	}
}
