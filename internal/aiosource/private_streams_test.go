package aiosource

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPrivateSourceWithoutAnnounceRequiresVerifiedCachedStreams(t *testing.T) {
	for _, tc := range []struct {
		name, cache               string
		private, announce, debrid bool
		want                      int
	}{
		{"private direct without announce", "unknown", true, false, false, 0},
		{"private unknown without announce", "unknown", true, false, true, 0},
		{"private uncached without announce", "uncached", true, false, true, 0},
		{"private cached without announce", "cached", true, false, true, 1},
		{"private direct with announce", "unknown", true, true, false, 1},
		{"private unknown with announce", "unknown", true, true, true, 1},
		{"private uncached with announce", "uncached", true, true, true, 1},
		{"public unknown", "unknown", false, false, true, 1},
		{"public uncached", "uncached", false, false, true, 1},
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
			streams, err := s.Streams(ctx, "https://addon.example", "token", "movie", "tt1234567")
			if err != nil || len(streams) != tc.want {
				t.Fatalf("unsafe private-source selection: %d streams, want %d, %v", len(streams), tc.want, err)
			}
			if tc.private && !tc.announce && len(streams) > 0 && (streams[0].Hints.Cached == nil || !*streams[0].Hints.Cached) {
				t.Fatal("unverified cache was offered without an announce")
			}
			requests, err := s.DB.ListStremioRequests(ctx, model.ListOptions{})
			if err != nil || requests.Total != 1 || requests.Items[0].Returned != tc.want {
				t.Fatal("filtering absent from Stremio history", err)
			}
		})
	}
}
