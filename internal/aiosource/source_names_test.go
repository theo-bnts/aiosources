package aiosource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func TestStreamsUseCurrentSourceNamesWithoutChangingPlaybackIdentity(t *testing.T) {
	for _, cached := range []bool{false, true} {
		label := "direct"
		if cached {
			label = "cached-debrid"
		}
		t.Run(label, func(t *testing.T) {
			ctx, s, conn := testService(t)
			registry, err := providers.New(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = registry.Close() })
			s.sources = registry
			const id = "machine-source"
			p := model.Provider{Version: 1, ID: id, Name: "Example Library", Adapter: "http_json", URL: "https://source.example/api"}
			raw, _ := json.Marshal(map[string]any{"version": p.Version, "id": p.ID, "name": p.Name, "adapter": p.Adapter, "url": p.URL, "enabled": false})
			doc, err := registry.Save(id, string(raw), "")
			if err != nil {
				t.Fatal(err)
			}
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, id, "1", hash)
			if cached {
				// Fresh local cache proof must suffice; no external request is needed.
				blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					t.Error("source-name presentation unexpectedly contacted a debrid service")
					w.WriteHeader(http.StatusServiceUnavailable)
				}))
				t.Cleanup(blocked.Close)
				s.Providers.ADBase, s.Providers.TBBase = blocked.URL, blocked.URL
				config, err := s.DB.AIOConfig(ctx)
				if err != nil {
					t.Fatal(err)
				}
				config.AllDebridSecret, config.TorBoxSecret = "display_ad", "display_tb"
				for _, secret := range []string{config.AllDebridSecret, config.TorBoxSecret} {
					if err := s.Vault.Put(ctx, secret, "synthetic-"+secret); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.SaveConfig(ctx, config); err != nil {
					t.Fatal(err)
				}
				rows, err := s.DB.AIOStreams(ctx, "imdb", "tt1234567", "movie")
				if err != nil {
					t.Fatal(err)
				}
				resource, _ := ParseID("tt1234567", "movie")
				generation, err := s.DB.AIOCacheGeneration(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for service, secret := range map[string]string{"alldebrid": config.AllDebridSecret, "torbox": config.TorBoxSecret} {
					now, account := time.Now(), fingerprint("synthetic-"+secret)
					if err := s.DB.SaveAIOAvailability(ctx, service, account, model.AIOAvailability{Generation: generation, Hash: hash, Cached: true, CheckedAt: now}); err != nil {
						t.Fatal(err)
					}
					if err := s.DB.SaveAIOResource(ctx, model.AIOResource{Generation: generation, Service: service, Account: account, Key: streamResourceKey(resource, rows), Hashes: []string{hash}, CheckedAt: now, LastAttempt: now}); err != nil {
						t.Fatal(err)
					}
				}
			}
			token, _ := testPrivateInstallation(t, ctx, s)
			first, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			want := 1
			if cached {
				want = 2
			}
			if err != nil || len(first) != want {
				t.Fatalf("streams: %d, want %d: %v", len(first), want, err)
			}
			p.Name = "Renamed Library é"
			raw, _ = json.Marshal(map[string]any{"version": p.Version, "id": p.ID, "name": p.Name, "adapter": p.Adapter, "url": p.URL, "enabled": false})
			doc, err = registry.Save(id, string(raw), doc.Revision)
			if err != nil {
				t.Fatal(err)
			}
			second, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			if err != nil || len(second) != len(first) {
				t.Fatal("renaming changed available streams", err)
			}
			for i, item := range second {
				if first[i].Hints.Indexer != "Example Library" || item.Hints.Indexer != p.Name || !strings.Contains(item.Title, p.Name) || strings.Contains(item.Name+item.Title, id) {
					t.Fatal("stream presentation did not use the current display name")
				}
				if item.URL != first[i].URL || item.InfoHash != first[i].InfoHash || item.Hints.BingeGroup != first[i].Hints.BingeGroup {
					t.Fatal("display-name change affected playback identity or binge grouping")
				}
				if cached && !strings.Contains(item.URL, "/"+id+"/1/") {
					t.Fatal("playback route replaced the stable source ID")
				}
			}
			requests, err := s.DB.ListStremioRequests(ctx, model.ListOptions{})
			if err != nil || len(requests.Items) != 2 {
				t.Fatal("request history missing", err)
			}
			request, err := s.DB.StremioRequest(ctx, requests.Items[0].ID)
			if err != nil || len(request.Results) != want {
				t.Fatal("request results missing", err)
			}
			for _, item := range request.Results {
				if item.Provider != id || item.ProviderName != p.Name {
					t.Fatal("history did not retain separate stable identity and display name")
				}
			}
			if err := registry.Delete(id, doc.Revision, nil); err != nil {
				t.Fatal(err)
			}
			fallback, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			if err != nil || len(fallback) != want || fallback[0].Hints.Indexer != id || !strings.Contains(fallback[0].Title, id) {
				t.Fatal("removed source definition prevented existing playback", err)
			}
			if err := registry.Close(); err != nil {
				t.Fatal(err)
			}
			fallback, err = s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			if err != nil || len(fallback) != want {
				t.Fatal("unreadable registry prevented existing playback", err)
			}
		})
	}
}
