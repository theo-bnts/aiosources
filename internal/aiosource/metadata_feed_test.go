package aiosource

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
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
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_state SET changes_through=now()-interval '1 hour',applied_through=now()-interval '1 hour',last_sync=now()-interval '1 hour'`); err != nil {
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
	if err := conn.QueryRow(ctx, `SELECT display_title FROM aiosources.aio_works WHERE media_type='movie' AND tmdb_id='101'`).Scan(&title); err != nil || title != "Renamed Synthetic Movie" || details.Load() != 1 {
		t.Fatal("a failed feed starved retained information jobs", title, details.Load(), err)
	}
	after, err := s.DB.AIOAutomationStatus(ctx)
	if err != nil || after.ChangesThrough == nil || !after.ChangesThrough.Equal(*before.ChangesThrough) || after.AppliedThrough == nil || !after.AppliedThrough.Equal(*before.AppliedThrough) || after.LastSync == nil || !after.LastSync.Equal(*before.LastSync) {
		t.Fatal("a partial pass advanced catalog freshness", before, after, err)
	}
}

func TestTMDBLiveDayTotalPagesShiftQueuesAllIDsWithoutAdvancingCheckpoint(t *testing.T) {
	day1 := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	start := day1.Add(12 * time.Hour)
	end := day2.Add(14 * time.Hour)

	for _, tc := range []struct {
		name           string
		start          time.Time
		wantErr        error
		wantCheckpoint *time.Time
		wantIDs        []string
		wantMaxPage    int
	}{
		{
			name:           "shrink_with_tail_pages",
			start:          start,
			wantCheckpoint: &day2,
			wantIDs:        []string{"1", "2", "10", "20", "30", "40"},
			wantMaxPage:    4,
		},
		{
			name:           "shrink_ending_at_lower_total",
			start:          day2.Add(8 * time.Hour),
			wantCheckpoint: nil,
			wantIDs:        []string{"10", "20"},
			wantMaxPage:    3,
		},
		{
			name:           "live_growth_with_boundary_overlap",
			start:          day2.Add(8 * time.Hour),
			wantCheckpoint: nil,
			wantIDs:        []string{"10", "20", "30", "40"},
			wantMaxPage:    3,
		},
		{
			name:           "collapse_to_zero",
			start:          day2.Add(8 * time.Hour),
			wantErr:        ErrInvalidResponse,
			wantCheckpoint: nil,
			wantIDs:        []string{},
			wantMaxPage:    2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, s, conn := testService(t)
			var mu sync.Mutex
			maxRequestedPage := 0
			var stabilized atomic.Bool
			day1Requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/tv/changes" {
					fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
					return
				}
				startDate := r.URL.Query().Get("start_date")
				endDate := r.URL.Query().Get("end_date")
				if startDate != endDate {
					t.Errorf("expected per-day change query, got %s..%s", startDate, endDate)
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if startDate == day1.Format("2006-01-02") {
					mu.Lock()
					day1Requests++
					mu.Unlock()
					fmt.Fprintf(w, `{"page":%d,"total_pages":2,"results":[{"id":%d}]}`, page, page)
					return
				}
				mu.Lock()
				if page > maxRequestedPage {
					maxRequestedPage = page
				}
				mu.Unlock()
				if stabilized.Load() {
					fmt.Fprintf(w, `{"page":%d,"total_pages":4,"results":[{"id":%d}]}`, page, page*10)
					return
				}
				switch tc.name {
				case "shrink_with_tail_pages":
					// Mirrors TMDB CDN skew where page 1 reports 4 pages, page 2 is
					// stale-cached at 2 pages, and pages 3..4 serve fresh tail results.
					pages := 4
					if page == 2 {
						pages = 2
					}
					fmt.Fprintf(w, `{"page":%d,"total_pages":%d,"results":[{"id":%d}]}`, page, pages, page*10)
				case "shrink_ending_at_lower_total":
					switch page {
					case 1:
						fmt.Fprint(w, `{"page":1,"total_pages":4,"results":[{"id":10}]}`)
					case 2:
						fmt.Fprint(w, `{"page":2,"total_pages":2,"results":[{"id":20}]}`)
					default:
						fmt.Fprintf(w, `{"page":%d,"total_pages":2,"results":[]}`, page)
					}
				case "live_growth_with_boundary_overlap":
					switch page {
					case 1:
						fmt.Fprint(w, `{"page":1,"total_pages":2,"results":[{"id":10},{"id":20}]}`)
					case 2:
						fmt.Fprint(w, `{"page":2,"total_pages":3,"results":[{"id":20},{"id":30}]}`)
					default:
						fmt.Fprint(w, `{"page":3,"total_pages":3,"results":[{"id":40}]}`)
					}
				case "collapse_to_zero":
					if page == 1 {
						fmt.Fprint(w, `{"page":1,"total_pages":4,"results":[{"id":10}]}`)
					} else {
						fmt.Fprint(w, `{"page":2,"total_pages":0,"results":[]}`)
					}
				}
			}))
			defer server.Close()
			s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()

			err := s.changes(ctx, "synthetic", tc.start, end)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error on live day shift: %v", err)
			}

			state, stateErr := s.DB.AIOAutomationStatus(ctx)
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			if tc.wantCheckpoint == nil {
				if state.ChangesThrough != nil {
					t.Fatalf("live day shift advanced freshness checkpoint: %+v", state.ChangesThrough)
				}
			} else if state.ChangesThrough == nil || !state.ChangesThrough.Equal(*tc.wantCheckpoint) {
				t.Fatalf("checkpoint = %v, want %v", state.ChangesThrough, *tc.wantCheckpoint)
			}

			var queued []string
			if err := conn.QueryRow(ctx, `SELECT ARRAY(SELECT tmdb_id FROM aiosources.aio_metadata_jobs WHERE media_type='movie' ORDER BY tmdb_id::bigint)`).Scan(&queued); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(queued, tc.wantIDs) {
				t.Fatalf("queued IDs = %v, want %v", queued, tc.wantIDs)
			}
			mu.Lock()
			gotMaxPage := maxRequestedPage
			prevDay1Requests := day1Requests
			mu.Unlock()
			if gotMaxPage != tc.wantMaxPage {
				t.Fatalf("max requested page = %d, want %d", gotMaxPage, tc.wantMaxPage)
			}

			if tc.name == "shrink_with_tail_pages" {
				// Once the live day stabilizes, resuming from the saved checkpoint
				// must not re-fetch the already-closed prior day and must advance
				// ChangesThrough to end.
				stabilized.Store(true)
				nextEnd := end.Add(15 * time.Minute)
				if err := s.changes(ctx, "synthetic", *state.ChangesThrough, nextEnd); err != nil {
					t.Fatalf("stabilized pass failed: %v", err)
				}
				after, err := s.DB.AIOAutomationStatus(ctx)
				if err != nil || after.ChangesThrough == nil || !after.ChangesThrough.Equal(nextEnd) {
					t.Fatalf("stabilized pass did not advance checkpoint: %+v %v", after, err)
				}
				mu.Lock()
				afterDay1Requests := day1Requests
				mu.Unlock()
				if afterDay1Requests != prevDay1Requests {
					t.Fatalf("already-closed day was re-queried: before=%d after=%d", prevDay1Requests, afterDay1Requests)
				}
			}
		})
	}
}

func TestTMDBClosedDayMovingTotalStillFailsAndPreservesPriorDays(t *testing.T) {
	day1 := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	day3 := day2.AddDate(0, 0, 1)
	for _, direction := range []string{"decrease", "increase"} {
		t.Run(direction, func(t *testing.T) {
			ctx, s, conn := testService(t)
			var day3Requested atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/tv/changes" {
					fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
					return
				}
				day := r.URL.Query().Get("start_date")
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				switch day {
				case day1.Format("2006-01-02"):
					fmt.Fprintf(w, `{"page":%d,"total_pages":2,"results":[{"id":%d}]}`, page, page)
				case day2.Format("2006-01-02"):
					pages := 4
					if direction == "decrease" && page == 2 {
						pages = 2
					} else if direction == "increase" && page == 1 {
						pages = 2
					}
					fmt.Fprintf(w, `{"page":%d,"total_pages":%d,"results":[{"id":%d}]}`, page, pages, page*10)
				default:
					day3Requested.Store(true)
					fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
				}
			}))
			defer server.Close()
			s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()

			err := s.changes(ctx, "synthetic", day1.Add(10*time.Hour), day3.Add(10*time.Hour))
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("closed day moving total was accepted: %v", err)
			}
			if day3Requested.Load() {
				t.Fatal("pagination proceeded past an inconsistent closed day")
			}
			state, stateErr := s.DB.AIOAutomationStatus(ctx)
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			if state.ChangesThrough == nil || !state.ChangesThrough.Equal(day2) {
				t.Fatalf("expected day1 checkpoint %v, got %v", day2, state.ChangesThrough)
			}
			var queued []string
			if err := conn.QueryRow(ctx, `SELECT ARRAY(SELECT tmdb_id FROM aiosources.aio_metadata_jobs WHERE media_type='movie' ORDER BY tmdb_id::bigint)`).Scan(&queued); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(queued, []string{"1", "2"}) {
				t.Fatalf("partial closed day polluted metadata queue: %v", queued)
			}
		})
	}
}

func TestTMDBFeedFailureDrainsMultipleBatchesWithoutAdvancingFreshness(t *testing.T) {
	ctx, s, conn := testService(t)
	if err := s.Vault.Put(ctx, "tmdb", "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	policy, err := s.DB.RuntimeSection(ctx, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	policy.Values["workers"], policy.Values["queue_batch"], policy.Values["publication_batch"] = 2, 2, 2
	if err := s.DB.UpdateRuntimeSection(ctx, "metadata", policy); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TMDBSecret = "tmdb"
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_state SET changes_through=now()-interval '1 hour',applied_through=now()-interval '1 hour',last_sync=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	before, _ := s.DB.AIOAutomationStatus(ctx)
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101", "102", "103", "104", "105"}, true); err != nil {
		t.Fatal(err)
	}
	var details atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/movie/changes":
			fmt.Fprint(w, `{"page":1,"total_pages":2,"results":[]}`)
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			id := strings.TrimPrefix(r.URL.Path, "/movie/")
			details.Add(1)
			fmt.Fprint(w, strings.Replace(movieDetail, `"id":101`, `"id":`+id, 1))
		default:
			t.Errorf("unexpected request during feed-only failure: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()
	if err := s.Sync(ctx, c); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
	var saved, pending int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='movie'),(SELECT count(*) FROM aiosources.aio_metadata_jobs)`).Scan(&saved, &pending); err != nil || saved != 5 || pending != 0 || details.Load() != 5 {
		t.Fatalf("feed failure starved multi-batch backlog: saved=%d pending=%d details=%d err=%v", saved, pending, details.Load(), err)
	}
	after, err := s.DB.AIOAutomationStatus(ctx)
	if err != nil || after.ChangesThrough == nil || !after.ChangesThrough.Equal(*before.ChangesThrough) || after.AppliedThrough == nil || !after.AppliedThrough.Equal(*before.AppliedThrough) || after.LastSync == nil || !after.LastSync.Equal(*before.LastSync) {
		t.Fatal("feed-only drain advanced freshness checkpoints", before, after, err)
	}
}

func TestTMDBLiveDayShiftDuringFullSyncCompletesBacklogAndRecoversOnNextPass(t *testing.T) {
	ctx, s, conn := testService(t)
	c := prepareMetadataPipeline(t, s)
	prior := utcDay(time.Now().UTC())
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_state SET changes_through=$1,applied_through=$1`, prior); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueueAIOMetadata(ctx, "movie", []string{"101", "102"}, true); err != nil {
		t.Fatal(err)
	}
	var stabilized atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/movie/changes":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			pages := 4
			if !stabilized.Load() && page == 2 {
				pages = 2
			}
			fmt.Fprintf(w, `{"page":%d,"total_pages":%d,"results":[{"id":%d}]}`, page, pages, 200+page)
		case r.URL.Path == "/tv/changes":
			fmt.Fprint(w, `{"page":1,"total_pages":0,"results":[]}`)
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			id := strings.TrimPrefix(r.URL.Path, "/movie/")
			fmt.Fprint(w, strings.Replace(movieDetail, `"id":101`, `"id":`+id, 1))
		default:
			t.Errorf("unexpected TMDB path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	s.TMDBBase, s.MetadataHTTP = server.URL, server.Client()

	if err := s.Sync(ctx, c); err != nil {
		t.Fatalf("live-day total_pages skew failed full sync: %v", err)
	}
	status, err := s.DB.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending != 0 || status.Works != 6 || status.SyncFailures != 0 || status.SyncError != "" {
		t.Fatalf("backlog or sync state unexpected after live-day shift: %+v", status)
	}
	if status.ChangesThrough == nil || !status.ChangesThrough.Equal(prior) || status.AppliedThrough == nil || !status.AppliedThrough.Equal(prior) {
		t.Fatalf("live-day shift prematurely advanced freshness: %+v", status)
	}

	stabilized.Store(true)
	if err := s.Sync(ctx, c); err != nil {
		t.Fatalf("stabilized sync failed: %v", err)
	}
	after, err := s.DB.AIOStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Pending != 0 || after.SyncFailures != 0 || after.ChangesThrough == nil || !after.ChangesThrough.After(prior) || after.AppliedThrough == nil || !after.AppliedThrough.Equal(*after.ChangesThrough) {
		t.Fatalf("stabilized sync did not catch up freshness: %+v", after)
	}
}
