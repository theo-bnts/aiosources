package aiosource

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

type bookAPIEvent struct {
	ID, Timestamp string
	Changes       []map[string]any
}

func (e bookAPIEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": e.ID, "timestamp": e.Timestamp, "changes": e.Changes})
}

type bookAPIFixture struct {
	mu             sync.Mutex
	day, time      time.Time
	rows           []bookAPIEvent
	documents      map[string]map[string]any
	dumpDocuments  map[string]map[string]any
	documentStatus int
	batchStatus    int
	feedStatus     int
	retryAfter     string
	ignoreOffsets  bool
	dumpCalls      int
	brokenDumpPart string
	requests       map[string]int
}

func bookAPIRecord(id string, revision int, fields map[string]any) map[string]any {
	kind := "/type/work"
	if strings.HasSuffix(id, "M") {
		kind = "/type/edition"
	}
	if strings.HasSuffix(id, "A") {
		kind = "/type/author"
	}
	data := map[string]any{"key": books.Path(id), "type": map[string]any{"key": kind}, "revision": revision}
	for key, value := range fields {
		data[key] = value
	}
	return data
}

func newBookAPIFixture(t *testing.T, s *Service) *bookAPIFixture {
	t.Helper()
	at := time.Now().UTC().Add(-2 * time.Hour)
	f := &bookAPIFixture{day: at.Truncate(24 * time.Hour), time: at, documents: map[string]map[string]any{}, requests: map[string]int{}, rows: []bookAPIEvent{{ID: "100", Timestamp: at.Format(time.RFC3339Nano), Changes: []map[string]any{}}}}
	f.documents["OL100W"] = bookAPIRecord("OL100W", 1, map[string]any{"title": "The Amber Orchard", "authors": []any{map[string]any{"author": map[string]any{"key": "/authors/OL200A"}}}})
	f.documents["OL200A"] = bookAPIRecord("OL200A", 1, map[string]any{"name": "Morgan Vale"})
	f.documents["OL300M"] = bookAPIRecord("OL300M", 1, map[string]any{"title": "The Amber Orchard", "works": []any{map[string]any{"key": "/works/OL100W"}}, "authors": []any{map[string]any{"key": "/authors/OL200A"}}, "isbn_13": []string{"9780306406157"}})
	snapshot, _ := json.Marshal(f.documents)
	_ = json.Unmarshal(snapshot, &f.dumpDocuments)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests[r.URL.Path]++
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}
		if r.URL.Path == "/recentchanges.json" || strings.HasPrefix(r.URL.Path, "/recentchanges/") {
			if f.feedStatus != 0 {
				w.WriteHeader(f.feedStatus)
				return
			}
			rows := f.rows
			if r.URL.Path != "/recentchanges.json" {
				rows = nil
				for _, row := range f.rows {
					at, _ := books.ParseTimestamp(row.Timestamp)
					if "/recentchanges/"+at.Format("2006/01/02")+".json" == r.URL.Path {
						rows = append(rows, row)
					}
				}
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if f.ignoreOffsets {
				offset = 0
			}
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit < 1 || limit > 1000 {
				limit = 1000
			}
			offset = min(offset, len(rows))
			page := rows[offset:min(offset+limit, len(rows))]
			if page == nil {
				page = []bookAPIEvent{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		if strings.Contains(r.URL.Path, "ol_dump_") {
			f.dumpCalls++
			w.Header().Set("ETag", `"`+f.day.Format("2006-01-02")+`"`)
			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			ids := []string{}
			for id := range f.dumpDocuments {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				data := f.dumpDocuments[id]
				body, _ := json.Marshal(data)
				kind := data["type"].(map[string]any)["key"]
				part := ""
				switch kind {
				case "/type/edition":
					part = "editions"
				case "/type/work":
					part = "works"
				case "/type/author":
					part = "authors"
				case "/type/redirect":
					part = "redirects"
				}
				if strings.Contains(r.URL.Path, "_editions_") || strings.Contains(r.URL.Path, "_works_") || strings.Contains(r.URL.Path, "_authors_") || strings.Contains(r.URL.Path, "_redirects_") {
					if !strings.Contains(r.URL.Path, "_"+part+"_") {
						continue
					}
				}
				fmt.Fprintf(gz, "%s\t%s\t%v\t%s\t%s\n", kind, books.Path(id), data["revision"], f.day.Format("2006-01-02"), body)
			}
			_ = gz.Close()
			body := b.Bytes()
			if f.brokenDumpPart != "" && strings.Contains(r.URL.Path, "_"+f.brokenDumpPart+"_") {
				body = body[:len(body)-8]
			}
			_, _ = w.Write(body)
			return
		}
		if r.URL.Path == "/api/get_many" {
			if f.batchStatus != 0 {
				w.WriteHeader(f.batchStatus)
				return
			}
			var keys []string
			_ = json.Unmarshal([]byte(r.URL.Query().Get("keys")), &keys)
			result := map[string]any{}
			for _, key := range keys {
				if data, ok := f.documents[books.Key(key)]; ok {
					result[key] = data
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "result": result})
			return
		}
		if f.documentStatus != 0 {
			w.WriteHeader(f.documentStatus)
			return
		}
		id := books.Key(strings.TrimSuffix(r.URL.Path, ".json"))
		data, ok := f.documents[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}))
	t.Cleanup(server.Close)
	s.BookHTTP = server.Client()
	s.BookAPIURL = server.URL
	s.BookExportURL = server.URL + "/ol_dump_" + f.day.Format("2006-01-02") + ".txt.gz"
	s.bookRate.setRate(1_000_000)
	return f
}

func bookFeedDue(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_changes_state SET checked_at=NULL,retry_at=NULL;UPDATE ingest.ol_import_state SET retry_at=NULL`); err != nil {
		t.Fatal(err)
	}
}

func TestBookChangesUpdateTheLocalCatalogWithoutAnotherDump(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','future','{"title":"Morgan Vale - The Azure Almanac EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.OpenLibraryStatus(ctx)
	if before.CatchingUp || before.ChangesThrough == nil {
		t.Fatalf("initial snapshot was not bridged to its tail: %+v", before)
	}
	f.mu.Lock()
	f.documents["OL100W"]["title"] = "The Amber Orchard Revised"
	f.documents["OL100W"]["revision"] = 2
	f.documents["OL200A"]["name"] = "Morgan Vale Updated"
	f.documents["OL200A"]["revision"] = 2
	f.documents["OL500W"] = bookAPIRecord("OL500W", 1, map[string]any{"title": "The Azure Almanac", "authors": []any{map[string]any{"author": map[string]any{"key": "/authors/OL200A"}}}})
	f.documents["OL200A"]["alternate_names"] = []string{"Morgan Vale"}
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}, {"key": "/authors/OL200A", "revision": 2}, {"key": "/works/OL500W", "revision": 1}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var title, author string
	if err := conn.QueryRow(ctx, `SELECT display_title,(SELECT name FROM ingest.ol_authors WHERE id='OL200A') FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title, &author); err != nil || title != "The Amber Orchard Revised" || author != "Morgan Vale Updated" {
		t.Fatalf("renamed evidence was not updated: %q %q %v", title, author, err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var decision string
	if err := conn.QueryRow(ctx, `SELECT status FROM ingest.aio_matches WHERE provider_id='fixture' AND source_id='future'`).Scan(&decision); err != nil || decision != "matched" {
		t.Fatal("new book did not requeue a formerly unmatched torrent", decision, err)
	}
	after, _ := s.DB.OpenLibraryStatus(ctx)
	if after.Pending != 0 || after.ChangesThrough == nil || !after.ChangesThrough.After(*before.ChangesThrough) {
		t.Fatalf("applied progress did not advance: %+v", after)
	}
	f.mu.Lock()
	calls := f.dumpCalls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("incremental changes downloaded another dump", calls)
	}
}

func TestBookPendingRevisionsSurviveAnInterruptedUpdate(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.OpenLibraryStatus(ctx)
	f.mu.Lock()
	f.documentStatus = 400
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.DB.OpenLibraryStatus(ctx)
	if pending.Pending != 1 || pending.ChangesThrough == nil || !pending.ChangesThrough.Equal(*before.ChangesThrough) {
		t.Fatalf("unapplied work advanced the safe checkpoint: %+v", pending)
	}
	var kind string
	if err := conn.QueryRow(ctx, `SELECT kind FROM ingest.activity_logs WHERE service='openlibrary' ORDER BY id DESC LIMIT 1`).Scan(&kind); err != nil || kind != "openlibrary.sync_pending" {
		t.Fatal("deferred API work was reported as a skipped export record", kind, err)
	}
	f.mu.Lock()
	f.documentStatus = 0
	f.documents["OL100W"]["revision"] = 2
	f.documents["OL100W"]["title"] = "The Amber Orchard Updated"
	f.mu.Unlock()
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_change_jobs SET next_attempt=now()`); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, s.DB, s.Vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	restarted.BookHTTP = s.BookHTTP
	restarted.BookAPIURL = s.BookAPIURL
	restarted.BookExportURL = s.BookExportURL
	restarted.bookRate.setRate(1_000_000)
	if err = restarted.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.DB.OpenLibraryStatus(ctx)
	if after.Pending != 0 || after.ChangesThrough == nil || !after.ChangesThrough.After(*before.ChangesThrough) {
		t.Fatalf("saved pending work did not resume: %+v", after)
	}
}

func TestBookJobAcknowledgementsCannotLoseAConcurrentRevision(t *testing.T) {
	ctx, s, _ := testService(t)
	at := time.Now().UTC().Add(-time.Hour)
	if err := s.DB.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL100W", Revision: 1, At: at}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.DB.BookChangeJobs(ctx, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatal(err)
	}
	if err = s.DB.QueueBookChanges(ctx, []books.ChangeKey{{ID: "OL100W", Revision: 2, At: at.Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	record := books.Record{Revision: 1, Work: &model.AIOWork{Type: "book", OpenLibrary: "OL100W", DisplayTitle: "The Amber Orchard", Years: []int{}}}
	if err = s.DB.ApplyBookChange(ctx, jobs[0], record); err != nil {
		t.Fatal(err)
	}
	remaining, err := s.DB.BookChangeJobs(ctx, 1)
	if err != nil || len(remaining) != 1 || remaining[0].Revision != 2 {
		t.Fatal("newer revision was incorrectly acknowledged", remaining, err)
	}
}

func TestBookSnapshotKeepsNewerAPIDataAndPrunesMissingEntries(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	job := store.BookChangeJob{ID: "OL100W", Revision: 2, ChangedAt: at}
	record := books.Record{Revision: 2, Work: &model.AIOWork{Type: "book", OpenLibrary: "OL100W", DisplayTitle: "A Newer API Title", Years: []int{}}}
	if err := s.DB.ApplyBookChange(ctx, job, record); err != nil {
		t.Fatal(err)
	}
	snapshot := "https://archive.org/download/ol_dump_" + f.day.Format("2006-01-02") + "/next.gz"
	if _, err := s.DB.BeginOpenLibrary(ctx, snapshot, "changed-export"); err != nil {
		t.Fatal(err)
	}
	older := books.Record{Revision: 1, Work: &model.AIOWork{Type: "book", OpenLibrary: "OL100W", DisplayTitle: "An Older Dump Title", Years: []int{}}}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 0, 1, []books.Record{older}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.BookExportLoaded(ctx); err != nil {
		t.Fatal(err)
	}
	for {
		done, err := s.DB.PruneBookSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	var title string
	var editions, authors int
	if err := conn.QueryRow(ctx, `SELECT display_title,(SELECT count(*) FROM ingest.ol_editions),(SELECT count(*) FROM ingest.ol_authors) FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title, &editions, &authors); err != nil || title != "A Newer API Title" || editions != 0 || authors != 0 {
		t.Fatal("snapshot reconciliation regressed fresh data or retained missing entries", title, editions, authors, err)
	}
}

func TestBookGroupedReadsReduceRequestsAndFallBackWithoutLosingRecords(t *testing.T) {
	for _, supported := range []bool{true, false} {
		t.Run(fmt.Sprint(supported), func(t *testing.T) {
			ctx, s, conn := testService(t)
			f := newBookAPIFixture(t, s)
			if err := s.SyncBooks(ctx, "manual"); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			if !supported {
				f.batchStatus = http.StatusMethodNotAllowed
			}
			keys := []map[string]any{}
			for index := 0; index < 51; index++ {
				id := fmt.Sprintf("OL%dW", 1000+index)
				f.documents[id] = bookAPIRecord(id, 1, map[string]any{"title": fmt.Sprintf("Synthetic Almanac %d", index)})
				keys = append(keys, map[string]any{"key": books.Path(id), "revision": 1})
			}
			f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: keys}}, f.rows...)
			f.mu.Unlock()
			bookFeedDue(t, ctx, conn)
			if err := s.SyncBooks(ctx, "manual"); err != nil {
				t.Fatal(err)
			}
			state, err := s.DB.OpenLibraryStatus(ctx)
			if err != nil || state.Pending != 0 || state.Works != 52 {
				t.Fatal("grouped/fallback publication lost records", state, err)
			}
			f.mu.Lock()
			grouped, singles := f.requests["/api/get_many"], 0
			for path, count := range f.requests {
				if strings.HasPrefix(path, "/works/") {
					singles += count
				}
			}
			f.mu.Unlock()
			if supported && (grouped != 2 || singles != 1) {
				t.Fatal("expected three requests for 51 records", grouped, singles)
			}
			if !supported && (grouped != 1 || singles != 51) {
				t.Fatal("unsupported batch endpoint did not fall back once", grouped, singles)
			}
		})
	}
}

func TestBookFeedGapWaitsForANewerDumpWhileStillApplyingNewChanges(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	f.mu.Lock()
	f.day = time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	f.time = f.day.Add(time.Hour)
	f.rows[0].Timestamp = f.time.Format(time.RFC3339Nano)
	f.mu.Unlock()
	s.BookExportURL = s.BookAPIURL + "/ol_dump_" + f.day.Format("2006-01-02") + ".txt.gz"
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.OpenLibraryStatus(ctx)
	f.mu.Lock()
	f.documents["OL100W"]["revision"] = 2
	f.documents["OL100W"]["title"] = "A Title Updated After The Gap"
	// The provider no longer returns the saved anchor. Even this short page
	// cannot prove coverage, so it must require recovery rather than succeed.
	f.rows = []bookAPIEvent{{ID: "105", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}}}}
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	gap, _ := s.DB.OpenLibraryStatus(ctx)
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title); err != nil || title != "A Title Updated After The Gap" {
		t.Fatal("live updates stopped during recovery", title, err)
	}
	if !gap.NeedsExport || gap.Generation != before.Generation || gap.ChangesThrough == nil || !gap.ChangesThrough.Equal(*before.ChangesThrough) {
		t.Fatal("old dump was reimported or the gap was declared complete", gap)
	}
	if due, err := s.DB.OpenLibraryDue(ctx, 15); err != nil || due {
		t.Fatal("waiting for a newer monthly export caused a busy retry loop", due, err)
	}
	f.mu.Lock()
	f.day = f.day.AddDate(0, 0, 1)
	snapshot, _ := json.Marshal(f.documents)
	_ = json.Unmarshal(snapshot, &f.dumpDocuments)
	f.mu.Unlock()
	s.BookExportURL = s.BookAPIURL + "/ol_dump_" + f.day.Format("2006-01-02") + ".txt.gz"
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	recovered, _ := s.DB.OpenLibraryStatus(ctx)
	if recovered.NeedsExport || recovered.CatchingUp || recovered.Generation <= before.Generation || recovered.ChangesThrough == nil || !recovered.ChangesThrough.After(*before.ChangesThrough) {
		t.Fatal("newer export did not recover the missing window", recovered)
	}
}

func TestBookCatchupCannotSucceedBeyondTheProviderCapOrWithoutItsBoundary(t *testing.T) {
	for _, scenario := range []string{"overflow", "repeated", "missing-boundary"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, s, conn := testService(t)
			f := newBookAPIFixture(t, s)
			if err := s.SyncBooks(ctx, "manual"); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.rows = nil
			f.day = f.day.AddDate(0, 0, -1)
			if scenario != "missing-boundary" {
				for index := 0; index < 11_100; index++ {
					f.rows = append(f.rows, bookAPIEvent{ID: strconv.Itoa(30_000 - index), Timestamp: f.day.Add(12*time.Hour - time.Duration(index)*time.Millisecond).Format(time.RFC3339Nano), Changes: []map[string]any{}})
				}
			}
			f.ignoreOffsets = scenario == "repeated"
			f.mu.Unlock()
			if _, err := conn.Exec(ctx, `UPDATE ingest.ol_changes_state SET bootstrap_complete=FALSE,bootstrap_day=$1,tail_since=$2,tail_start_id='100'`, f.day, f.day.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			h, err := s.beginOperation(ctx, "metadata", "openlibrary", "manual", "", "openlibrary")
			if err != nil {
				t.Fatal(err)
			}
			err = s.catchUpBookDay(ctx, h)
			h.finish(ctx, err)
			if err != nil {
				t.Fatal(err)
			}
			state, _ := s.DB.OpenLibraryStatus(ctx)
			if !state.NeedsExport || !state.CatchingUp {
				t.Fatal("incomplete API history was accepted", state)
			}
		})
	}
}

func TestBookProviderCooldownSurvivesRestartAndProtectsTheDump(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	f.mu.Lock()
	f.feedStatus, f.retryAfter = http.StatusTooManyRequests, "3600"
	f.mu.Unlock()
	if err := s.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("provider quota response was accepted")
	}
	state, _ := s.DB.BookChangesState(ctx)
	if state.RetryAt == nil || time.Until(*state.RetryAt) < 59*time.Minute {
		t.Fatal("provider Retry-After was lost", state)
	}
	restarted, err := New(ctx, s.DB, s.Vault, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	restarted.BookHTTP, restarted.BookAPIURL, restarted.BookExportURL = s.BookHTTP, s.BookAPIURL, s.BookExportURL
	if err := restarted.SyncBooks(ctx, "manual"); err == nil {
		t.Fatal("manual run bypassed the provider cooldown")
	}
	if due, err := s.DB.OpenLibraryDue(ctx, 15); err != nil || due {
		t.Fatal("automatic restart ignored the cooldown", due, err)
	}
	f.mu.Lock()
	calls, dumps := f.requests["/recentchanges.json"], f.dumpCalls
	f.mu.Unlock()
	if calls != 1 || dumps != 0 {
		t.Fatal("restart contacted the provider while blocked", calls, dumps)
	}
	var logged int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.operation_events WHERE phase='book_changes' AND http_status=429`).Scan(&logged); err != nil || logged != 1 {
		t.Fatal("book API call was missing from ordinary history", logged, err)
	}
}

func TestBookPageOverlapAllowsTheFinalPartialPageButRejectsRepeatedFullPages(t *testing.T) {
	previous := make([]books.Change, bookChangePageSize)
	for index := range previous {
		previous[index].ID = strconv.Itoa(index + 1)
	}
	if !bookPageOverlaps(previous, previous[len(previous)-bookChangeOverlap:]) {
		t.Fatal("final overlapping page with no new changes was rejected")
	}
	if bookPageOverlaps(previous, previous) {
		t.Fatal("a repeated full page was accepted as progress")
	}
}

func TestBookMissingDocumentMustBeConfirmedAndCanLaterBeRestored(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	delete(f.documents, "OL100W")
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var present bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.aio_works WHERE openlibrary_id='OL100W')`).Scan(&present); err != nil || !present {
		t.Fatal("one transient 404 removed the local record", err)
	}
	if _, err := conn.Exec(ctx, `UPDATE ingest.ol_change_jobs SET next_attempt=now()`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var deleted int
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.aio_works WHERE openlibrary_id='OL100W'),(SELECT ol_revision FROM ingest.ol_deleted WHERE id='OL100W')`).Scan(&present, &deleted); err != nil || present || deleted != 2 {
		t.Fatal("confirmed absence was not applied", present, deleted, err)
	}
	f.mu.Lock()
	f.documents["OL100W"] = bookAPIRecord("OL100W", 3, map[string]any{"title": "The Restored Amber Orchard"})
	f.rows = append([]bookAPIEvent{{ID: "102", Timestamp: f.time.Add(61 * time.Minute).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 3}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title,(SELECT count(*) FROM ingest.ol_deleted WHERE id='OL100W') FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title, &deleted); err != nil || title != "The Restored Amber Orchard" || deleted != 0 {
		t.Fatal("a later upstream restoration was blocked", title, deleted, err)
	}
}

func TestBookMergeFetchesItsTargetAndDoesNotTouchVideoInformation(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	seedMovie(t, ctx, s, conn, "fixture", "film", strings.Repeat("a", 40))
	f.mu.Lock()
	f.documents["OL100W"] = map[string]any{"type": map[string]any{"key": "/type/redirect"}, "key": "/works/OL100W", "location": "/works/OL500W", "revision": 2}
	f.documents["OL500W"] = bookAPIRecord("OL500W", 1, map[string]any{"title": "The Canonical Amber Orchard"})
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	var target, title string
	var old, videos int
	if err := conn.QueryRow(ctx, `SELECT ingest.ol_resolve('OL100W'),(SELECT display_title FROM ingest.aio_works WHERE openlibrary_id='OL500W'),(SELECT count(*) FROM ingest.aio_works WHERE openlibrary_id='OL100W'),(SELECT count(*) FROM ingest.aio_works WHERE media_type='movie')`).Scan(&target, &title, &old, &videos); err != nil || target != "OL500W" || title != "The Canonical Amber Orchard" || old != 0 || videos != 1 {
		t.Fatal("merge lost its canonical target or changed video data", target, title, old, videos, err)
	}
}

func TestBookLiveCollectorKeepsCompactJobsWhileTheCatalogIsBusy(t *testing.T) {
	ctx, s, conn := testService(t)
	f := newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.documents["OL100W"]["revision"] = 2
	f.documents["OL100W"]["title"] = "A Later Bibliographic Title"
	f.rows = append([]bookAPIEvent{{ID: "101", Timestamp: f.time.Add(time.Hour).Format(time.RFC3339Nano), Changes: []map[string]any{{"key": "/works/OL100W", "revision": 2}}}}, f.rows...)
	f.mu.Unlock()
	bookFeedDue(t, ctx, conn)
	h, err := s.beginOperation(ctx, "metadata", "openlibrary", "manual", "", "openlibrary")
	if err != nil {
		t.Fatal(err)
	}
	stop := s.keepBookTail(ctx, h, 15)
	defer func() {
		stop()
		h.finish(ctx, nil)
	}()
	deadline := time.Now().Add(8 * time.Second)
	for {
		state, err := s.DB.BookChangesState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.AnchorID == "101" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("long-running work prevented live change collection")
		}
		time.Sleep(25 * time.Millisecond)
	}
	stop()
	var pending int
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title,(SELECT count(*) FROM ingest.ol_change_jobs) FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&title, &pending); err != nil || title != "The Amber Orchard" || pending != 1 {
		t.Fatal("collector mutated the snapshot instead of recording durable jobs", title, pending, err)
	}
}

func TestBookNewInformationReconsidersNativeIdentifiersAfterDependenciesArrive(t *testing.T) {
	ctx, s, conn := testService(t)
	newBookAPIFixture(t, s)
	if err := s.SyncBooks(ctx, "manual"); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('fixture','isbn10','{"title":"An Unrelated Filename EPUB","attributes":{"isbn10":["1861972717"]}}'),
 ('fixture','explicit','{"title":"An Unrelated Filename EPUB","openlibrary_id":"/works/OL500W"}'),
 ('fixture','filename','{"title":"Opaque ISBN 9781861972712 EPUB"}'),
 ('fixture','invalid','{"title":"An Unrelated Filename EPUB","isbn":"9781861972713"}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	edition := books.Record{Revision: 1, Edition: &books.Edition{ID: "OL600M", Works: []string{"OL500W"}, Title: "The Azure Almanac", Normalized: "the azure almanac", ISBN: []string{"9781861972712"}, Authors: []string{}, Languages: []string{}}}
	if err = s.DB.ApplyBookChange(ctx, store.BookChangeJob{ID: "OL600M", Revision: 1, ChangedAt: at}, edition); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	// The edition arrived first. Its canonical work must requeue ISBN-based
	// misses again, even when their release titles have no common words.
	work := books.Record{Revision: 1, Work: &model.AIOWork{Type: "book", OpenLibrary: "OL500W", DisplayTitle: "The Azure Almanac", Years: []int{}}}
	if err = s.DB.ApplyBookChange(ctx, store.BookChangeJob{ID: "OL500W", Revision: 1, ChangedAt: at}, work); err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var matched, invalid int
	if err := conn.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='matched'),count(*) FILTER(WHERE source_id='invalid' AND status='matched') FROM ingest.aio_matches WHERE provider_id='fixture'`).Scan(&matched, &invalid); err != nil || matched != 3 || invalid != 0 {
		var decisions string
		_ = conn.QueryRow(ctx, `SELECT string_agg(source_id||':'||status||':'||reason,', ' ORDER BY source_id) FROM ingest.aio_matches WHERE provider_id='fixture'`).Scan(&decisions)
		t.Fatal("new identifier evidence was missed or an invalid checksum was accepted", matched, invalid, err, decisions)
	}
}
