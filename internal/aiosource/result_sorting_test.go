package aiosource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestResultSortingDirectionsMissingDatesAndLanguage(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	a := resultRank{size: 10, name: "alpha", source: "zulu", published: &old, cached: 2, language: 3}
	b := resultRank{size: 20, name: "beta", source: "alpha", published: &newer, cached: 1, language: 0}
	for _, tc := range []struct {
		field     string
		ascending int
	}{{"size", -1}, {"name", -1}, {"source", 1}, {"published", -1}, {"age", 1}, {"cached", 1}, {"language", 1}} {
		if got := compareResults(a, b, []model.ResultSort{{Field: tc.field, Direction: "asc"}}); got != tc.ascending {
			t.Fatalf("%s asc: %d", tc.field, got)
		}
		if got := compareResults(a, b, []model.ResultSort{{Field: tc.field, Direction: "desc"}}); got != -tc.ascending {
			t.Fatalf("%s desc: %d", tc.field, got)
		}
	}
	missing := a
	missing.published = nil
	for _, field := range []string{"age", "published"} {
		for _, direction := range []string{"asc", "desc"} {
			if compareResults(missing, a, []model.ResultSort{{Field: field, Direction: direction}}) != 1 {
				t.Fatal("missing date sorted first")
			}
		}
	}
	for _, tc := range []struct {
		codes []string
		rank  int
	}{{[]string{"fr"}, 3}, {[]string{"multi"}, 2}, {nil, 1}, {[]string{"en"}, 0}} {
		if rank := recordRank(model.AIORecord{Title: "Example.2024", Languages: tc.codes}, "", "fr"); rank.language != tc.rank {
			t.Fatal(tc, rank)
		}
	}
}

func TestStreamPriorityAndLimitKeepHistoryInExactResponseOrder(t *testing.T) {
	ranks := []resultRank{{cached: 1, language: 3, size: 100}, {cached: 2, language: 0, size: 200}, {cached: 2, language: 3, size: 50}, {cached: 2, language: 3, size: 150}}
	streams := []Stream{}
	history := []model.StremioResult{}
	for i := range ranks {
		name := fmt.Sprint(i)
		streams = append(streams, Stream{Name: name})
		history = append(history, model.StremioResult{Name: name})
	}
	p := model.StreamPreferences{ResultPreferences: model.ResultPreferences{Sorting: []model.ResultSort{{Field: "cached", Direction: "desc"}, {Field: "language", Direction: "desc"}, {Field: "size", Direction: "desc"}}, MaxResults: 3}}
	out, details := sortStreamResults(streams, history, ranks, p)
	want := []string{"3", "2", "1"}
	for i, name := range want {
		if out[i].Name != name || details[i].Name != name {
			t.Fatal(out, details)
		}
	}
	p.CachedOnly = true
	p.MaxResults = 1
	p.MaxCached = 2
	out, details = sortStreamResults(streams, history, ranks, p)
	if len(out) != 2 || len(details) != 2 || out[0].Name != "3" || out[1].Name != "2" {
		t.Fatal("cached-only did not replace the general result limit", out, details)
	}
}

func TestPublicPreferenceRoundTripIncludesResultControls(t *testing.T) {
	p := model.StreamPreferences{Language: "fr", Mode: "all", ResultPreferences: model.ResultPreferences{MaxCached: 20, MaxResults: 80, ResponseSeconds: 12, Sorting: []model.ResultSort{{Field: "cached", Direction: "desc"}, {Field: "language", Direction: "desc"}, {Field: "size", Direction: "asc"}}}}
	token, err := WithInstallationPreferences("example", p)
	if err != nil {
		t.Fatal(err)
	}
	base, got, err := InstallationPreferences(token)
	if err != nil || base != "example" || !reflect.DeepEqual(got, p) {
		t.Fatal(base, got, err)
	}
	tz := model.PublicTorznabOptions{ResultPreferences: model.ResultPreferences{Sorting: []model.ResultSort{{Field: "name", Direction: "asc"}}, MaxResults: 99, ResponseSeconds: 42}}
	encoded, err := WithPublicTorznabOptions("example", tz)
	if err != nil {
		t.Fatal(err)
	}
	_, decoded, err := PublicTorznabOptions(encoded)
	if err != nil || !reflect.DeepEqual(decoded, tz) {
		t.Fatal(decoded, err)
	}
}

func TestLimitedAvailabilityReusesLocalCacheAndStopsBeforeRemainingHashes(t *testing.T) {
	for _, public := range []bool{false, true} {
		t.Run(fmt.Sprint("public=", public), func(t *testing.T) {
			ctx, s, _ := testService(t)
			hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)}
			if err := s.Vault.Put(ctx, "tb", "synthetic-test-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.TorBoxSecret = "tb"
			c.ResponseSeconds = 5
			if err := s.DB.SetAIOConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			generation, err := s.DB.AIOCacheGeneration(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hashes[0], Cached: true, CheckedAt: time.Now(), Shareable: true, Generation: generation, Files: []model.AIOFile{{Path: "Example.mkv", Size: 100}}}); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			requested := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/torrents/checkcached" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(400)
					return
				}
				values := strings.Split(r.URL.Query().Get("hash"), ",")
				mu.Lock()
				requested = append(requested, values...)
				mu.Unlock()
				data := map[string]any{}
				for _, hash := range values {
					data[hash] = map[string]any{"hash": hash, "name": "Example", "size": 100, "files": []any{map[string]any{"id": 0, "name": "Example.mkv", "size": 100}}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
			}))
			defer server.Close()
			s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
			var visitor *PublicInstallation
			if public {
				visitor = &PublicInstallation{Services: map[string]string{"torbox": "synthetic-visitor-key"}}
			}
			plan, err := s.planAvailability(ctx, c, hashes, visitor)
			if err != nil {
				t.Fatal(err)
			}
			states, err := s.limitedAvailability(ctx, c, "movie:limited", hashes, plan, 2, Resource{Type: "movie"}, nil)
			if err != nil || !states["torbox"][hashes[0]].Cached || !states["torbox"][hashes[1]].Cached {
				t.Fatal(states, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(requested, []string{hashes[1]}) {
				t.Fatalf("budget checked unexpected hashes: %v", requested)
			}
			if _, known := states["torbox"][hashes[2]]; known {
				t.Fatal("unchecked hash fabricated an observation")
			}
		})
	}
}

func TestCachedOnlyStreamLimitReusesAllKnownEvidenceWithoutLosingOtherLanguages(t *testing.T) {
	ctx, s, conn := testService(t)
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)}
	for i, hash := range hashes {
		seedMovie(t, ctx, s, conn, "source", fmt.Sprint(i), hash)
	}
	if err := s.Vault.Put(ctx, "tb", "synthetic-test-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.TorBoxSecret = "tb"
	c.Trackers["source"] = model.AIOTracker{Private: true}
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	for i, hash := range hashes {
		if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Shareable: true, Generation: generation, Files: []model.AIOFile{{Path: fmt.Sprintf("Example.%d.mkv", i), Size: int64((i + 1) * 100)}}}); err != nil {
			t.Fatal(err)
		}
	}
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("known local cache started a provider request")
		w.WriteHeader(500)
	})
	p := model.StreamPreferences{Mode: "all", CachedOnly: true, ResultPreferences: model.ResultPreferences{MaxResults: 1, MaxCached: 2, Sorting: []model.ResultSort{{Field: "size", Direction: "desc"}}}}
	token := testPrivateToken(t, ctx, s)
	setTestInstallationPreferences(t, ctx, s, token, p)
	streams, err := s.Streams(ctx, "http://local", token, "movie", "tt1234567")
	if err != nil || len(streams) != 2 || streams[0].Hints.Size != 300 || streams[1].Hints.Size != 200 {
		t.Fatal("cached-only used the disabled general limit", streams, err)
	}
	visitor := &PublicInstallation{Services: map[string]string{"torbox": "synthetic-visitor-key"}, Preferences: p}
	streams, err = s.PublicStreams(ctx, "http://local", "synthetic-capability", "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 2 {
		t.Fatal(streams, err)
	}
	for _, stream := range streams {
		if stream.InfoHash != "" || len(stream.Sources) > 0 || strings.Contains(stream.URL, "announce") {
			t.Fatal("public private-source response leaked download information")
		}
	}
}

func TestLimitedAvailabilityTimeoutAndQuotaKeepKnownResultsAndLeaveOthersUnknown(t *testing.T) {
	for _, public := range []bool{false, true} {
		for _, failure := range []string{"timeout", "quota"} {
			t.Run(fmt.Sprint(public, "/", failure), func(t *testing.T) {
				ctx, s, _ := testService(t)
				if err := s.Vault.Put(ctx, "tb", "synthetic-test-key"); err != nil {
					t.Fatal(err)
				}
				c, _ := s.DB.AIOConfig(ctx)
				c.TorBoxSecret = "tb"
				c.ResponseSeconds = 1
				if err := s.DB.SetAIOConfig(ctx, c); err != nil {
					t.Fatal(err)
				}
				hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)}
				generation, _ := s.DB.AIOCacheGeneration(ctx)
				if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hashes[0], Cached: true, Shareable: true, Generation: generation, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Example.mkv", Size: 100}}}); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if failure == "quota" {
						w.Header().Set("Retry-After", "60")
						w.WriteHeader(429)
						return
					}
					select {
					case <-r.Context().Done():
					case <-time.After(3 * time.Second):
					}
				}))
				defer server.Close()
				s.Providers.TBBase, s.Providers.Client = server.URL, server.Client()
				var visitor *PublicInstallation
				if public {
					visitor = &PublicInstallation{Services: map[string]string{"torbox": "synthetic-visitor-key"}}
				}
				plan, err := s.planAvailability(ctx, c, hashes, visitor)
				if err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				states, err := s.limitedAvailability(ctx, c, "movie:bounded-error", hashes, plan, 2, Resource{Type: "movie"}, nil)
				if err != nil || time.Since(started) > 2*time.Second || calls.Load() != 1 || len(states["torbox"]) != 1 || !states["torbox"][hashes[0]].Cached {
					t.Fatal("failed check erased known data, fabricated a miss or ignored its budget", err, calls.Load(), states)
				}
			})
		}
	}
}

func TestLimitedAllDebridRetainsNegativeRetryClockAndNeverDeletesExistingItems(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.Vault.Put(ctx, "ad", "synthetic-test-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.AllDebridSecret = "ad"
	c.CacheMinutes = 1440
	c.ResponseSeconds = 5
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 40)
	freshHash := strings.Repeat("b", 40)
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	miss := model.AIOAvailability{Hash: hash, Cached: false, CheckedAt: time.Now().Add(-16 * time.Minute), Generation: generation}
	if err := s.DB.SaveAIOAvailability(ctx, "alldebrid", fingerprint("synthetic-test-key"), miss); err != nil {
		t.Fatal(err)
	}
	freshMiss := miss
	freshMiss.Hash = freshHash
	freshMiss.CheckedAt = time.Now()
	if err := s.DB.SaveAIOAvailability(ctx, "alldebrid", fingerprint("synthetic-test-key"), freshMiss); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":%q,"statusCode":4}]}}`, hash)
		case "/v4/magnet/files":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Example.mkv","s":100,"l":"https://files.example/video"}]}]}}`)
		default:
			t.Error("existing administrator item was submitted or deleted")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	s.Providers.ADBase, s.Providers.Client = server.URL, server.Client()
	for attempt := range 2 {
		plan, err := s.planAvailability(ctx, c, []string{freshHash, hash}, nil)
		if err != nil {
			t.Fatal(err)
		}
		states, err := s.limitedAvailability(ctx, c, "movie:retry-clock", []string{freshHash, hash}, plan, 1, Resource{Type: "movie"}, nil)
		if err != nil || !states["alldebrid"][hash].Cached || len(states["alldebrid"][hash].Files) != 1 || calls.Load() != 2 {
			t.Fatal("negative retry stopped updating, or fresh positive was queried again", attempt, states, calls.Load(), err)
		}
	}
}
