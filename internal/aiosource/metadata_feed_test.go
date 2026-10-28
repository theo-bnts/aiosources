package aiosource

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTMDBChangePagesCannotSilentlySkipOrRepeatHistory(t *testing.T) {
	for _, name := range []string{"empty intermediate", "empty final", "moving total", "repeated page", "zero pages with results", "valid"} {
		t.Run(name, func(t *testing.T) {
			ctx, s, _ := testService(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/tv/changes" {
					fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				pages, records := 2, fmt.Sprintf(`[{"id":%d}]`, page)
				switch {
				case name == "empty intermediate" && page == 1, name == "empty final" && page == 2:
					records = `[]`
				case name == "moving total" && page == 2:
					pages = 3
				case name == "repeated page":
					records = `[{"id":1}]`
				case name == "zero pages with results":
					pages = 0
				}
				fmt.Fprintf(w, `{"page":%d,"total_pages":%d,"results":%s}`, page, pages, records)
			}))
			defer server.Close()
			s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
			end := time.Now().UTC().Truncate(time.Microsecond)
			err := s.changes(ctx, "synthetic", end.Add(-time.Hour), end)
			state, stateErr := s.DB.AIOAutomationStatus(ctx)
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			if name == "valid" {
				if err != nil || state.ChangesThrough == nil || !state.ChangesThrough.Equal(end) {
					t.Fatal("valid pagination was not acknowledged", state, err)
				}
			} else if !errors.Is(err, ErrInvalidResponse) || state.ChangesThrough != nil {
				t.Fatal("unverifiable change pages advanced freshness", state, err)
			}
		})
	}
}

func TestTMDBFeedFailureStillAppliesOneRetainedBatchWithoutAdvancingFreshness(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "fixture", "one", strings.Repeat("a", 40))
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	if _, err := conn.Exec(ctx, `UPDATE ingest.aio_state SET changes_through=now()-interval '1 hour',applied_through=now()-interval '1 hour',last_sync=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.AIOAutomationStatus(ctx)
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101"}, true); err != nil {
		t.Fatal(err)
	}
	var details atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/movie/changes":
			fmt.Fprint(w, `{"page":1,"total_pages":2,"results":[]}`)
		case "/movie/101":
			details.Add(1)
			fmt.Fprint(w, strings.ReplaceAll(movieDetail, "Example Movie", "Renamed Synthetic Movie"))
		default:
			t.Errorf("unexpected request during a feed-only failure: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	if err := s.Sync(ctx, c); !errors.Is(err, ErrInvalidResponse) {
		t.Fatal("a failed feed was reported as successful", err)
	}
	var title string
	if err := conn.QueryRow(ctx, `SELECT display_title FROM ingest.aio_works WHERE media_type='movie' AND tmdb_id='101'`).Scan(&title); err != nil || title != "Renamed Synthetic Movie" || details.Load() != 1 {
		t.Fatal("a failed feed starved retained information jobs", title, details.Load(), err)
	}
	after, err := s.DB.AIOAutomationStatus(ctx)
	if err != nil || after.ChangesThrough == nil || !after.ChangesThrough.Equal(*before.ChangesThrough) || after.AppliedThrough == nil || !after.AppliedThrough.Equal(*before.AppliedThrough) || after.LastSync == nil || !after.LastSync.Equal(*before.LastSync) {
		t.Fatal("a partial pass advanced catalog freshness", before, after, err)
	}
}
