package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCompleteAnnounceURLsPreserveCredentialPosition(t *testing.T) {
	urls := []string{
		"https://tracker.example/synthetic-key/announce",
		"https://tracker.example/tracker/synthetic-key/announce.php",
		"https://tracker.example/announce/synthetic-key",
		"https://tracker.example/announce?passkey=synthetic%2Fkey&source=one",
		"https://tracker.example/announce?source=one&passkey=synthetic%2Bkey&mode=two",
		"http://tracker.example:8080/synthetic%2fkey/announce?key=a%2Bb%26c&key=second",
		"udp://tracker.example:6969/announce?passkey=synthetic-key",
	}
	for _, announce := range urls {
		t.Run(announce, func(t *testing.T) {
			ctx, s, _ := testService(t)
			if err := s.Vault.Put(ctx, "announce_url", announce); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "announce_url"}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			magnet, sources, err := s.magnet(ctx, model.AIORecord{Provider: "source", Hash: strings.Repeat("a", 40), Title: "Synthetic movie"}, c)
			parsed, parseErr := url.Parse(magnet)
			if err != nil || parseErr != nil || parsed.Query().Get("tr") != announce || len(sources) != 1 || sources[0] != "tracker:"+announce {
				t.Fatal("the exact announce URL was not preserved")
			}
			stored, err := s.DB.Setting(ctx, "aiosource.config.v2")
			if err != nil || strings.Contains(stored, "synthetic") || strings.Contains(stored, "tracker.example") || !strings.Contains(stored, `"announce_secret":"announce_url"`) {
				t.Fatal("configuration must contain only the secret reference")
			}
			ciphertext, err := s.DB.Secret(ctx, "announce_url")
			if err != nil || strings.Contains(string(ciphertext), announce) {
				t.Fatal("announce URL was not encrypted")
			}
			if err = s.Vault.Delete(ctx, "announce_url"); !errors.Is(err, model.ErrConflict) {
				t.Fatal("a referenced announce secret was not protected")
			}
			c, _ = s.DB.AIOConfig(ctx)
			c.Trackers["source"] = model.AIOTracker{Private: true}
			if err = s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			if err = s.Vault.Delete(ctx, "announce_url"); err != nil {
				t.Fatal("an unreferenced announce secret could not be deleted")
			}
		})
	}
}

func TestAnnounceValidationRejectsUnsafeOrMalformedValuesWithoutDisclosure(t *testing.T) {
	for _, value := range []string{
		"synthetic-key", "", "https:///announce?passkey=synthetic-key",
		"https://user:synthetic-key@tracker.example/announce",
		"https://tracker.example/announce#synthetic-key",
		"ftp://tracker.example/synthetic-key", "javascript:synthetic-key",
		"https://tracker.example:bad/announce?passkey=synthetic-key",
		"https://tracker.example/%zz?passkey=synthetic-key",
		"https://tracker.example/announce?passkey=synthetic key",
		"https://tracker.example/announce?passkey=synthetic-key\n",
		"https://tracker.example/announce?passkey=synthetic-key\x00",
		"https://tracker.example/" + strings.Repeat("x", 8192),
	} {
		err := validateAnnounce(value)
		if !errors.Is(err, model.ErrInvalid) || strings.Contains(err.Error(), "synthetic-key") || strings.Contains(err.Error(), "tracker.example") {
			t.Fatal("invalid announce value was accepted or disclosed")
		}
	}
}

func TestAnnounceSecretRotationIsValidatedBeforeUse(t *testing.T) {
	ctx, s, _ := testService(t)
	c, _ := s.DB.AIOConfig(ctx)
	c.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "announce_url"}
	if err := s.Vault.Put(ctx, "announce_url", "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(ctx, c); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("a passkey alone was accepted as a complete URL")
	}
	if err := s.Vault.Put(ctx, "announce_url", "https://tracker.example/first/announce"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	const updated = "https://tracker.example/second/announce?passkey=synthetic%2Fkey"
	if err := s.Vault.Put(ctx, "announce_url", updated); err != nil {
		t.Fatal(err)
	}
	value, err := s.trackerAnnounce(ctx, c.Trackers["source"])
	if err != nil || value != updated {
		t.Fatal("valid URL rotation was not used")
	}
	if err = s.Vault.Put(ctx, "announce_url", "https://user:synthetic-key@tracker.example/announce"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.trackerAnnounce(ctx, c.Trackers["source"]); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("invalid URL rotation was used")
	}
}

func TestLegacyAnnounceMigrationIsAtomicAndRunsAtStartup(t *testing.T) {
	ctx, s, _ := testService(t)
	if err := s.Vault.Put(ctx, "old_passkey", "synthetic/key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.Trackers["source"] = model.AIOTracker{Private: true, Announce: "https://tracker.example/announce", PasskeySecret: "old_passkey"}
	// Another configuration wins before the legacy client saves its stale draft.
	if err := s.DB.SetAIOConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Vault.List(ctx)
	if err := s.SaveConfig(ctx, c); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale migration was accepted")
	}
	after, _ := s.Vault.List(ctx)
	if len(before) != len(after) {
		t.Fatal("failed migration left orphaned secrets")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, s.DB, s.Vault, s.sources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := restarted.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	current, _ := s.DB.AIOConfig(ctx)
	tracker := current.Trackers["source"]
	if tracker.AnnounceSecret == "" || tracker.Announce != "" || tracker.PasskeySecret != "" || !tracker.Private {
		t.Fatal("legacy configuration was not converted")
	}
	value, err := restarted.trackerAnnounce(ctx, tracker)
	if err != nil || value != "https://tracker.example/announce/synthetic%2Fkey" {
		t.Fatal("migration changed the previous playback URL")
	}
	setting, _ := s.DB.Setting(ctx, "aiosource.config.v2")
	if strings.Contains(setting, "tracker.example") || strings.Contains(setting, "passkey_secret") || strings.Contains(setting, `"announce":`) {
		t.Fatal("legacy plaintext settings remain")
	}
	if _, err = s.Vault.Resolve(ctx, "old_passkey"); err != nil {
		t.Fatal("migration deleted an existing user secret")
	}
	if err = restarted.migrateTrackerSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := s.DB.AIOConfig(ctx)
	if unchanged.Revision != current.Revision || unchanged.Trackers["source"].AnnounceSecret != tracker.AnnounceSecret {
		t.Fatal("migration was not idempotent")
	}
}

func TestPlaybackProvidersReceiveTheExactFullAnnounceURL(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "source", "1", hash)
			const announce = "https://tracker.example/synthetic%2Fkey/announce?source=one&passkey=synthetic%2Bkey&mode=two"
			if err := s.Vault.Put(ctx, "tracker_url", announce); err != nil {
				t.Fatal(err)
			}
			if err := s.Vault.Put(ctx, "provider_key", "synthetic-provider-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.Trackers["source"] = model.AIOTracker{Private: true, AnnounceSecret: "tracker_url"}
			if service == "alldebrid" {
				c.AllDebridSecret = "provider_key"
			} else {
				c.TorBoxSecret = "provider_key"
			}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			uploaded := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload", "/torrents/createtorrent":
					var magnet string
					if service == "alldebrid" {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						magnet = r.Form.Get("magnets[]")
					} else {
						if err := r.ParseMultipartForm(1 << 20); err != nil {
							t.Error(err)
						}
						defer r.MultipartForm.RemoveAll()
						magnet = r.FormValue("magnet")
					}
					parsed, err := url.Parse(magnet)
					if err != nil || parsed.Query().Get("tr") != announce {
						t.Error("playback altered the announce credential position or encoding")
					}
					if uploaded {
						t.Error("playback uploaded the same torrent twice")
					}
					uploaded = true
					if service == "alldebrid" {
						fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":42,"hash":"%s","ready":true}]}}`, hash)
					} else {
						fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","torrent_id":42}}`, hash)
					}
				case "/v4/magnet/files":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":42,"files":[{"n":"Example.Movie.2021.mkv","s":100,"l":"https://host.example/file"}]}]}}`)
				case "/v4/link/unlock":
					fmt.Fprint(w, `{"status":"success","data":{"link":"https://video.example/movie.mkv"}}`)
				case "/torrents/mylist":
					fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":true,"download_present":true,"files":[{"id":0,"name":"Example.Movie.2021.mkv","size":100}]}}`, hash)
				case "/torrents/requestdl":
					fmt.Fprint(w, `{"success":true,"data":"https://video.example/movie.mkv"}`)
				default:
					t.Error("unexpected provider request")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			s.Providers.Client = server.Client()
			s.Providers.ADBase, s.Providers.TBBase = server.URL, server.URL
			link, err := s.Playback(ctx, service, "source", "1", "movie", "tt1234567")
			if err != nil || link != "https://video.example/movie.mkv" || !uploaded {
				t.Fatal("private playback did not complete", err)
			}
			// The test owns the mock server lifecycle. No account recovery should
			// attempt a real or already-closed provider endpoint during teardown.
			c, _ = s.DB.AIOConfig(ctx)
			c.AllDebridSecret, c.TorBoxSecret = "", ""
			if err = s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
		})
	}
}
