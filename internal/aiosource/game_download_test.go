package aiosource

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestGameDownloadChoosesArchivesWithoutPrivateAnnouncements(t *testing.T) {
	ctx, s, conn := testService(t)
	_, err := conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years,game_data) VALUES('game','101','Synthetic Odyssey','{2024}','{"platforms":[{"id":6,"name":"PC (Microsoft Windows)","slug":"win"}]}');
 INSERT INTO aiosources.aio_titles SELECT id,'Synthetic Odyssey','synthetic odyssey' FROM aiosources.aio_works WHERE igdb_id='101';
 INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('fixture','game','{"title":"Synthetic.Odyssey.PC","categories":[4050],"info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Vault.Put(ctx, "game_debrid", "synthetic-game-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TorBoxSecret = "game_debrid"
	c.Trackers["fixture"] = model.AIOTracker{Private: true}
	if err = s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-game-key" {
			t.Error("download used another account")
		}
		switch r.URL.Path {
		case "/torrents/createtorrent":
			if r.ParseMultipartForm(4096) != nil || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
				t.Error("private download bypassed cached-only policy")
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, strings.Repeat("a", 40))
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":true,"download_present":true,"files":[{"id":1,"name":"Synthetic.Game.part1.rar","size":100},{"id":2,"name":"Synthetic.Game.part2.rar","size":200}]}}`, strings.Repeat("a", 40))
		case "/torrents/requestdl":
			if r.URL.Query().Get("file_id") != "2" {
				t.Error("wrong game archive selected")
			}
			fmt.Fprint(w, `{"success":true,"data":"https://files.example/game.part2.rar"}`)
		default:
			t.Errorf("unexpected provider request: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	if err = s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: strings.Repeat("a", 40), Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Synthetic.Game.part1.rar", Size: 100}, {Path: "Synthetic.Game.part2.rar", Size: 200}}}); err != nil {
		t.Fatal(err)
	}
	titles, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Synthetic Odyssey"}, model.CatalogSearchFilters{Type: "game"})
	if err != nil || titles.Total != 1 {
		t.Fatal("game search did not find matched archives", err)
	}
	work := titles.Items[0].ID
	rows, err := s.CatalogReleases(ctx, work, model.CatalogSelection{}, model.ListOptions{})
	if err != nil || len(rows.Items) != 1 || rows.Items[0].Services[0].FileCount != 2 {
		t.Fatal("game archives were treated as videos", err)
	}
	result, err := s.CatalogDownload(ctx, work, model.CatalogDownloadInput{OccurrenceID: rows.Items[0].OccurrenceID, Fingerprint: rows.Items[0].Fingerprint, Service: "torbox"})
	if !errors.Is(err, ErrPending) || len(result.Files) != 2 || requests.Load() != 0 {
		t.Fatal("game download skipped local file choice", err)
	}
	selected, err := s.CheckOperationDownload(ctx, result.OperationID, "Synthetic.Game.part2.rar")
	if err != nil || selected.Status != "ready" || selected.URL != "https://files.example/game.part2.rar" {
		t.Fatal("game file could not be downloaded", err)
	}
	for _, scope := range []string{"books", "videos"} {
		if err = s.Reset(ctx, scope); err != nil {
			t.Fatal(err)
		}
		var games, cached int
		err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='game'),(SELECT count(*) FROM aiosources.aio_shared_availability WHERE hash=repeat('a',40))`).Scan(&games, &cached)
		if err != nil || games != 1 || cached != 1 {
			t.Fatal("unrelated reset removed games or their cache", scope, err)
		}
	}
}
