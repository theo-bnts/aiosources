package aiosource

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func setTestDebridSecret(c *model.AIOConfig, service, ref string) {
	switch service {
	case "realdebrid":
		c.RealDebridSecret = ref
	case "premiumize":
		c.PremiumizeSecret = ref
	case "debridlink":
		c.DebridLinkSecret = ref
	case "deepbrid":
		c.DeepbridSecret = ref
	}
}

// These responses follow the providers' documented torrent lifecycles. The
// state change models an external download finishing, then being deleted.
func extendedPlaybackServer(t *testing.T, service, hash, key string, ready, deleted *atomic.Bool, adds *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("another account's credential used")
			w.WriteHeader(401)
			return
		}
		switch service {
		case "realdebrid":
			switch r.URL.Path {
			case "/torrents":
				if ready.Load() && !deleted.Load() {
					fmt.Fprintf(w, `[{"id":"17","hash":"%s","status":"downloaded"}]`, hash)
				} else {
					fmt.Fprint(w, `[]`)
				}
			case "/torrents/addMagnet":
				adds.Add(1)
				w.WriteHeader(201)
				fmt.Fprint(w, `{"id":"17"}`)
			case "/torrents/info/17":
				if deleted.Load() {
					w.WriteHeader(404)
					fmt.Fprint(w, `{"error_code":7,"error":"not found"}`)
				} else if !ready.Load() {
					fmt.Fprintf(w, `{"id":"17","hash":"%s","status":"magnet_conversion"}`, hash)
				} else {
					fmt.Fprintf(w, `{"id":"17","hash":"%s","status":"downloaded","files":[{"id":1,"path":"/Example.Movie.2021.mkv","bytes":100,"selected":1}],"links":["https://provider.example/video"]}`, hash)
				}
			case "/unrestrict/link":
				fmt.Fprint(w, `{"download":"https://cdn.example/ready.mkv"}`)
			default:
				t.Error("unexpected Real-Debrid operation")
				w.WriteHeader(400)
			}
		case "premiumize":
			switch r.URL.Path {
			case "/cache/check":
				fmt.Fprintf(w, `{"status":"success","response":[%t]}`, ready.Load() && !deleted.Load())
			case "/transfer/create":
				adds.Add(1)
				fmt.Fprint(w, `{"status":"success","id":"17"}`)
			case "/transfer/list":
				if deleted.Load() {
					fmt.Fprint(w, `{"status":"success","transfers":[]}`)
				} else if ready.Load() {
					fmt.Fprint(w, `{"status":"success","transfers":[{"id":"17","status":"finished"}]}`)
				} else {
					fmt.Fprint(w, `{"status":"success","transfers":[{"id":"17","status":"running"}]}`)
				}
			case "/transfer/directdl":
				fmt.Fprint(w, `{"status":"success","content":[{"path":"Example.Movie.2021.mkv","size":100,"link":"https://cdn.example/ready.mkv"}]}`)
			default:
				t.Error("unexpected Premiumize operation")
				w.WriteHeader(400)
			}
		case "debridlink":
			if r.URL.Path == "/seedbox/add" {
				var body struct {
					URL string `json:"url"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				if !ready.Load() && body.URL == hash {
					fmt.Fprint(w, `{"success":false,"error":"notAddTorrent"}`)
					return
				}
				if strings.HasPrefix(body.URL, "magnet:") {
					adds.Add(1)
				}
			} else if r.URL.Path != "/seedbox/list" {
				t.Error("unexpected Debrid-Link operation")
				w.WriteHeader(400)
				return
			}
			if deleted.Load() {
				fmt.Fprint(w, `{"success":true,"value":[]}`)
				return
			}
			item := fmt.Sprintf(`{"id":"17","hashString":"%s","downloadPercent":0,"isZip":false,"files":[]}`, hash)
			if ready.Load() {
				item = fmt.Sprintf(`{"id":"17","hashString":"%s","downloadPercent":100,"isZip":false,"files":[{"name":"Example.Movie.2021.mkv","size":100,"downloadUrl":"https://cdn.example/ready.mkv","downloadPercent":100}]}`, hash)
			}
			if r.URL.Path == "/seedbox/list" {
				item = "[" + item + "]"
			}
			fmt.Fprintf(w, `{"success":true,"value":%s}`, item)
		case "deepbrid":
			if r.URL.Path == "/torrents/add" {
				adds.Add(1)
				fmt.Fprint(w, `{"error":0,"id":17}`)
				return
			}
			if r.URL.Path != "/torrents/info" {
				t.Error("unexpected Deepbrid operation")
				w.WriteHeader(400)
				return
			}
			if deleted.Load() {
				fmt.Fprint(w, `{"error":1,"message":"No data"}`)
			} else if !ready.Load() {
				fmt.Fprint(w, `{"error":0,"id":"17","status":"downloading"}`)
			} else {
				fmt.Fprint(w, `{"error":0,"id":"17","status":"downloaded","filename":"Example.Movie.2021.mkv","links":["https://cdn.example/ready.mkv"]}`)
			}
		}
	}))
}

func TestExtendedDownloadsResumeAndReportDeletionWithoutRecreating(t *testing.T) {
	for _, service := range []string{"realdebrid", "premiumize", "debridlink", "deepbrid"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "source", "one", hash)
			if err := s.Vault.Put(ctx, "provider-key", "synthetic-key"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			setTestDebridSecret(&c, service, "provider-key")
			c.Trackers = map[string]model.AIOTracker{"source": {Private: false}}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var ready, deleted atomic.Bool
			var adds atomic.Int32
			upstream := extendedPlaybackServer(t, service, hash, "synthetic-key", &ready, &deleted, &adds)
			defer upstream.Close()
			s.Providers.RDBase, s.Providers.PMBase, s.Providers.DLBase, s.Providers.DBBase = upstream.URL, upstream.URL, upstream.URL, upstream.URL
			titles, err := s.CatalogSearch(ctx, model.ListOptions{Query: "Example", Limit: 50}, model.CatalogSearchFilters{})
			if err != nil || len(titles.Items) != 1 {
				t.Fatal("missing local title", err)
			}
			work := titles.Items[0].ID
			releases, err := s.CatalogReleases(ctx, work, model.CatalogSelection{}, model.ListOptions{})
			if err != nil || len(releases.Items) != 1 || len(releases.Items[0].Services) != 1 || releases.Items[0].Services[0].Service != service {
				t.Fatalf("service missing from Search: %+v %v", releases, err)
			}
			row := releases.Items[0]
			pending, err := s.CatalogDownload(ctx, work, model.CatalogDownloadInput{OccurrenceID: row.OccurrenceID, Fingerprint: row.Fingerprint, Service: service, AllowUncached: true})
			if !errors.Is(err, ErrPending) || pending.Status != "pending" || pending.PreparationID == "" || adds.Load() != 1 {
				t.Fatalf("preparation not retained: %+v %v", pending, err)
			}
			saved, err := s.DB.OperationDownload(ctx, pending.OperationID)
			if err != nil || saved.Input.PreparationID != pending.PreparationID {
				t.Fatal("saved continuation lost", err)
			}
			ready.Store(true)
			result, err := s.CheckOperationDownload(ctx, pending.OperationID)
			if err != nil || result.URL != "https://cdn.example/ready.mkv" || result.OperationID != pending.OperationID || adds.Load() != 1 {
				t.Fatalf("readiness did not reuse the transfer: %+v %v", result, err)
			}
			deleted.Store(true)
			_, err = s.CheckOperationDownload(ctx, pending.OperationID)
			if providerFailureCode(err) != "not_found" || adds.Load() != 1 {
				t.Fatalf("deleted transfer was retried or misclassified: %v", err)
			}
			if service == "realdebrid" || service == "deepbrid" {
				var retained bool
				if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.aio_availability WHERE service=$1 AND cached)`, service).Scan(&retained); err != nil || retained {
					t.Fatal("deleted account item remained available", err)
				}
			}
			var leaked bool
			if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingest.operation_downloads WHERE input::text LIKE '%synthetic-key%') OR EXISTS(SELECT 1 FROM ingest.operation_events WHERE phase LIKE '%synthetic-key%')`).Scan(&leaked); err != nil || leaked {
				t.Fatal("credential entered download history", err)
			}
		})
	}
}

func TestExtendedPublicPlaybackNeverUsesAdministratorAnnouncesOrPersistsVisitorState(t *testing.T) {
	for _, service := range []string{"realdebrid", "premiumize", "debridlink", "deepbrid"} {
		t.Run(service, func(t *testing.T) {
			ctx, s, conn := testService(t)
			hash := strings.Repeat("a", 40)
			seedMovie(t, ctx, s, conn, "private-source", "one", hash)
			if err := s.Vault.Put(ctx, "admin-announce", "https://announce.example/administrator-secret/announce"); err != nil {
				t.Fatal(err)
			}
			c, _ := s.DB.AIOConfig(ctx)
			c.Trackers = map[string]model.AIOTracker{"private-source": {Private: true, AnnounceSecret: "admin-announce"}}
			if err := s.SaveConfig(ctx, c); err != nil {
				t.Fatal(err)
			}
			var ready, deleted atomic.Bool
			var adds atomic.Int32
			ready.Store(true)
			upstream := extendedPlaybackServer(t, service, hash, "visitor-key", &ready, &deleted, &adds)
			defer upstream.Close()
			s.Providers.RDBase, s.Providers.PMBase, s.Providers.DLBase, s.Providers.DBBase = upstream.URL, upstream.URL, upstream.URL, upstream.URL
			visitor := &PublicInstallation{Services: map[string]string{service: "visitor-key"}}
			if service == "deepbrid" {
				streams, err := s.PublicStreams(ctx, "https://public.example", "encrypted", "movie", "tt1234567", visitor)
				if err != nil || len(streams) != 0 || adds.Load() != 0 {
					t.Fatal("unknown private torrent was offered", err)
				}
				generation, _ := s.DB.AIOCacheGeneration(ctx)
				s.rememberVisitorPreparation(service, "visitor-key", hash, "17", []model.AIOFile{{Path: "Example.Movie.2021.mkv", Size: 100}}, generation)
			}
			streams, err := s.PublicStreams(ctx, "https://public.example", "encrypted", "movie", "tt1234567", visitor)
			if err != nil || len(streams) != 1 || streams[0].InfoHash != "" || len(streams[0].Sources) != 0 {
				t.Fatalf("private cached result unsafe or absent: %+v %v", streams, err)
			}
			link, err := s.PublicPlayback(ctx, service, "private-source", "one", "movie", "tt1234567", visitor)
			if err != nil || link != "https://cdn.example/ready.mkv" || adds.Load() != 0 {
				t.Fatalf("private cached playback started a new download: %s %v", link, err)
			}
			var retained int
			if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_preparations)+(SELECT count(*) FROM ingest.aio_availability)+(SELECT count(*) FROM ingest.operation_downloads)`).Scan(&retained); err != nil || retained != 0 {
				t.Fatal("visitor account state persisted", err)
			}
			ready.Store(false)
			if err := s.DB.ResetAIOCache(ctx); err != nil {
				t.Fatal(err)
			}
			empty, err := s.PublicStreams(ctx, "https://public.example", "encrypted", "movie", "tt1234567", visitor)
			if err != nil || len(empty) != 0 || adds.Load() != 0 {
				t.Fatal("uncached private torrent offered without a visitor announce", err)
			}
			if _, err := s.PublicPlayback(ctx, service, "private-source", "one", "movie", "tt1234567", visitor); !errors.Is(err, ErrPending) {
				t.Fatal("unverified private playback passed", err)
			}
			encoded, _ := json.Marshal(streams)
			if strings.Contains(string(encoded), "administrator-secret") || strings.Contains(string(encoded), "visitor-key") {
				t.Fatal("secret returned in stream metadata")
			}
		})
	}
}
