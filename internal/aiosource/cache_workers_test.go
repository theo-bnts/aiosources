package aiosource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAllDebridChecksOverlapAndNeverDeleteExistingMagnets(t *testing.T) {
	ctx, s, _ := testService(t)
	hashes := []string{}
	status := 4
	magnets := []adMagnet{}
	for i, character := range []string{"a", "b", "c", "d"} {
		hash := strings.Repeat(character, 40)
		hashes = append(hashes, hash)
		magnets = append(magnets, adMagnet{ID: int64(i + 1), Hash: hash, Status: &status})
	}
	inventory, _ := json.Marshal(map[string]any{"status": "success", "data": map[string]any{"magnets": magnets}})
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			_, _ = w.Write(inventory)
		case "/v4/magnet/files":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":%s,"files":[{"n":"movie.mkv","s":100,"l":"https://example.invalid/file"}]}]}}`, r.Form.Get("id[]"))
		default:
			t.Errorf("existing user magnet was uploaded or deleted: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	s.Providers.ADBase, s.Providers.Client = server.URL, server.Client()
	c, _ := s.DB.AIOConfig(ctx)
	if err := s.checkResource(ctx, "alldebrid", "synthetic", "movie:synthetic", hashes, c); err != nil {
		t.Fatal(err)
	}
	items, err := s.DB.AIOAvailability(ctx, "alldebrid", fingerprint("synthetic"), hashes, time.Now().Add(-time.Hour))
	if err != nil || len(items) != 4 || peak.Load() < 2 || peak.Load() > 4 {
		t.Fatal("cache checks were serial or lost results", items, peak.Load(), err)
	}
	for _, item := range items {
		if !item.Cached || len(item.Files) != 1 {
			t.Fatal("existing cached file was lost", item)
		}
	}
	jobs, err := s.DB.AIOCleanup(ctx, fingerprint("synthetic"))
	if err != nil || len(jobs) != 0 {
		t.Fatal("user magnets became cleanup jobs", jobs, err)
	}
}

func TestStremioEpisodeNeedsFileEvidenceForCachedMarker(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	work := model.AIOWork{Type: "tv", TMDB: "202", IMDb: "tt7654321", Titles: []string{"Example Serial"}, Years: []int{2020}}
	if err := s.DB.SaveAIOWork(ctx, work, []string{"example serial"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','1',jsonb_build_object('title','Example.Serial.S01.COMPLETE.2020','info_hash',$1::text,'size',1000))`, hash); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "tb", "synthetic"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TorBoxSecret = "tb"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"success":true,"data":{"%s":{"hash":"%s","files":[]}}}`, hash, hash)
	}))
	t.Cleanup(server.Close)
	s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
	streams, err := s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "series", "tt7654321:1:2")
	if err != nil || len(streams) != 0 {
		t.Fatal("private source without announce exposed an unverified episode", streams, err)
	}
	c, _ = s.DB.AIOConfig(ctx)
	c.Trackers["source"] = model.AIOTracker{Private: false}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	streams, err = s.Streams(ctx, "http://local", testPrivateToken(t, ctx, s), "series", "tt7654321:1:2")
	if err != nil || len(streams) != 1 || strings.Contains(streams[0].Name, "TB+") || !strings.Contains(streams[0].Name, "Cache unknown") {
		t.Fatal("global readiness advertised an unverified episode", streams, err)
	}
	token := testPrivateToken(t, ctx, s)
	setTestInstallationPreferences(t, ctx, s, token, model.StreamPreferences{Mode: "all", CachedOnly: true})
	streams, err = s.Streams(ctx, "http://local", token, "series", "tt7654321:1:2")
	if err != nil || len(streams) != 0 {
		t.Fatal("cached-only offered an episode with unknown file availability", err, len(streams))
	}

}
