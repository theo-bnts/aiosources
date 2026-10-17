package aiosource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAllDebridFileListAcceptsDocumentedStringMagnetIDs(t *testing.T) {
	for _, fixture := range []struct {
		id    string
		valid bool
	}{
		{`"7"`, true}, {`7`, true}, {`"8"`, false}, {`null`, false}, {`"7.0"`, false}, {`-7`, false},
	} {
		t.Run(fixture.id, func(t *testing.T) {
			p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.URL.Path != "/v4/magnet/files" || r.Form.Get("id[]") != "7" {
					t.Error("incorrect file-list request")
				}
				fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":%s,"files":[{"n":"Example.mkv","s":100,"l":"https://files.example/selected"}]}]}}`, fixture.id)
			})
			files, err := p.ADFiles(context.Background(), "synthetic-key", 7)
			if fixture.valid {
				if err != nil || len(files) != 1 || files[0].Link != "https://files.example/selected" {
					t.Fatalf("documented file-list response rejected: %+v %v", files, err)
				}
			} else if err == nil {
				t.Fatal("wrong or malformed magnet identity was accepted")
			}
		})
	}
}

func TestPlaybackFailuresAreSpecificRepeatableAndDoNotExposeProviderData(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "example", "1", hash)
			const key = "synthetic-private-provider-key"
			if err := s.Vault.Put(ctx, "debrid", key); err != nil {
				t.Fatal(err)
			}
			c, err := s.DB.AIOConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4.1/magnet/status":
					fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":"%s","statusCode":4}]}}`, hash)
				case "/torrents/createtorrent":
					fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":"%s"}}`, hash)
				case "/v4/magnet/files", "/torrents/mylist":
					fmt.Fprint(w, key+" https://files.example/private-playback-link")
				default:
					t.Error("invalid file data reached playback link generation")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			s.Providers.ADBase, s.Providers.TBBase, s.Providers.Client = server.URL, server.URL, server.Client()
			for range 2 {
				if _, err := s.Playback(ctx, service, "example", "1", "movie", "tt1234567"); err == nil {
					t.Fatal("malformed provider response was accepted")
				}
			}
			logs, err := s.DB.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: service})
			if err != nil {
				t.Fatal(err)
			}
			failures := 0
			for _, log := range logs.Items {
				if log.Kind != "debrid.playback_failed" {
					continue
				}
				failures++
				if log.Data["operation"] != "file_list" || log.Data["failure_code"] != "parse" || !strings.Contains(log.Message, "reading the file list") {
					t.Fatalf("playback failure has no useful stage: %+v", log)
				}
				encoded, _ := json.Marshal(log)
				if strings.Contains(string(encoded), key) || strings.Contains(string(encoded), "files.example") || strings.Contains(string(encoded), hash) {
					t.Fatal("playback diagnostic contains private provider data")
				}
			}
			if failures != 2 {
				t.Fatalf("repeated failures were hidden by incident deduplication: %d", failures)
			}
		})
	}
}

func TestAllDebridCachedStreamResolvesPlaybackWithStringFileListID(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "example", "1", hash)
	if err := s.Vault.Put(ctx, "debrid", "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.AllDebridSecret = "debrid"
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	var unlocks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		switch r.URL.Path {
		case "/v4.1/magnet/status":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":"%s","statusCode":4}]}}`, hash)
		case "/v4/magnet/files":
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":"7","files":[{"n":"Example.Movie.2021.mkv","s":100,"l":"https://files.example/selected"}]}]}}`)
		case "/v4/link/unlock":
			unlocks.Add(1)
			if r.Form.Get("link") != "https://files.example/selected" {
				t.Error("wrong file sent to unlock")
			}
			fmt.Fprint(w, `{"status":"success","data":{"link":"https://video.example/movie.mkv"}}`)
		default:
			t.Error("playback attempted to add or delete an existing account magnet")
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	s.Providers.ADBase, s.Providers.Client = server.URL, server.Client()
	token, err := testPrivateInstallation(t, ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := s.Streams(ctx, "https://addon.example", token, "movie", "tt1234567")
	if err != nil || len(streams) != 1 || !strings.HasPrefix(streams[0].Name, "[AD+]") || unlocks.Load() != 0 {
		t.Fatalf("cached stream failed or unlocked before playback: %+v %v", streams, err)
	}
	link, err := s.Playback(ctx, "alldebrid", "example", "1", "movie", "tt1234567")
	if err != nil || link != "https://video.example/movie.mkv" || unlocks.Load() != 1 {
		t.Fatalf("cached stream did not resolve its playback link: %q %v", link, err)
	}
}
