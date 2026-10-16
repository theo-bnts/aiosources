package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestProviderFailuresReachPlaybackResponsesLogsAndHistoryWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		service, apiCode, category     string
		upstreamStatus, responseStatus int
	}{
		{"alldebrid", "MAGNET_TOO_MANY", "account_limit", 200, 409},
		{"alldebrid", "MAGNET_TOO_MANY_ACTIVE", "account_limit", 429, 409},
		{"alldebrid", "MAGNET_MUST_BE_PREMIUM", "subscription", 200, 403},
		{"alldebrid", "AUTH_BAD_APIKEY", "authentication", 401, 502},
		{"alldebrid", "MAGNET_INVALID_ID", "not_found", 200, 404},
		{"torbox", "ACTIVE_LIMIT", "account_limit", 400, 409},
		{"torbox", "COOLDOWN_LIMIT", "rate_limit", 429, 429},
		{"torbox", "DOWNLOAD_TOO_LARGE", "file_too_large", 400, 422},
		{"torbox", "DATABASE_ERROR", "provider_unavailable", 503, 503},
		{"torbox", "ITEM_NOT_FOUND", "not_found", 200, 404},
		{"torbox", "DOWNLOAD_NOT_CACHED", "not_cached", 400, 409},
	} {
		t.Run(tc.service+"/"+tc.apiCode, func(t *testing.T) {
			f, addon, conn := addonAPI(t)
			hash := strings.Repeat("a", 40)
			if err := f.db.SaveAIOWork(f.ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Movie"}, Years: []int{2021}}, []string{"example movie"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('source','one',jsonb_build_object('title','Example.Movie.2021','info_hash',$1::text,'imdb_id','tt1234567'))`, hash); err != nil {
				t.Fatal(err)
			}
			if err := addon.Match(f.ctx); err != nil {
				t.Fatal(err)
			}
			const private = "synthetic-private-provider-key"
			if err := addon.Vault.Put(f.ctx, "debrid", private); err != nil {
				t.Fatal(err)
			}
			c, _ := f.db.AIOConfig(f.ctx)
			c.AutoMatch = false
			c.Trackers["source"] = model.AIOTracker{Private: true}
			if tc.service == "alldebrid" {
				c.AllDebridSecret = "debrid"
			} else {
				c.TorBoxSecret = "debrid"
			}
			if err := addon.SaveConfig(f.ctx, c); err != nil {
				t.Fatal(err)
			}
			generation, _ := f.db.AIOCacheGeneration(f.ctx)
			if err := f.db.SaveSharedAvailability(f.ctx, tc.service, model.AIOAvailability{Generation: generation, Hash: hash, Cached: true, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Example.Movie.2021.mp4", Size: 100}}}); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v4/user":
					fmt.Fprint(w, `{"status":"success","data":{"user":{"isPremium":true}}}`)
				case "/user/me":
					fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
				case "/v4.1/magnet/status":
					fmt.Fprint(w, `{"status":"success","data":{"magnets":[]}}`)
				case "/v4/magnet/upload", "/torrents/createtorrent":
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(tc.upstreamStatus)
					if tc.service == "alldebrid" {
						if tc.upstreamStatus == 200 {
							fmt.Fprintf(w, `{"status":"success","data":{"magnets":[{"error":{"code":%q,"message":%q}}]}}`, tc.apiCode, private+" https://private.example/announce")
						} else {
							fmt.Fprintf(w, `{"status":"error","error":{"code":%q,"message":%q}}`, tc.apiCode, private+" https://private.example/announce")
						}
					} else {
						fmt.Fprintf(w, `{"success":false,"error":%q,"detail":%q,"data":null}`, tc.apiCode, private+" https://private.example/announce")
					}
				default:
					t.Error("provider rejection reached files, unlock or deletion")
					w.WriteHeader(500)
				}
			}))
			defer upstream.Close()
			addon.Providers.ADBase, addon.Providers.TBBase, addon.Providers.Client = upstream.URL, upstream.URL, upstream.Client()
			for _, origin := range []string{"private", "public"} {
				if err := f.db.SaveSharedAvailability(f.ctx, tc.service, model.AIOAvailability{Generation: generation, Hash: hash, Cached: true, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Example.Movie.2021.mp4", Size: 100}}}); err != nil {
					t.Fatal(err)
				}
				var streams []aiosource.Stream
				var err error
				if origin == "public" {
					access, _ := f.db.PublicAccess(f.ctx)
					access.Enabled = true
					if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
						t.Fatal(err)
					}
					token, e := addon.CreatePublicInstallation(f.ctx, "https://addon.example", map[string]string{tc.service: private}, nil)
					if e != nil {
						t.Fatal(e)
					}
					visitor, e := addon.OpenPublicInstallation(f.ctx, "https://addon.example", token)
					if e != nil {
						t.Fatal(e)
					}
					streams, err = addon.PublicStreams(f.ctx, "https://addon.example", token, "movie", "tt1234567", visitor)
				} else {
					token, _ := addon.Token(f.ctx)
					streams, err = addon.Streams(f.ctx, "https://addon.example", token, "movie", "tt1234567")
				}
				if err != nil || len(streams) != 1 {
					t.Fatal("verified cached stream absent", err)
				}
				response := f.request("GET", strings.TrimPrefix(streams[0].URL, "https://addon.example"), "", false, "")
				if response.Code != tc.responseStatus || !strings.Contains(response.Body.String(), model.ProviderFailureMessage(tc.category)) {
					t.Fatalf("provider error lost in %s playback: %d %s", origin, response.Code, response.Body.String())
				}
				var failure struct {
					Code string `json:"code"`
				}
				if json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != tc.category {
					t.Fatal("provider category missing from response", response.Body.String())
				}
				cached, e := f.db.SharedAvailability(f.ctx, tc.service, []string{hash}, time.Now().Add(-time.Minute))
				if e != nil || len(cached) != 1 || cached[0].Cached != (tc.category != "not_cached") {
					t.Fatal("account error invalidated shared cache, or cache miss retained stale positive evidence", cached, e)
				}
				if tc.category == "rate_limit" || tc.category == "provider_unavailable" {
					if response.Header().Get("Retry-After") != "120" {
						t.Fatal("retry header lost")
					}
				}
				if strings.Contains(response.Body.String(), private) || strings.Contains(response.Body.String(), "private.example") {
					t.Fatal("provider data disclosed in HTTP error")
				}
				rows, e := f.db.ListStremioRequestsFiltered(f.ctx, model.ListOptions{}, origin, "playback")
				if e != nil || rows.Total != 1 {
					t.Fatal("failed playback absent from history", e)
				}
				detail, e := f.db.StremioRequest(f.ctx, rows.Items[0].ID)
				if e != nil || len(detail.Calls) == 0 || detail.Calls[len(detail.Calls)-1].ErrorCode != tc.category || detail.Calls[len(detail.Calls)-1].HTTPStatus != tc.upstreamStatus {
					t.Fatal("actual provider failure absent from request diagnostics", e)
				}
				encoded, _ := json.Marshal(detail)
				if strings.Contains(string(encoded), private) || strings.Contains(string(encoded), "private.example") {
					t.Fatal("provider data retained in request history")
				}
			}
			logs, err := f.db.Logs(f.ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 100}, Service: tc.service})
			if err != nil {
				t.Fatal(err)
			}
			failures := 0
			for _, entry := range logs.Items {
				if entry.Kind != "debrid.playback_failed" {
					continue
				}
				failures++
				if entry.Data["failure_code"] != tc.category {
					t.Fatal("provider error not classified in Logs")
				}
			}
			if failures != 2 {
				t.Fatal("repeated public/private failures were hidden", failures)
			}
			if tc.category == "not_found" || tc.category == "not_cached" {
				var unavailable bool
				if err := conn.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM ingest.aio_incidents WHERE service=$1 AND active)`, tc.service).Scan(&unavailable); err != nil || unavailable {
					t.Fatal("a missing resource marked the whole provider unavailable", unavailable, err)
				}
			}
			if session := f.request("GET", "/api/session", "", true, ""); session.Code != 200 || !strings.Contains(session.Body.String(), `"authenticated":true`) {
				t.Fatal("provider failure revoked administrator session")
			}
		})
	}
}
