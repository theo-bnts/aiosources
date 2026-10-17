package aiosource

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSavedInstallationFiltersAreIndependentAndRejectURLOverrides(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "alpha", "one", strings.Repeat("a", 40))
	seedMovie(t, ctx, s, conn, "beta", "two", strings.Repeat("b", 40))
	token, _ := testPrivateInstallation(t, ctx, s)
	for _, source := range []string{"alpha", "beta"} {
		p := model.StreamPreferences{Language: "fr", Mode: "unknown", Sources: []string{source}, Types: []string{"movie"}}
		link, err := testPrivateInstallation(t, ctx, s)
		setTestInstallationPreferences(t, ctx, s, link, p)
		if err != nil || !s.Authorized(ctx, link) {
			t.Fatal("preferences changed authorization", err)
		}
		streams, err := s.Streams(ctx, "https://addon.example", link, "movie", "tt1234567")
		if err != nil || len(streams) != 1 || streams[0].InfoHash != map[string]string{"alpha": strings.Repeat("a", 40), "beta": strings.Repeat("b", 40)}[source] {
			t.Fatal("connections contaminated each other", source, len(streams), err)
		}
		if manifest, err := s.PrivateManifest(ctx, link); err != nil || manifest["id"] == Manifest()["id"] {
			t.Fatal("independent configuration kept the base manifest identity")
		}
	}
	for _, p := range []model.StreamPreferences{{Mode: "all", Sources: []string{"missing"}}, {Mode: "all", Types: []string{"series"}}, {Mode: "all", Services: []string{"torbox"}}, {Mode: "all", CachedOnly: true}} {
		link, err := testPrivateInstallation(t, ctx, s)
		setTestInstallationPreferences(t, ctx, s, link, p)
		if err != nil {
			t.Fatal(err)
		}
		streams, err := s.Streams(ctx, "https://addon.example", link, "movie", "tt1234567")
		if err != nil || len(streams) != 0 {
			t.Fatal("excluded connection fell back to direct torrents", p, len(streams), err)
		}
	}
	streams, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
	if err != nil || len(streams) != 2 {
		t.Fatal("URL filters changed base installation", len(streams), err)
	}
	if _, err = s.PlaybackWithPreferences(ctx, "torbox", "beta", "two", "movie", "tt1234567", model.StreamPreferences{Sources: []string{"alpha"}}); err == nil {
		t.Fatal("filtered source accepted by playback")
	}
	for _, data := range []string{`{"mode":"all","sources":["bad/source"]}`, `{"mode":"all","services":["unknown"]}`, `{"mode":"all","types":["book"]}`, `{"mode":"all","sources":["alpha","alpha"]}`, `{"mode":"all","private_key":"no"}`, `{"mode":"all"} {}`} {
		bad := token + "~v1~" + base64.RawURLEncoding.EncodeToString([]byte(data))
		if s.Authorized(ctx, bad) {
			t.Fatal("malformed options authenticated", data)
		}
	}
	before, _ := s.DB.AIOConfig(ctx)
	encoded, _ := json.Marshal(before)
	if strings.Contains(string(encoded), "sources") || strings.Contains(string(encoded), "cached_only") {
		t.Fatal("URL-only filters persisted in global configuration")
	}
}

func TestPublicCachedOnlyURLNeverProvidesAnnounceOrStartsAnUncachedTorrent(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "alpha", "one", hash)
	c, _ := s.DB.AIOConfig(ctx)
	c.Trackers["alpha"] = model.AIOTracker{Private: false}
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/torrents/createtorrent" || r.FormValue("add_only_if_cached") != "true" || strings.Contains(r.FormValue("magnet"), "tr=") {
			t.Error("cached-only installation permitted an uncached add or announce")
		}
		fmt.Fprint(w, `{"success":false,"error":"DOWNLOAD_NOT_CACHED","detail":"Cache no longer contains this torrent"}`)
	})
	visitor := &PublicInstallation{Services: map[string]string{"torbox": "synthetic-visitor-key"}, Preferences: model.StreamPreferences{Mode: "all", Services: []string{"torbox"}, CachedOnly: true}}
	if _, err := s.PublicPlayback(ctx, "torbox", "alpha", "one", "movie", "tt1234567", visitor); err == nil || calls.Load() != 1 {
		t.Fatal("cached-only playback did not fail closed", calls.Load(), err)
	}
}

func TestInstallationFiltersNeverContactAnUnselectedProvider(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "alpha", "one", strings.Repeat("a", 40))
	if err := s.Vault.Put(ctx, "ad-key", "synthetic-ad-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.AllDebridSecret = "ad-key"
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	token, _ := testPrivateInstallation(t, ctx, s)
	setTestInstallationPreferences(t, ctx, s, token, model.StreamPreferences{Mode: "all", Services: []string{"torbox"}})
	link := token
	var calls atomic.Int64
	s.Providers.Client = &http.Client{Transport: metadataRoundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected provider request")
	})}
	streams, err := s.Streams(ctx, "https://addon.example", link, "movie", "tt1234567")
	if err != nil || len(streams) != 0 || calls.Load() != 0 {
		t.Fatal("unselected provider was queried", len(streams), calls.Load(), err)
	}
	// Both configured keys deliberately lack a valid credential. Direct torrent
	// access must not read them or create availability jobs.
	c, _ = s.DB.AIOConfig(ctx)
	if err := s.Vault.Put(ctx, "tb-key", "synthetic-tb-key"); err != nil {
		t.Fatal(err)
	}
	c.TorBoxSecret = "tb-key"
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for name, key := range map[string]string{"ad-key": "synthetic-ad-key", "tb-key": "synthetic-tb-key"} {
			if err := s.Vault.Put(ctx, name, key); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := conn.Exec(ctx, `UPDATE ingest.secrets SET ciphertext='invalid'::bytea WHERE name IN ('ad-key','tb-key')`); err != nil {
		t.Fatal(err)
	}
	setTestInstallationPreferences(t, ctx, s, token, model.StreamPreferences{Mode: "all", Services: []string{"direct"}})
	streams, err = s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
	if err != nil || len(streams) != 1 || streams[0].URL != "" || calls.Load() != 0 {
		t.Fatal("direct-only request checked a debrid service", len(streams), calls.Load(), err)
	}
	var jobs int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.operation_runs WHERE kind='cache'`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("direct-only request scheduled a cache check", jobs, err)
	}
}
