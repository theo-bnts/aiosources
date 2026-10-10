package aiosource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/theo-bnts/aiosources/internal/model"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func providerTest(t *testing.T, handler http.HandlerFunc) *ProviderHTTP {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p := NewProviderHTTP()
	p.ADBase = server.URL
	p.TBBase = server.URL
	p.Client = server.Client()
	return p
}
func TestTorBoxAvailabilityDistinguishesUnknown(t *testing.T) {
	hash := strings.Repeat("a", 40)
	missing := strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, data         string
		positive, negative int
		failed             bool
	}{
		{"cached", fmt.Sprintf(`{"%s":{"hash":"%s","name":"Example","size":100,"files":[]}}`, hash, hash), 1, 1, false},
		{"absent", `{}`, 0, 2, false}, {"malformed member", fmt.Sprintf(`{"%s":{"hash":"%s","name":"Example"}}`, hash, hash), 0, 0, false},
		{"null", `null`, 0, 0, true}, {"array", `[]`, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/torrents/checkcached" || r.Header.Get("Authorization") != "Bearer synthetic-key" || r.URL.Query().Get("list_files") != "true" {
					t.Errorf("wrong cache request")
				}
				fmt.Fprintf(w, `{"success":true,"data":%s}`, tc.data)
			})
			out, err := p.TBCheck(context.Background(), "synthetic-key", []string{hash, missing})
			if (err != nil) != tc.failed {
				t.Fatalf("error %v", err)
			}
			a, b := 0, 0
			for _, v := range out {
				if v.Cached {
					a++
				} else {
					b++
				}
			}
			if a != tc.positive || b != tc.negative {
				t.Fatalf("got %d positive %d negative", a, b)
			}
		})
	}
}
func TestProviderAuthenticationFailures(t *testing.T) {
	for _, service := range []string{"alldebrid", "torbox"} {
		for _, code := range []int{200, 401, 403, 429} {
			t.Run(fmt.Sprintf("%s_%d", service, code), func(t *testing.T) {
				p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(code)
					fmt.Fprint(w, `{"status":"error","success":false,"error":{"code":"AUTH_BAD_APIKEY"},"detail":"BAD_TOKEN"}`)
				})
				_, err := p.request(context.Background(), service, "key", "GET", "test", nil, "")
				if code == 429 {
					if err == nil || errors.Is(err, ErrAuth) {
						t.Fatalf("rate limit converted to authentication: %v", err)
					}
				} else if !errors.Is(err, ErrAuth) {
					t.Fatalf("got %v", err)
				}
			})
		}
	}
}
func TestAllDebridNestedFilesAndIdentity(t *testing.T) {
	hash := strings.Repeat("a", 40)
	p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/v4/magnet/files":
			if r.Form.Get("id[]") != "7" {
				t.Error("file ID mismatch")
			}
			fmt.Fprint(w, `{"status":"success","data":{"magnets":[{"id":7,"files":[{"n":"Series S01","e":[{"n":"Episode.S01E02.mkv","s":400,"l":"https://files.example/two"}]}]}]}}`)
		case "/v4/magnet/upload":
			fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"id":7,"hash":"%s","ready":true}]}}`, strings.Repeat("b", 40))
		default:
			t.Error("unexpected endpoint")
		}
	})
	files, err := p.ADFiles(context.Background(), "key", 7)
	if err != nil || len(files) != 1 || files[0].Path != "Series S01/Episode.S01E02.mkv" {
		t.Fatalf("files %+v %v", files, err)
	}
	if _, err = p.ADAdd(context.Background(), "key", hash, hash); err == nil {
		t.Error("accepted different hash")
	}
	for _, raw := range []string{`null`, `[{}]`, `[{"n":"x","s":-1,"l":"a"}]`, `[{"n":"x","e":null}]`} {
		if _, err := flattenAD(json.RawMessage(raw), "", 0); err == nil {
			t.Errorf("accepted malformed tree %s", raw)
		}
	}
}
func TestFileSelectionNeverSubstitutesAnotherEpisode(t *testing.T) {
	r, _ := ParseID("tt123:1:2", "series")
	files := []model.AIOFile{{Path: "Show.S01E01.mkv", Size: 9999}, {Path: "Show.S01E02.mkv", Size: 10}, {Path: "sample.S01E02.mkv", Size: 100}, {Path: "Show.S01E02.srt", Size: 10}}
	f, ok := SelectFile(files, r)
	if !ok || f.Path != "Show.S01E02.mkv" {
		t.Fatalf("selected %+v %v", f, ok)
	}
	if _, ok = SelectFile(append(files, model.AIOFile{Path: "duplicate.S01E02.mp4", Size: 20}), r); ok {
		t.Error("accepted ambiguous episode")
	}
	r, _ = ParseID("tt123:1:3", "series")
	if _, ok = SelectFile(files, r); ok {
		t.Error("selected wrong episode")
	}
	r, _ = ParseID("tt123", "movie")
	if f, ok = SelectFile(files, r); !ok || f.Size != 9999 {
		t.Error("did not select largest movie")
	}
}
func TestPlaybackDefersDownloadToPlayer(t *testing.T) {
	hash := strings.Repeat("a", 40)
	requests := []string{}
	p := providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/torrents/createtorrent":
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				t.Error("not multipart")
			}
			if err := r.ParseMultipartForm(8192); err != nil {
				t.Error(err)
			}
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":0,"hash":"%s"}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":"%s","download_finished":true,"download_present":true,"files":[{"id":0,"name":"Show.S01E02.mkv","size":123}]}}`, hash)
		case "/torrents/requestdl":
			if r.URL.Query().Get("file_id") != "0" || r.URL.Query().Get("token") != "key" {
				t.Error("wrong playback query")
			}
			fmt.Fprint(w, `{"success":true,"data":"https://video.example/movie.mkv"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	r, _ := ParseID("tt123:1:2", "series")
	link, err := p.TBPlay(context.Background(), "key", hash, "magnet:?xt=urn:btih:"+hash, r)
	if err != nil || link != "https://video.example/movie.mkv" || len(requests) != 3 {
		t.Fatalf("%s %v %+v", link, err, requests)
	}
	for _, raw := range []string{"http://example.com", "https://u:p@example.com/a", "javascript:alert(1)", "https:///a"} {
		if _, err := HTTPSLink(raw); err == nil {
			t.Error("accepted unsafe URL")
		}
	}
}
func TestProviderCancellationAndBatchLimits(t *testing.T) {
	p := providerTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.pace(ctx, "torbox"); !errors.Is(err, context.Canceled) {
		t.Errorf("pace: %v", err)
	}
	hashes := make([]string, 101)
	for i := range hashes {
		hashes[i] = strings.Repeat("a", 40)
	}
	if _, err := p.TBCheck(context.Background(), "key", hashes); err == nil {
		t.Error("accepted oversize batch")
	}
	if RetryDelay(-1) != 15*time.Minute || RetryDelay(1) != 30*time.Minute {
		t.Error("invalid backoff")
	}
	if _, err := url.Parse("https://example.com"); err != nil {
		t.Fatal(err)
	}
}
