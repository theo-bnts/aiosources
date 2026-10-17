package aiosource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPrivateSourceWithoutAnnounceRequiresVerifiedCachedStreams(t *testing.T) {
	for _, tc := range []struct {
		name, cache               string
		private, announce, debrid bool
		want                      int
		cachedOnly                bool
	}{
		{"private direct without announce", "unknown", true, false, false, 0, false},
		{"private unknown without announce", "unknown", true, false, true, 0, false},
		{"private uncached without announce", "uncached", true, false, true, 0, false},
		{"private cached without announce", "cached", true, false, true, 1, false},
		{"private direct with announce", "unknown", true, true, false, 1, false},
		{"private unknown with announce", "unknown", true, true, true, 1, false},
		{"private uncached with announce", "uncached", true, true, true, 1, false},
		{"public unknown", "unknown", false, false, true, 1, false},
		{"public uncached", "uncached", false, false, true, 1, false},
		{"cached only excludes direct announces", "unknown", true, true, false, 0, true},
		{"cached only excludes unknown with announce", "unknown", true, true, true, 0, true},
		{"cached only excludes absent with announce", "uncached", true, true, true, 0, true},
		{"cached only keeps cached without announce", "cached", true, true, true, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "source", "one", hash)
			c, _ := s.DB.AIOConfig(ctx)
			tracker := model.AIOTracker{Private: tc.private}
			if tc.announce {
				if err := s.Vault.Put(ctx, "announce", "https://tracker.example/synthetic-key/announce"); err != nil {
					t.Fatal(err)
				}
				tracker.AnnounceSecret = "announce"
			}
			c.Trackers["source"] = tracker
			if tc.debrid {
				if err := s.Vault.Put(ctx, "tb", "synthetic-key"); err != nil {
					t.Fatal(err)
				}
				c.TorBoxSecret = "tb"
			}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			if tc.cache != "unknown" {
				generation, _ := s.DB.AIOCacheGeneration(ctx)
				availability := model.AIOAvailability{Generation: generation, Hash: hash, Cached: tc.cache == "cached", CheckedAt: time.Now()}
				if availability.Cached {
					availability.Files = []model.AIOFile{{Path: "Example.Movie.2021.mp4", Size: 100}}
				}
				if err := s.DB.SaveSharedAvailability(ctx, "torbox", availability); err != nil {
					t.Fatal(err)
				}
			}
			s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/torrents/checkcached" {
					t.Error("stream request initiated playback")
				}
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, "synthetic-private-error-data")
			})
			token := testPrivateToken(t, ctx, s)
			setTestInstallationPreferences(t, ctx, s, token, model.StreamPreferences{Mode: "all", CachedOnly: tc.cachedOnly})
			streams, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			if err != nil || len(streams) != tc.want {
				t.Fatalf("unsafe private-source selection: %d streams, want %d, %v", len(streams), tc.want, err)
			}
			if tc.private && !tc.announce && len(streams) > 0 && (streams[0].Hints.Cached == nil || !*streams[0].Hints.Cached) {
				t.Fatal("unverified cache was offered without an announce")
			}
			if tc.cachedOnly {
				encoded, _ := json.Marshal(streams)
				if strings.Contains(string(encoded), "synthetic-key") || strings.Contains(string(encoded), "tracker.example") {
					t.Fatal("cached-only response disclosed an announce")
				}
				for _, stream := range streams {
					if len(stream.Sources) > 0 || strings.HasPrefix(stream.URL, "magnet:") {
						t.Fatal("cached-only response offered direct torrent playback")
					}
				}
			}
			requests, err := s.DB.ListStremioRequests(ctx, model.ListOptions{})
			if err != nil || requests.Total != 1 || requests.Items[0].Returned != tc.want {
				t.Fatal("filtering absent from Stremio history", err)
			}
		})
	}
}

func TestPrivateCachedOnlyPlaybackNeverFallsBackToAdministratorAnnounce(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "source", "one", hash)
			if err := s.Vault.Put(ctx, "announce", "https://announce.example.invalid/admin-private-passkey/announce"); err != nil {
				t.Fatal(err)
			}
			if err := s.Vault.Put(ctx, "debrid", "synthetic-debrid-key"); err != nil {
				t.Fatal(err)
			}
			config, _ := s.DB.AIOConfig(ctx)
			config.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "announce"}
			if service == "alldebrid" {
				config.AllDebridSecret = "debrid"
			} else {
				config.TorBoxSecret = "debrid"
			}
			if err := s.SaveConfig(ctx, config); err != nil {
				t.Fatal(err)
			}
			var adds atomic.Int32
			s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload":
					adds.Add(1)
					_ = r.ParseForm()
					if strings.Contains(r.FormValue("magnets[]"), "tr=") || strings.Contains(r.FormValue("magnets[]"), "admin-private-passkey") {
						t.Error("administrator announce sent for cached-only playback")
					}
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"ready":false}]}}`, hash)
				case "/v4/magnet/delete":
					fmt.Fprint(w, `{"status":"success","data":{"message":"Deleted"}}`)
				case "/torrents/createtorrent":
					adds.Add(1)
					_ = r.ParseMultipartForm(4096)
					if r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") || strings.Contains(r.FormValue("magnet"), "admin-private-passkey") {
						t.Error("cached-only preparation leaked an announce or allowed an uncached add")
					}
					fmt.Fprint(w, `{"success":false,"error":"DOWNLOAD_NOT_CACHED","detail":"No longer cached"}`)
				default:
					t.Error("uncached playback proceeded after a cache miss")
					w.WriteHeader(500)
				}
			})
			preferences := model.StreamPreferences{Mode: "all", CachedOnly: true}
			link, err := s.PlaybackWithPreferences(ctx, service, "source", "one", "movie", "tt1234567", preferences)
			if !(errors.Is(err, ErrPending) || providerFailureCode(err) == "not_cached") || link != "" || adds.Load() != 1 {
				t.Fatal("cache miss failed to preserve cached-only policy", err, adds.Load())
			}
		})
	}
}
