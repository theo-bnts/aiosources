package aiosource

import (
	"fmt"
	"testing"
	"time"
)

// These complete imports use a loopback dump and change feed, including the
// reconciliation/checkpoints that a parser-only benchmark would miss.
func BenchmarkBookImport(b *testing.B) {
	ctx, s, conn := testService(b)
	fixture := newBookAPIFixture(b, s)
	fixture.mu.Lock()
	for i := 1000; i < 2000; i++ {
		work, edition := fmt.Sprintf("OL%dW", i), fmt.Sprintf("OL%dM", i)
		fixture.dumpDocuments[work] = bookAPIRecord(work, 1, map[string]any{"title": fmt.Sprintf("Synthetic Orchard %d", i)})
		fixture.dumpDocuments[edition] = bookAPIRecord(edition, 1, map[string]any{"title": fmt.Sprintf("Synthetic Orchard %d", i), "physical_format": "ebook", "works": []any{map[string]any{"key": "/works/" + work}}})
	}
	fixture.mu.Unlock()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := s.Reset(ctx, "books"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.SyncBooks(ctx, "manual"); err != nil {
			b.Fatal(err)
		}
		status, err := s.DB.OpenLibraryStatus(ctx)
		if err != nil || !status.Complete || status.Works != 1001 || status.Editions != 1001 || status.Authors != 1 {
			b.Fatal("Book bootstrap lost selected information or completion", err)
		}
		var completed string
		if err := conn.QueryRow(ctx, `SELECT status FROM aiosources.operation_runs WHERE service='openlibrary' ORDER BY created_at DESC LIMIT 1`).Scan(&completed); err != nil || completed != "succeeded" {
			b.Fatal("Book bootstrap did not finish its operation history", completed, err)
		}
	}
	b.ReportMetric(2003, "documents/op")
}

func BenchmarkBookChanges(b *testing.B) {
	ctx, s, conn := testService(b)
	fixture := newBookAPIFixture(b, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		title := fmt.Sprintf("Synthetic Revised Orchard %d", i)
		fixture.mu.Lock()
		fixture.documents["OL100W"]["title"] = title
		fixture.documents["OL100W"]["revision"] = i + 2
		fixture.rows = append([]bookAPIEvent{{ID: fmt.Sprint(i + 101), Timestamp: fixture.time.Add(time.Duration(i+1) * time.Second).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": i + 2}}}}, fixture.rows...)
		fixture.mu.Unlock()
		if _, err := conn.Exec(ctx, `UPDATE aiosources.ol_changes_state SET checked_at=NULL,retry_at=NULL`); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := s.SyncBooks(ctx, "manual"); err != nil {
			b.Fatal(err)
		}
		var actual string
		if err := conn.QueryRow(ctx, `SELECT display_title FROM aiosources.aio_works WHERE openlibrary_id='OL100W'`).Scan(&actual); err != nil || actual != title {
			b.Fatal("Change feed did not update the saved work", err)
		}
		var completed string
		if err := conn.QueryRow(ctx, `SELECT status FROM aiosources.operation_runs WHERE service='openlibrary' ORDER BY created_at DESC LIMIT 1`).Scan(&completed); err != nil || completed != "succeeded" {
			b.Fatal("Book changes did not finish their operation history", completed, err)
		}
	}
}

func BenchmarkInformationReset(b *testing.B) {
	for _, scope := range []string{"books", "videos"} {
		b.Run(scope, func(b *testing.B) {
			ctx, s, conn := testService(b)
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				if _, err := conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,openlibrary_id,book_normalized,display_title,years)
       SELECT 'book','OL'||n||'W','synthetic book '||n,'Synthetic Book '||n,ARRAY[2024] FROM generate_series(1,5000)n;
      INSERT INTO aiosources.ol_editions(id,work_keys,title,normalized) SELECT 'OL'||n||'M',ARRAY['OL'||n||'W'],'Synthetic Edition','synthetic edition' FROM generate_series(1,5000)n;
      INSERT INTO aiosources.aio_works(media_type,tmdb_id,display_title,years) SELECT 'movie',n::text,'Synthetic Movie',ARRAY[2024] FROM generate_series(1,5000)n;
      INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,display_title,lower(display_title) FROM aiosources.aio_works;`); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := s.Reset(ctx, scope); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				var books, videos int
				if err := conn.QueryRow(ctx, `SELECT count(*) FILTER(WHERE media_type='book'),count(*) FILTER(WHERE media_type='movie') FROM aiosources.aio_works`).Scan(&books, &videos); err != nil {
					b.Fatal(err)
				}
				if scope == "books" && (books != 0 || videos != 5000) || scope == "videos" && (videos != 0 || books != 5000) {
					b.Fatal("Reset removed the wrong information", books, videos)
				}
				if _, err := conn.Exec(ctx, `TRUNCATE aiosources.aio_titles,aiosources.ol_editions,aiosources.aio_works CASCADE`); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}
