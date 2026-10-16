package aiosource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCatalogDownloadUsesExistingProvidersAndSanitizedHistory(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("b", 40)
			seedMovie(t, ctx, s, conn, "source", "one", hash)
			const key = "synthetic-private-catalog-key"
			if err := s.Vault.Put(ctx, "debrid", key); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			c.Trackers["source"] = model.AIOTracker{Private: true}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var uploads atomic.Int32
			var pending atomic.Bool
			s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload":
					uploads.Add(1)
					_ = r.ParseForm()
					if strings.Contains(r.Form.Get("magnets[]"), "tr=") {
						t.Error("private announce was included in a cached-only request")
					}
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"ready":%t}]}}`, hash, !pending.Load())
				case "/v4/magnet/delete":
					fmt.Fprint(w, `{"status":"success","data":{"message":"Deleted"}}`)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Example.Movie.2021.mkv","s":100,"l":"https://files.example/selected"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://files.example/ephemeral-download"}}`)
				case "/torrents/createtorrent":
					uploads.Add(1)
					if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
						t.Error("cached-only download policy was bypassed")
					}
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":%t,"download_present":%t,"files":[{"id":1,"name":"Example.Movie.2021.mkv","size":100}]}}`, hash, !pending.Load(), !pending.Load())
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://files.example/ephemeral-download"}`)
				default:
					t.Errorf("unexpected provider path %s", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example.Movie"}, "")
			if err != nil || page.Total != 1 {
				t.Fatalf("title search: %+v %v", page, err)
			}
			rows, err := s.DB.CatalogRecords(ctx, page.Items[0].ID)
			if err != nil || len(rows) != 1 {
				t.Fatal(err)
			}
			input := model.CatalogDownloadInput{Fingerprint: rows[0].Fingerprint, OccurrenceID: rows[0].OccurrenceID, Service: service, AllowUncached: true}
			result, err := s.CatalogDownload(ctx, page.Items[0].ID, input)
			if err != nil || result.Status != "ready" || result.URL != "https://files.example/ephemeral-download" {
				t.Fatalf("download: %+v %v", result, err)
			}
			pending.Store(true)
			result, err = s.CatalogDownload(ctx, page.Items[0].ID, input)
			if !errors.Is(err, ErrPending) || result.Status != "pending" || result.URL != "" {
				t.Fatalf("pending response: %+v %v", result, err)
			}
			history, err := s.DB.ListOperations(ctx, model.OperationFilter{Group: "debrid"})
			if err != nil || history.Total != 2 {
				t.Fatalf("download history absent: %+v %v", history, err)
			}
			for _, run := range history.Items {
				if run.Kind != "download" || run.Status != "succeeded" || run.Processed != 1 {
					t.Fatalf("incorrect download history: %+v", run)
				}
				events, e := s.DB.OperationEvents(ctx, run.ID, model.ListOptions{Limit: 200})
				if e != nil || events.Total < 4 {
					t.Fatalf("provider calls absent: %+v %v", events, e)
				}
				encoded, _ := json.Marshal(struct {
					Run    model.OperationRun
					Events any
				}{run, events})
				if strings.Contains(string(encoded), key) || strings.Contains(string(encoded), hash) || strings.Contains(string(encoded), "files.example") {
					t.Fatal("download credentials leaked to history")
				}
			}
			before := uploads.Load()
			input.Fingerprint = "old-fingerprint"
			if _, err = s.CatalogDownload(ctx, page.Items[0].ID, input); !errors.Is(err, model.ErrConflict) || uploads.Load() != before {
				t.Fatal("stale displayed release reached provider", err)
			}
			input.Fingerprint = rows[0].Fingerprint
			input.OccurrenceID = strings.Repeat("f", 64)
			if _, err = s.CatalogDownload(ctx, page.Items[0].ID, input); !errors.Is(err, model.ErrNotFound) || uploads.Load() != before {
				t.Fatal("forged occurrence reached provider", err)
			}
			if _, err = conn.Exec(ctx, `UPDATE ingest.aio_works SET revision=revision+1`); err != nil {
				t.Fatal(err)
			}
			input.OccurrenceID = rows[0].OccurrenceID
			if _, err = s.CatalogDownload(ctx, page.Items[0].ID, input); !errors.Is(err, model.ErrNotFound) || uploads.Load() != before {
				t.Fatal("stale match reached provider", err)
			}
		})
	}
}

func TestCatalogEpisodeSelectionUsesVerifiedFilesAndSeasonPacks(t *testing.T) {
	ctx, s, conn := testService(t)
	w := model.AIOWork{Type: "tv", TMDB: "202", IMDb: "tt2345678", Titles: []string{"Example Series"}, Years: []int{2021}}
	if err := s.DB.SaveAIOWork(ctx, w, []string{"example series"}); err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"Example.Series.S01.COMPLETE.1080p", "Example.Series.S01E02.1080p", "Example.Series.S02E02.1080p"} {
		if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source',$1,jsonb_build_object('title',$2::text,'info_hash',$3::text,'imdb_id','tt2345678'))`, fmt.Sprint(i), title, strings.Repeat(fmt.Sprint(i+1), 40)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "debrid", "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TorBoxSecret = "debrid"
	c.Trackers["source"] = model.AIOTracker{Private: true}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	for _, hash := range []string{strings.Repeat("1", 40), strings.Repeat("2", 40)} {
		if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: generation, Hash: hash, Cached: true, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Example.Series.S01E02.mkv", Size: 100}}}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example Series"}, "tv")
	if err != nil || page.Total != 1 {
		t.Fatalf("series missing: %+v %v", page, err)
	}
	season, episode := 1, 2
	selection := model.CatalogSelection{Season: &season, Episode: &episode}
	releases, err := s.CatalogReleases(ctx, page.Items[0].ID, selection, model.ListOptions{Sort: "size_desc"})
	if err != nil || releases.Total != 2 {
		t.Fatalf("episode lost its pack or retained wrong season: %+v %v", releases, err)
	}
	for _, release := range releases.Items {
		if release.Services[0].State != "cached" || release.Services[0].CanAdd {
			t.Fatalf("incorrect cache/private policy: %+v", release)
		}
	}
	for _, invalid := range []model.CatalogSelection{{}, {Season: &season}, {Episode: &episode}} {
		if _, err := s.CatalogReleases(ctx, page.Items[0].ID, invalid, model.ListOptions{}); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("ambiguous episode accepted", err)
		}
	}
	pageEnd, err := s.CatalogReleases(ctx, page.Items[0].ID, selection, model.ListOptions{Offset: int(^uint(0) >> 1)})
	if err != nil || pageEnd.Total != 2 || len(pageEnd.Items) != 0 {
		t.Fatalf("out-of-range page lost its total or overflowed: %+v %v", pageEnd, err)
	}
}

func TestCatalogUncachedPreparationAndReadinessPreserveOwnedDownloads(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("d", 40)
			seedMovie(t, ctx, s, conn, "source", "one", hash)
			const announce = "https://announce.example/synthetic-catalog-passkey/announce"
			if err := s.Vault.Put(ctx, "debrid", "synthetic-debrid-key"); err != nil {
				t.Fatal(err)
			}
			if err := s.Vault.Put(ctx, "announce", announce); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "announce"}
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var uploads atomic.Int32
			var ready atomic.Bool
			s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					if uploads.Load() == 0 {
						fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
					} else {
						status := 0
						if ready.Load() {
							status = 4
						}
						fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"statusCode":%d}]}}`, hash, status)
					}
				case "/v4/magnet/upload":
					if uploads.Add(1) != 1 {
						t.Error("readiness duplicated a persistent AllDebrid download")
					}
					_ = r.ParseForm()
					magnet, _ := url.Parse(r.Form.Get("magnets[]"))
					if magnet.Query().Get("tr") != announce {
						t.Error("explicit uncached preparation lost its announce URL")
					}
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"ready":false}]}}`, hash)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Example.Movie.2021.mkv","s":100,"l":"https://files.example/selected"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://files.example/ready"}}`)
				case "/torrents/createtorrent":
					_ = r.ParseMultipartForm(4096)
					first := uploads.Add(1) == 1
					magnet, _ := url.Parse(r.FormValue("magnet"))
					if first && (r.FormValue("add_only_if_cached") != "" || magnet.Query().Get("tr") != announce) {
						t.Error("uncached preparation was not explicit or lost its announce")
					}
					if !first && (r.FormValue("add_only_if_cached") != "true" || magnet.Query().Get("tr") != "") {
						t.Error("readiness could start an uncached download")
					}
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":%t,"download_present":%t,"files":[{"id":1,"name":"Example.Movie.2021.mkv","size":100}]}}`, hash, ready.Load(), ready.Load())
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://files.example/ready"}`)
				default:
					t.Errorf("readiness deleted or unexpectedly contacted %s", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example Movie"}, "")
			if err != nil || page.Total != 1 {
				t.Fatal(err)
			}
			rows, err := s.DB.CatalogRecords(ctx, page.Items[0].ID)
			if err != nil || len(rows) != 1 {
				t.Fatal(err)
			}
			input := model.CatalogDownloadInput{Fingerprint: rows[0].Fingerprint, OccurrenceID: rows[0].OccurrenceID, Service: service, AllowUncached: true}
			if result, err := s.CatalogDownload(ctx, page.Items[0].ID, input); !errors.Is(err, ErrPending) || result.Status != "pending" {
				t.Fatalf("uncached preparation: %+v %v", result, err)
			}
			input.AllowUncached = false
			if _, err := s.CatalogDownload(ctx, page.Items[0].ID, input); !errors.Is(err, ErrPending) {
				t.Fatal("readiness lost pending download", err)
			}
			ready.Store(true)
			if result, err := s.CatalogDownload(ctx, page.Items[0].ID, input); err != nil || result.URL != "https://files.example/ready" {
				t.Fatalf("completed download unavailable: %+v %v", result, err)
			}
		})
	}
}

func TestCachedOnlyAdministratorCleanupCanBeRetriedAfterFailure(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("e", 40)
	seedMovie(t, ctx, s, conn, "source", "one", hash)
	const key = "synthetic-cleanup-key"
	if err := s.Vault.Put(ctx, "debrid", key); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.AllDebridSecret = "debrid"
	c.Trackers["source"] = model.AIOTracker{Private: true}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	var failed atomic.Bool
	var added atomic.Bool
	failed.Store(true)
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			if added.Load() {
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"statusCode":0}]}}`, hash)
			} else {
				fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
			}
		case "/v4/magnet/upload":
			added.Store(true)
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"ready":false}]}}`, hash)
		case "/v4/magnet/delete":
			if failed.Load() {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"status":"error","error":{"code":"MAINTENANCE"}}`)
			} else {
				added.Store(false)
				fmt.Fprint(w, `{"status":"success","data":{"message":"Deleted"}}`)
			}
		default:
			t.Error("unexpected cached-only request")
			w.WriteHeader(500)
		}
	})
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example Movie"}, "")
	if err != nil || page.Total != 1 {
		t.Fatal(err)
	}
	rows, err := s.DB.CatalogRecords(ctx, page.Items[0].ID)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	_, err = s.CatalogDownload(ctx, page.Items[0].ID, model.CatalogDownloadInput{Fingerprint: rows[0].Fingerprint, OccurrenceID: rows[0].OccurrenceID, Service: "alldebrid"})
	if !errors.Is(err, ErrPending) {
		t.Fatal("non-cached torrent was incorrectly returned", err)
	}
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(jobs) != 1 || !jobs[0].Failed || jobs[0].ID != 7 || jobs[0].Hash != hash {
		t.Fatalf("cleanup ownership was lost: %+v %v", jobs, err)
	}
	failed.Store(false)
	if err := s.RecoverCleanup(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	jobs, err = s.DB.AIOCleanup(ctx, fingerprint(key))
	if err != nil || len(jobs) != 0 {
		t.Fatal("cleanup could not recover", err)
	}
}

func TestCatalogSeasonChoicesKeepEverySeasonInAnAnchoredPack(t *testing.T) {
	ctx, s, conn := testService(t)
	w := model.AIOWork{Type: "tv", TMDB: "202", IMDb: "tt2345678", Titles: []string{"Example Series"}, Years: []int{2021}, SeasonYears: map[int]int{1: 2021, 2: 2022, 3: 2023}}
	if err := s.DB.SaveAIOWork(ctx, w, []string{"example series"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','pack','{"title":"Example.Series.S01-S03.COMPLETE.1080p","season":1,"imdb_id":"tt2345678","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example Series"}, "")
	if err != nil || page.Total != 1 {
		t.Fatalf("anchored pack was not identified: %+v %v", page, err)
	}
	title, err := s.CatalogTitle(ctx, page.Items[0].ID)
	if err != nil || !slices.Equal(title.Seasons, []int{1, 2, 3}) {
		t.Fatalf("pack anchor hid available seasons: %+v %v", title, err)
	}
	season, episode := 2, 1
	releases, err := s.CatalogReleases(ctx, title.ID, model.CatalogSelection{Season: &season, Episode: &episode}, model.ListOptions{})
	if err != nil || releases.Total != 1 {
		t.Fatalf("pack unavailable for another season: %+v %v", releases, err)
	}
}
