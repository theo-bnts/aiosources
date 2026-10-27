package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
)

func seedTestBook(t *testing.T, ctx context.Context, s *Service, conn *pgx.Conn, provider string) int64 {
	t.Helper()
	snapshot := "https://archive.org/download/example/books.gz"
	if _, err := s.DB.BeginOpenLibrary(ctx, snapshot, "fixture"); err != nil {
		t.Fatal(err)
	}
	w := model.AIOWork{Type: "book", OpenLibrary: "OL100W", DisplayTitle: "The Amber Orchard", Titles: []string{"The Amber Orchard"}, Years: []int{2024}, Book: model.BookInformation{AuthorKeys: []string{"OL200A"}, Cover: 12345}}
	if err := s.DB.SaveOpenLibraryBatch(ctx, snapshot, 0, 3, []books.Record{{Work: &w}, {Author: &books.Author{ID: "OL200A", Name: "Morgan Vale", Aliases: []string{}}}, {Edition: &books.Edition{ID: "OL300M", Works: []string{"OL100W"}, Title: "The Amber Orchard", Normalized: "the amber orchard", ISBN: []string{}, Authors: []string{"OL200A"}, Languages: []string{"en"}, Year: 2024}}}); err != nil {
		t.Fatal(err)
	}
	f, _ := json.Marshal(map[string]any{"title": "Morgan Vale - The Amber Orchard EPUB", "info_hash": strings.Repeat("b", 40), "size": 500, "categories": []int{7020}})
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES($1,'book',$2)`, provider, f); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.FinishOpenLibrary(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := conn.QueryRow(ctx, `SELECT id FROM ingest.aio_works WHERE openlibrary_id='OL100W'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBookDownloadSelectsFormatWithoutLeakingAnnouncements(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			work := seedTestBook(t, ctx, s, conn, "fixture")
			if err := s.Vault.Put(ctx, "book_debrid", "synthetic-book-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			if service == "torbox" {
				c.TorBoxSecret = "book_debrid"
			} else {
				c.AllDebridSecret = "book_debrid"
			}
			c.Trackers["fixture"] = model.AIOTracker{Private: true}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload":
					_ = r.ParseForm()
					if strings.Contains(r.Form.Get("magnets[]"), "tr=") {
						t.Error("private announce used")
					}
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"ready":true}]}}`, strings.Repeat("b", 40))
				case "/v4/magnet/delete":
					fmt.Fprint(w, `{"status":"success","data":{"message":"Deleted"}}`)
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"The Amber Orchard.epub","s":100,"l":"https://files.example/epub"},{"n":"The Amber Orchard.pdf","s":200,"l":"https://files.example/pdf"}]}]}}`)
				case "/v4/link/unlock":
					_ = r.ParseForm()
					if r.Form.Get("link") != "https://files.example/epub" {
						t.Error("wrong format selected")
					}
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://files.example/book.epub"}}`)
				case "/torrents/createtorrent":
					if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
						t.Error("cached-only policy bypassed")
					}
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, strings.Repeat("b", 40))
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":true,"download_present":true,"files":[{"id":1,"name":"The Amber Orchard.epub","size":100},{"id":2,"name":"The Amber Orchard.pdf","size":200}]}}`, strings.Repeat("b", 40))
				case "/torrents/requestdl":
					if r.URL.Query().Get("file_id") != "1" {
						t.Error("wrong format selected")
					}
					fmt.Fprint(w, `{"success":true,"data":"https://files.example/book.epub"}`)
				default:
					t.Errorf("unexpected provider request: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			})
			generation, _ := s.DB.AIOCacheGeneration(ctx)
			if err := s.DB.SaveSharedAvailability(ctx, service, model.AIOAvailability{Hash: strings.Repeat("b", 40), Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "The Amber Orchard.epub", Size: 100}, {Path: "The Amber Orchard.pdf", Size: 200}}}); err != nil {
				t.Fatal(err)
			}
			rows, err := s.CatalogReleases(ctx, work, model.CatalogSelection{}, model.ListOptions{})
			if err != nil || len(rows.Items) != 1 || rows.Items[0].Services[0].FileCount != 2 {
				t.Fatalf("book availability: %+v %v", rows, err)
			}
			row := rows.Items[0]
			result, err := s.CatalogDownload(ctx, work, model.CatalogDownloadInput{OccurrenceID: row.OccurrenceID, Fingerprint: row.Fingerprint, Service: service})
			if !errors.Is(err, ErrPending) || len(result.Files) != 2 || requests.Load() != 0 {
				t.Fatalf("local file choice made a provider request: %+v %v", result, err)
			}
			selected, err := s.CheckOperationDownload(ctx, result.OperationID, "The Amber Orchard.epub")
			if err != nil || selected.Status != "ready" || selected.URL != "https://files.example/book.epub" {
				t.Fatalf("book download: %+v %v", selected, err)
			}
		})
	}
}

func TestPublicBooksKeepCachedOnlyAndSourcePrivacy(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	work := seedTestBook(t, ctx, s, conn, "private-one")
	if err := s.Vault.Put(ctx, "book_admin_announce", "https://announce.example.invalid/synthetic-admin-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "book_admin_key", "synthetic-administrator-key"); err != nil {
		t.Fatal(err)
	}
	config, _ := s.DB.AIOConfig(ctx)
	config.TorBoxSecret = "book_admin_key"
	config.Trackers = map[string]model.AIOTracker{"private-one": {Private: true, AnnounceSecret: "book_admin_announce"}}
	if err := s.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-visitor-book-key" {
			t.Error("administrator key borrowed")
		}
		switch r.URL.Path {
		case "/user/me":
			fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
		case "/torrents/createtorrent":
			if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") || strings.Contains(r.FormValue("magnet"), "admin") {
				t.Error("administrator announce leaked or uncached public add allowed")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, strings.Repeat("b", 40))
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":true,"download_present":true,"files":[{"id":1,"name":"The Amber Orchard.epub","size":100},{"id":2,"name":"The Amber Orchard.pdf","size":200}]}}`, strings.Repeat("b", 40))
		case "/torrents/requestdl":
			fmt.Fprint(w, `{"success":true,"data":"https://files.example/public-book.epub"}`)
		case "/torrents/checkcached":
			fmt.Fprintf(w, `{"success":true,"data":{%q:{"hash":%q,"name":"The Amber Orchard","size":100,"files":[{"id":1,"name":"The Amber Orchard.epub","size":100}]}}}`, strings.Repeat("b", 40), strings.Repeat("b", 40))
		default:
			t.Errorf("unexpected public provider request: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled = true
	access.SearchEnabled = true
	access.ExposeSources = false
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	configuration, err := s.CreatePublicInstallation(ctx, "https://addon.example", map[string]string{"torbox": "synthetic-visitor-book-key"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.OpenPublicInstallation(ctx, "https://addon.example", configuration)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := s.PublicCatalogSearch(ctx, visitor, model.ListOptions{Query: "Orchard"}, model.CatalogSearchFilters{Type: "book"}); err != nil || page.Total != 1 || len(page.Items[0].Providers) != 0 || page.Items[0].Cover == "" {
		t.Fatalf("public book search: %+v %v", page, err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	for _, state := range []struct {
		cached  bool
		files   []model.AIOFile
		visible bool
	}{{false, nil, false}, {true, []model.AIOFile{{Path: "readme.txt"}}, false}, {true, []model.AIOFile{{Path: "The Amber Orchard.epub", Size: 100}, {Path: "The Amber Orchard.pdf", Size: 200}}, true}} {
		if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: strings.Repeat("b", 40), Cached: state.cached, Files: state.files, CheckedAt: time.Now(), Generation: generation}); err != nil {
			t.Fatal(err)
		}
		rows, err := s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
		if err != nil || (rows.Total == 1) != state.visible {
			t.Fatalf("public book visibility: %+v %v", rows, err)
		}
		if state.visible {
			row := rows.Items[0]
			if row.Provider != "" || row.ProviderName != "" || row.DownloadToken == "" || row.Services[0].CanAdd {
				t.Fatal("source privacy or private uncached access changed")
			}
			pending, e := s.PublicCatalogDownload(ctx, visitor, work, model.CatalogDownloadInput{DownloadToken: row.DownloadToken, Service: "torbox"})
			if !errors.Is(e, ErrPending) || len(pending.Files) != 2 || pending.PreparationID == "" {
				t.Fatalf("public book choice: %+v %v", pending, e)
			}
			selected, e := s.PublicCatalogDownload(ctx, visitor, work, model.CatalogDownloadInput{PreparationID: pending.PreparationID, CatalogSelection: model.CatalogSelection{File: "The Amber Orchard.epub"}})
			if e != nil || selected.Status != "ready" || selected.URL != "https://files.example/public-book.epub" {
				t.Fatalf("public book download failed: %+v %v", selected, e)
			}
		}
	}
}

func TestBookPreparationsResumeAcrossEveryExtendedProvider(t *testing.T) {
	for _, service := range []string{"realdebrid", "premiumize", "debridlink", "deepbrid"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			work := seedTestBook(t, ctx, s, conn, "fixture")
			if err := s.Vault.Put(ctx, "book_debrid", "synthetic-book-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			setTestDebridSecret(&c, service, "book_debrid")
			c.Trackers["fixture"] = model.AIOTracker{Private: false}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var ready, deleted atomic.Bool
			var adds atomic.Int32
			upstream := extendedPlaybackServer(t, service, strings.Repeat("b", 40), "synthetic-book-key", &ready, &deleted, &adds, "The Amber Orchard.epub")
			defer upstream.Close()
			s.Providers.RDBase, s.Providers.PMBase, s.Providers.DLBase, s.Providers.DBBase = upstream.URL, upstream.URL, upstream.URL, upstream.URL
			rows, err := s.CatalogReleases(ctx, work, model.CatalogSelection{}, model.ListOptions{})
			if err != nil || len(rows.Items) != 1 {
				t.Fatalf("book releases: %+v %v", rows, err)
			}
			row := rows.Items[0]
			pending, err := s.CatalogDownload(ctx, work, model.CatalogDownloadInput{OccurrenceID: row.OccurrenceID, Fingerprint: row.Fingerprint, Service: service, AllowUncached: true})
			if !errors.Is(err, ErrPending) || pending.PreparationID == "" || adds.Load() != 1 {
				t.Fatalf("book preparation not saved: %+v %v", pending, err)
			}
			ready.Store(true)
			result, err := s.CheckOperationDownload(ctx, pending.OperationID)
			if err != nil || result.Status != "ready" || adds.Load() != 1 {
				t.Fatalf("book preparation was lost or recreated: %+v %v", result, err)
			}
			deleted.Store(true)
			_, err = s.CheckOperationDownload(ctx, pending.OperationID)
			if providerFailureCode(err) != "not_found" || adds.Load() != 1 {
				t.Fatalf("deleted book was recreated: %v", err)
			}
		})
	}
}
