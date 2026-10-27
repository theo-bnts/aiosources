package aiosource

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestBookImportResumesCommittedExportWithoutDuplicates(t *testing.T) {
	ctx, s, _ := testService(t)
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	for i := 1; i <= 252; i++ {
		fmt.Fprintf(gz, "/type/work\t/works/OL%dW\t1\t2026-01-01T00:00:00\t{\"key\":\"/works/OL%dW\",\"title\":\"Example Book %d\"}\n", i, i, i)
	}
	fmt.Fprintln(gz, "/type/author\t/authors/OL900A\t1\t2026-01-01\t{\"key\":\"/authors/OL900A\",\"name\":\"Example Author\"}")
	fmt.Fprintln(gz, "/type/edition\t/books/OL900M\t1\t2026-01-01\t{\"key\":\"/books/OL900M\",\"title\":\"Example Edition\",\"works\":[{\"key\":\"/works/OL1W\"}]}")
	fmt.Fprintln(gz, "/type/work\t/works/OL901W\t1\t2026-01-01\t{invalid}")
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	var broken atomic.Bool
	broken.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "AIOSources/") {
			t.Error("missing application identification")
		}
		w.Header().Set("ETag", `"fixture-v1"`)
		body := buffer.Bytes()
		if broken.Load() {
			body = body[:len(body)-8]
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	s.BookHTTP = server.Client()
	s.BookExportURL = server.URL
	if err := s.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("incomplete export was accepted")
	}
	status, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil || status.Position != 250 || status.Complete {
		t.Fatalf("checkpoint lost: %+v %v", status, err)
	}
	broken.Store(false)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	status, err = s.DB.OpenLibraryStatus(ctx)
	if err != nil || status.Position != 255 || status.Imported != 254 || status.Skipped != 1 || status.Works != 252 || !status.Complete {
		t.Fatalf("resume duplicated or lost data: %+v %v", status, err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	again, _ := s.DB.OpenLibraryStatus(ctx)
	if again.Imported != 254 {
		t.Fatalf("unchanged export was reimported: %+v", again)
	}
}

func TestBooksUseLocalMatchingSearchAndReviewWithoutVideoRegression(t *testing.T) {
	ctx, s, conn := testService(t)
	snapshot := "https://archive.org/download/example/fixture.gz"
	if _, err := s.DB.BeginOpenLibrary(ctx, snapshot, "fixture"); err != nil {
		t.Fatal(err)
	}
	work := model.AIOWork{Type: "book", OpenLibrary: "OL100W", DisplayTitle: "The Amber Orchard", Titles: []string{"The Amber Orchard"}, Years: []int{2001}, Book: model.BookInformation{AuthorKeys: []string{"OL200A"}, Cover: 12345}}
	records := []books.Record{{Author: &books.Author{ID: "OL200A", Name: "Morgan Vale", Aliases: []string{"Amber Writer"}}}, {Work: &work}, {Edition: &books.Edition{ID: "OL300M", Works: []string{"OL100W"}, Title: "Le Verger Ambré", Normalized: "le verger ambre", ISBN: []string{"9780306406157"}, Authors: []string{"OL200A"}, Languages: []string{"fr"}, Year: 2024}}}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 0, 3, records); err != nil {
		t.Fatal(err)
	}
	fields, _ := json.Marshal(map[string]any{"title": "Morgan Vale - The Amber Orchard EPUB", "info_hash": strings.Repeat("b", 40), "size": 500, "categories": []int{7020}})
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','book',$1)`, fields); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.FinishOpenLibrary(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"The.Amber.Orchard", "978-0-306-40615-7", "Morgan Vale", "Le Verger Ambre", "OL100W", "OL300M", "Morgan Orchard", "Orchard Morgan", "Amber Writer"} {
		page, err := s.CatalogSearch(ctx, model.ListOptions{Query: q}, model.CatalogSearchFilters{Language: "fr", Type: "book"})
		if err != nil || page.Total != 1 || page.Items[0].Title != "Le Verger Ambré" || page.Items[0].TMDB != "" || page.Items[0].Cover == "" {
			t.Fatalf("local book search failed (%q): %+v %v", q, page, err)
		}
	}
	translated, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Le Verger Ambre"}, model.CatalogSearchFilters{Language: "de", Type: "book"})
	if err != nil || translated.Total != 1 || translated.Items[0].Title != "Le Verger Ambré" {
		t.Fatal("query-language title fallback was lost", err)
	}
	translatedDetail, err := s.CatalogTitle(ctx, translated.Items[0].ID, "de", "le verger ambre")
	if err != nil || translatedDetail.Title != "Le Verger Ambré" {
		t.Fatal("detail title did not preserve the query-language fallback", err)
	}
	reviewCandidates, err := s.DB.AIOReviewCandidates(ctx, "Le Verger Ambre", "le verger ambre", "book", model.CatalogSearchFilters{Language: "de"})
	if err != nil || len(reviewCandidates) != 1 || reviewCandidates[0].DisplayTitle != "Le Verger Ambré" {
		t.Fatal("review titles do not follow the same language fallback", err)
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Type: "audiobook"})
	if err != nil || page.Total != 0 {
		t.Fatalf("ebook became audiobook: %+v %v", page, err)
	}
	page, err = s.CatalogSearch(ctx, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Year: 2024, Type: "ebook"})
	if err != nil || page.Total != 1 {
		t.Fatalf("edition year lost: %+v %v", page, err)
	}
	var occurrence string
	if err = conn.QueryRow(ctx, `SELECT occurrence_id FROM ingest.torrents WHERE provider_id='fixture' AND source_id='book'`).Scan(&occurrence); err != nil {
		t.Fatal(err)
	}
	detail, err := s.DB.AIOReviewDetail(ctx, occurrence)
	if err != nil || detail.Work == nil || detail.Work.OpenLibrary != "OL100W" {
		t.Fatalf("book review unavailable: %+v %v", detail, err)
	}
	if err = s.SaveReview(ctx, occurrence, model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: detail.Work.ID, WorkRevision: detail.Work.Revision}); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `UPDATE ingest.aio_works SET years='{}' WHERE media_type='book'`); err != nil {
		t.Fatal(err)
	}
	dated, e := s.CatalogSearch(ctx, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Type: "book"})
	if e != nil || dated.Total != 1 || len(dated.Items[0].Years) != 1 || dated.Items[0].Years[0] != 2024 {
		t.Fatal("known edition year was not displayed", e)
	}
	seedMovie(t, ctx, s, conn, "fixture", "film", strings.Repeat("c", 40))
	video, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example Movie"}, model.CatalogSearchFilters{Type: "movie"})
	if err != nil || video.Total != 1 {
		t.Fatalf("video search regressed: %+v %v", video, err)
	}
}

func TestBookFileChoicePreservesVideoSelection(t *testing.T) {
	files := []model.AIOFile{{Path: "Example Book.epub", Size: 200}, {Path: "Example Book.pdf", Size: 400}, {Path: "cover.jpg", Size: 20}}
	if _, ok := SelectFile(files, Resource{Type: "book"}); ok {
		t.Fatal("multiple book formats were guessed")
	}
	if file, ok := SelectFile(files, Resource{Type: "book", File: "Example Book.epub"}); !ok || file.Path != "Example Book.epub" {
		t.Fatal("book selection failed")
	}
	if _, ok := SelectFile(files, Resource{Type: "movie"}); ok {
		t.Fatal("ebook was returned as video")
	}
	if _, ok := SelectFile(files, Resource{Type: "book", File: "cover.jpg"}); ok {
		t.Fatal("non-book attachment was selected")
	}
}

func TestBookEditionAliasesAndMergesDoNotLoseMatchesOrDuplicateWorks(t *testing.T) {
	ctx, s, conn := testService(t)
	seedTestBook(t, ctx, s, conn, "fixture")
	snapshot := "https://archive.org/download/example/next.gz"
	if _, err := s.DB.BeginOpenLibrary(ctx, snapshot, "second"); err != nil {
		t.Fatal(err)
	}
	orphan := books.Edition{ID: "OL400M", Works: []string{"OL400M"}, Title: "A Standalone Edition", Normalized: "a standalone edition", ISBN: []string{}, Authors: []string{}, Languages: []string{"en"}}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 0, 1, []books.Record{{Edition: &orphan}}); err != nil {
		t.Fatal(err)
	}
	target := model.AIOWork{Type: "book", OpenLibrary: "OL500W", DisplayTitle: "A Standalone Edition", Years: []int{}, Book: model.BookInformation{AuthorKeys: []string{}}}
	orphan.Works = []string{"OL500W"}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 1, 3, []books.Record{{Work: &target}, {Edition: &orphan}}); err != nil {
		t.Fatal(err)
	}
	state, err := s.DB.OpenLibraryStatus(ctx)
	if err != nil || state.Works != 2 || state.Editions != 2 || state.Authors != 1 {
		t.Fatalf("live counts drifted: %+v %v", state, err)
	}
	var resolved string
	if err = conn.QueryRow(ctx, `SELECT ingest.ol_resolve('OL400M')`).Scan(&resolved); err != nil || resolved != "OL500W" {
		t.Fatal("edition redirect lost", err)
	}
	// Exact edition evidence must remain discoverable beyond the first 200 aliases.
	if _, err = conn.Exec(ctx, `INSERT INTO ingest.ol_editions(id,work_keys,title,normalized) SELECT 'OL'||(1000+n)||'M',ARRAY['OL100W'],'A Prior Edition '||n,'a prior edition '||n FROM generate_series(1,250)n;
 INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,author_keys) VALUES('OL2000M',ARRAY['OL100W'],'Zither and Amber','zither and amber',ARRAY['OL200A']);
 INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','translated','{"title":"Morgan Vale - Zither and Amber EPUB","info_hash":"dddddddddddddddddddddddddddddddddddddddd"}');`); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.FinishOpenLibrary(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Zither Amber"}, model.CatalogSearchFilters{Type: "book"})
	if err != nil || page.Total != 1 {
		t.Fatalf("later edition alias was not matched: %+v %v", page, err)
	}
	seedMovie(t, ctx, s, conn, "fixture", "film", strings.Repeat("e", 40))
	if err = s.DB.ResetBookInformation(ctx); err != nil {
		t.Fatal(err)
	}
	var works, editions, torrents int
	if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.ol_editions),(SELECT count(*) FROM ingest.torrents)`).Scan(&works, &editions, &torrents); err != nil || works != 1 || editions != 0 || torrents != 3 {
		t.Fatalf("book reset touched video or source data: %d %d %d %v", works, editions, torrents, err)
	}
}

func TestBookSchemaKeepsVideoReviewEvidenceUnchanged(t *testing.T) {
	ctx, _, conn := testService(t)
	for _, fields := range []string{`{"title":"Example.Movie.2024.1080p","author":"Studio credit"}`, `{"title":"Example.Series.S01E02","external_ids":{"imdb":"tt1234567"},"attributes":{"language":["fr"]}}`} {
		var same bool
		if err := conn.QueryRow(ctx, `SELECT ingest.aio_review_evidence($1)=ingest.aio_video_review_evidence($1)`, fields).Scan(&same); err != nil || !same {
			t.Fatal("video review evidence changed", err)
		}
	}
	var changed bool
	if err := conn.QueryRow(ctx, `SELECT ingest.aio_review_evidence('{"title":"Example Book EPUB","attributes":{"isbn":["9780306406157"]}}')<>ingest.aio_review_evidence('{"title":"Example Book EPUB","attributes":{"isbn":["9780306406158"]}}')`).Scan(&changed); err != nil || !changed {
		t.Fatal("changed book identifiers did not invalidate the review", err)
	}
}

func TestInterruptedBookRefreshWithdrawsChangedEditionEvidence(t *testing.T) {
	ctx, s, conn := testService(t)
	seedTestBook(t, ctx, s, conn, "fixture")
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_editions SET isbn=ARRAY['9780306406157'] WHERE id='OL300M';UPDATE ingest.torrents SET fields=fields||'{"isbn":"9780306406157"}' WHERE provider_id='fixture' AND source_id='book'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := "https://archive.org/download/example/changed.gz"
	if _, err := s.DB.BeginOpenLibrary(ctx, snapshot, "changed"); err != nil {
		t.Fatal(err)
	}
	edition := books.Edition{ID: "OL300M", Works: []string{"OL100W"}, Title: "The Amber Orchard", Normalized: "the amber orchard", ISBN: []string{"9780000000002"}, Authors: []string{"OL200A"}, Languages: []string{"en"}, Year: 2024}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 0, 1, []books.Record{{Edition: &edition}}); err != nil {
		t.Fatal(err)
	}
	result, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Type: "book"})
	if err != nil || result.Total != 0 {
		t.Fatal("stale edition evidence stayed published during an interrupted refresh", err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches WHERE provider_id='fixture' AND source_id='book'`).Scan(&status); err != nil || status != "unmatched" {
		t.Fatal("removed ISBN stayed authoritative", err)
	}
}

func TestBookImportRespectsExportRetryAfter(t *testing.T) {
	ctx, s, conn := testService(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer upstream.Close()
	s.BookHTTP = upstream.Client()
	s.BookExportURL = upstream.URL
	if err := s.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("rate-limited export succeeded")
	}
	var respected bool
	if err := conn.QueryRow(ctx, `SELECT retry_at>clock_timestamp()+interval '59 minutes' FROM ingest.ol_import_state`).Scan(&respected); err != nil || !respected {
		t.Fatal("export retry deadline ignored", err)
	}
	due, err := s.DB.OpenLibraryDue(ctx, 30)
	if err != nil || due {
		t.Fatal("rate-limited export can restart immediately", err)
	}
}

func TestBookImportBoundsHeadersWithoutTimingOutHealthyTransfers(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(fmt.Sprintf("headers-received-%t", headers), func(t *testing.T) {
			t.Parallel()
			ctx, s, _ := testService(t)
			policy, err := s.DB.RuntimeSection(ctx, "outgoing")
			if err != nil {
				t.Fatal(err)
			}
			policy.Values["metadata_seconds"] = 5
			if err := s.DB.UpdateRuntimeSection(ctx, "outgoing", policy); err != nil {
				t.Fatal(err)
			}
			var buffer bytes.Buffer
			gz := gzip.NewWriter(&buffer)
			fmt.Fprintln(gz, "/type/work\t/works/OL100W\t1\t2026-01-01\t{\"key\":\"/works/OL100W\",\"title\":\"The Amber Orchard\"}")
			fmt.Fprintln(gz, "/type/author\t/authors/OL200A\t1\t2026-01-01\t{\"key\":\"/authors/OL200A\",\"name\":\"Morgan Vale\"}")
			fmt.Fprintln(gz, "/type/edition\t/books/OL300M\t1\t2026-01-01\t{\"key\":\"/books/OL300M\",\"title\":\"The Amber Orchard\",\"works\":[{\"key\":\"/works/OL100W\"}]}")
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !headers {
					<-r.Context().Done()
					return
				}
				w.Header().Set("ETag", `"fixture-timeout"`)
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				timer := time.NewTimer(6 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
					_, _ = w.Write(buffer.Bytes())
				case <-r.Context().Done():
				}
			}))
			defer upstream.Close()
			s.BookHTTP = upstream.Client()
			s.BookExportURL = upstream.URL
			started := time.Now()
			err = s.SyncBooks(ctx, "manual")
			if headers && err != nil {
				t.Fatal("healthy export used a total request deadline", err)
			}
			if !headers && (err == nil || time.Since(started) > 10*time.Second) {
				t.Fatal("configured header timeout was not applied", err)
			}
		})
	}
}
