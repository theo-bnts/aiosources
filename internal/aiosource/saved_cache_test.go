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

type savedCacheTransport struct{ calls atomic.Int64 }

func (p *savedCacheTransport) RoundTrip(*http.Request) (*http.Response, error) {
	p.calls.Add(1)
	return nil, errors.New("provider calls are forbidden for saved-only listings")
}

func TestSavedCacheStreamsNeverContactAnyProviderAndKeepUnknownsAndPrivateSafety(t *testing.T) {
	ctx, s, conn := testService(t)
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)}
	for i, hash := range hashes {
		seedMovie(t, ctx, s, conn, "public-source", fmt.Sprint(i), hash)
	}
	seedMovie(t, ctx, s, conn, "private-source", "private", strings.Repeat("e", 40))
	const key = "synthetic-saved-cache-key"
	if err := s.Vault.Put(ctx, "saved-cache-key", key); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.CacheMinutes = 60
	c.AllDebridSecret, c.DebridLinkSecret, c.DeepbridSecret, c.PremiumizeSecret, c.RealDebridSecret, c.TorBoxSecret = "saved-cache-key", "saved-cache-key", "saved-cache-key", "saved-cache-key", "saved-cache-key", "saved-cache-key"
	c.Trackers["public-source"] = model.AIOTracker{Private: false}
	c.Trackers["private-source"] = model.AIOTracker{Private: true}
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	for _, service := range model.DebridServices() {
		for i, hash := range hashes[:3] {
			checked := time.Now().Add(-time.Minute)
			if i == 2 {
				checked = checked.Add(-24 * time.Hour)
			}
			value := model.AIOAvailability{Hash: hash, Cached: i != 1, CheckedAt: checked, Generation: generation, Files: []model.AIOFile{{Path: "Example.mkv", Size: 100}}}
			if err := s.DB.SaveAIOAvailability(ctx, service, fingerprint(key), value); err != nil {
				t.Fatal(err)
			}
			if service != "realdebrid" && service != "deepbrid" {
				if err := s.DB.SaveSharedAvailability(ctx, service, value); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	transport := &savedCacheTransport{}
	s.Providers.Client = &http.Client{Transport: transport}
	token := testPrivateToken(t, ctx, s)
	for _, public := range []bool{false, true} {
		for _, cachedOnly := range []bool{false, true} {
			p := model.StreamPreferences{Mode: "all", SkipCacheCheck: true, CachedOnly: cachedOnly}
			setTestInstallationPreferences(t, ctx, s, token, p)
			var streams []Stream
			var err error
			if public {
				keys := map[string]string{}
				for _, service := range model.DebridServices() {
					keys[service] = key
				}
				streams, err = s.PublicStreams(ctx, "https://addon.example", "synthetic-public-link", "movie", "tt1234567", &PublicInstallation{Services: keys, Preferences: p})
			} else {
				streams, err = s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
			}
			if err != nil {
				t.Fatal(err)
			}
			positive, negative, unknown := 0, 0, 0
			for _, stream := range streams {
				if stream.InfoHash == strings.Repeat("e", 40) {
					t.Fatal("unchecked private source escaped")
				}
				if stream.Hints.Cached == nil {
					unknown++
				} else if *stream.Hints.Cached {
					positive++
				} else {
					negative++
				}
			}
			expectedPositive := 6
			if public {
				expectedPositive = 4
			}
			if cachedOnly {
				if positive != expectedPositive || unknown != 0 || negative != 0 {
					t.Fatal("cached-only saved observations", public, positive, negative, unknown)
				}
			} else if len(streams) != 24 || positive != expectedPositive || negative != expectedPositive || unknown != 24-2*expectedPositive {
				t.Fatal("saved evidence became stale, negative or incomplete", public, len(streams), positive, negative, unknown)
			}
		}
	}
	if transport.calls.Load() != 0 {
		t.Fatal("saved-only listing contacted a provider", transport.calls.Load())
	}
}

func TestSavedCachePreferenceAloneSurvivesPublicURLAndManifestIdentity(t *testing.T) {
	p := model.StreamPreferences{Mode: "all", SkipCacheCheck: true}
	token, err := WithInstallationPreferences("synthetic", p)
	if err != nil || !strings.Contains(token, "~v1~") {
		t.Fatal("saved-only option was discarded", err)
	}
	_, decoded, err := InstallationPreferences(token)
	if err != nil || !decoded.SkipCacheCheck {
		t.Fatal("saved-only preference failed to decode", err)
	}
	if InstallationManifest(p)["id"] == InstallationManifest(model.StreamPreferences{Mode: "all"})["id"] {
		t.Fatal("independent manifests collided")
	}
}
