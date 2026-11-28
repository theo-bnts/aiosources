package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/notifications"
)

func insertTestWork(t testing.TB, f sharingAPIFixture, conn *pgx.Conn, mediaType, tmdbID, title string, year int) int64 {
	t.Helper()
	var workID int64
	if err := conn.QueryRow(f.ctx, `INSERT INTO aiosources.aio_works(media_type,tmdb_id,years,display_title,localized_titles,poster_path)
 VALUES($1,$2,$3,$4,jsonb_build_object('en',$4::text),'/poster.jpg') RETURNING id`,
		mediaType, tmdbID, []int{year}, title).Scan(&workID); err != nil {
		t.Fatal("insert work:", err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) VALUES($1,$2,$3)`,
		workID, title, model.NormalizeTitle(title)); err != nil {
		t.Fatal("insert title:", err)
	}
	return workID
}

func insertMatchedTorrent(t testing.TB, f sharingAPIFixture, conn *pgx.Conn, provider, sourceID, title string, size int64, workID int64, languages []string) {
	t.Helper()
	fields := map[string]any{
		"title":     title,
		"size":      size,
		"info_hash": strings.Repeat("a", 40-len(sourceID)) + sourceID,
	}
	if len(languages) > 0 {
		fields["languages"] = languages
	}
	raw, _ := json.Marshal(fields)
	var fp string
	if err := conn.QueryRow(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES($1,$2,$3::jsonb) RETURNING fields_fingerprint`,
		provider, sourceID, string(raw)).Scan(&fp); err != nil {
		t.Fatal("insert torrent:", err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason,normalized)
 VALUES($1,$2,$3,$4,1,'matched','verified',$5)`,
		provider, sourceID, fp, workID, model.NormalizeTitle(title)); err != nil {
		t.Fatal("insert match:", err)
	}
	if _, err := conn.Exec(f.ctx, `DELETE FROM aiosources.aio_match_queue WHERE provider_id=$1 AND source_id=$2`, provider, sourceID); err != nil {
		t.Fatal("clear match queue:", err)
	}
}

func TestNotificationChannelsSecretProtectionAndTestDelivery(t *testing.T) {
	f, addon, _ := addonAPI(t)

	var mu sync.Mutex
	var received []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	addon.NotificationOptions = notifications.Options{Client: srv.Client(), AllowAnyDiscord: true}

	// Store encrypted webhook secret.
	putSec := f.request("PUT", "/api/secrets/discord_hook_1", fmt.Sprintf(`{"value":%q,"revision":0}`, srv.URL+"/api/webhooks/1/tok"), true, "")
	if putSec.Code != 204 {
		t.Fatalf("put secret failed: %d %s", putSec.Code, putSec.Body.String())
	}

	// Create first channel — automatically becomes default.
	created := f.request("POST", "/api/notifications/channels", `{"name":"Discord Alerts","type":"discord","enabled":true,"allow_trusted":true,"secret_ref":"discord_hook_1","config":{"username":"MyAlerts"}}`, true, "")
	if created.Code != 201 {
		t.Fatalf("create channel failed: %d %s", created.Code, created.Body.String())
	}
	var ch model.NotificationChannel
	if err := json.Unmarshal(created.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	if !ch.IsDefault || ch.ID == "" || ch.Revision != 1 {
		t.Fatalf("unexpected channel: %+v", ch)
	}

	// Referenced secret cannot be deleted while channel exists.
	delSec := f.request("DELETE", "/api/secrets/discord_hook_1", `{"revision":1}`, true, "")
	if delSec.Code != 409 {
		t.Fatalf("expected 409 deleting referenced secret, got %d %s", delSec.Code, delSec.Body.String())
	}

	// Test delivery works.
	tested := f.request("POST", "/api/notifications/channels/"+ch.ID+"/test", `{}`, true, "")
	if tested.Code != 200 {
		t.Fatalf("test notification failed: %d %s", tested.Code, tested.Body.String())
	}
	mu.Lock()
	gotCount := len(received)
	mu.Unlock()
	if gotCount != 1 {
		t.Fatalf("expected 1 test delivery, got %d", gotCount)
	}

	// Delete channel, then deleting secret succeeds.
	delCh := f.request("DELETE", "/api/notifications/channels/"+ch.ID, `{"revision":1}`, true, "")
	if delCh.Code != 204 {
		t.Fatalf("delete channel failed: %d %s", delCh.Code, delCh.Body.String())
	}
	delSec2 := f.request("DELETE", "/api/secrets/discord_hook_1", `{"revision":1}`, true, "")
	if delSec2.Code != 204 {
		t.Fatalf("expected 204 deleting unreferenced secret, got %d %s", delSec2.Code, delSec2.Body.String())
	}
}

func TestReleaseAlertLifecycleFiltersRetryAndAutoDelete(t *testing.T) {
	f, addon, conn := addonAPI(t)

	var mu sync.Mutex
	var failNext bool
	var deliveries []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		shouldFail := failNext
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		deliveries = append(deliveries, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	addon.NotificationOptions = notifications.Options{Client: srv.Client(), AllowAnyDiscord: true}

	if res := f.request("PUT", "/api/secrets/discord_main", fmt.Sprintf(`{"value":%q,"revision":0}`, srv.URL+"/api/webhooks/10/secret-tok"), true, ""); res.Code != 204 {
		t.Fatalf("put secret: %d", res.Code)
	}
	chResp := f.request("POST", "/api/notifications/channels", `{"name":"Main Discord","type":"discord","enabled":true,"allow_trusted":true,"secret_ref":"discord_main"}`, true, "")
	if chResp.Code != 201 {
		t.Fatalf("create channel: %d %s", chResp.Code, chResp.Body.String())
	}
	var ch model.NotificationChannel
	_ = json.Unmarshal(chResp.Body.Bytes(), &ch)

	// Insert works: one movie with 0 torrents, one movie with a torrent, one TV series with S01E01 only.
	unreleasedMovie := insertTestWork(t, f, conn, "movie", "90001", "Future Odyssey", 2027)
	releasedMovie := insertTestWork(t, f, conn, "movie", "90002", "Existing Odyssey", 2025)
	insertMatchedTorrent(t, f, conn, "src-a", "1001", "Existing.Odyssey.2025.1080p", 2<<30, releasedMovie, []string{"fr"})
	tvWork := insertTestWork(t, f, conn, "tv", "90003", "Space Chronicle", 2026)
	insertMatchedTorrent(t, f, conn, "src-a", "1002", "Space.Chronicle.S01E01.1080p", 1<<30, tvWork, []string{"en"})

	// Default search returns only released works; include_unavailable=true returns unreleased works too.
	defSearch := f.request("GET", "/api/catalog/search?q=Odyssey", "", true, "")
	var defPage model.List[model.CatalogTitle]
	if defSearch.Code != 200 || json.Unmarshal(defSearch.Body.Bytes(), &defPage) != nil || len(defPage.Items) != 1 || defPage.Items[0].ID != releasedMovie {
		t.Fatalf("default search mismatch: %d %+v", defSearch.Code, defPage)
	}
	fullSearch := f.request("GET", "/api/catalog/search?q=Odyssey&include_unavailable=true", "", true, "")
	var fullPage model.List[model.CatalogTitle]
	if fullSearch.Code != 200 || json.Unmarshal(fullSearch.Body.Bytes(), &fullPage) != nil || len(fullPage.Items) != 2 {
		t.Fatalf("full search mismatch: %d %+v", fullSearch.Code, fullPage)
	}
	if fullPage.Items[0].ID != releasedMovie || fullPage.Items[1].ID != unreleasedMovie || fullPage.Items[1].Releases != 0 {
		t.Fatalf("expected released movie first then 0-release movie: %+v", fullPage.Items)
	}

	// Reject alert on a movie that already has a torrent.
	conflictMovie := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{}}`, releasedMovie, ch.ID), true, "")
	if conflictMovie.Code != 409 {
		t.Fatalf("expected 409 on already-available movie, got %d %s", conflictMovie.Code, conflictMovie.Body.String())
	}

	// Reject TV alert without season/episode, and reject S01E01 which already has a torrent.
	badTV := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{}}`, tvWork, ch.ID), true, "")
	if badTV.Code != 422 {
		t.Fatalf("expected 422 for TV alert without episode, got %d %s", badTV.Code, badTV.Body.String())
	}
	conflictEp := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"season":1,"episode":1,"channel_id":%q,"filters":{}}`, tvWork, ch.ID), true, "")
	if conflictEp.Code != 409 {
		t.Fatalf("expected 409 for already-available episode S01E01, got %d %s", conflictEp.Code, conflictEp.Body.String())
	}

	// Create alert on S01E02 (no release yet) and on unreleased movie with filters: min_size="1 GB", language FR + MULTI.
	epAlertResp := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"season":1,"episode":2,"channel_id":%q,"filters":{}}`, tvWork, ch.ID), true, "")
	if epAlertResp.Code != 201 {
		t.Fatalf("create S01E02 alert failed: %d %s", epAlertResp.Code, epAlertResp.Body.String())
	}
	movieAlertResp := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{"min_size":"1 GB","language":"fr","language_mode":"multi"}}`, unreleasedMovie, ch.ID), true, "")
	if movieAlertResp.Code != 201 {
		t.Fatalf("create movie alert failed: %d %s", movieAlertResp.Code, movieAlertResp.Body.String())
	}
	var movieAlert model.ReleaseAlert
	_ = json.Unmarshal(movieAlertResp.Body.Bytes(), &movieAlert)

	// Duplicate alert rejected.
	dupResp := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{}}`, unreleasedMovie, ch.ID), true, "")
	if dupResp.Code != 409 {
		t.Fatalf("expected 409 duplicate alert, got %d %s", dupResp.Code, dupResp.Body.String())
	}

	// 1) Non-matching torrents: 500 MB FR (too small) and 4 GB EN (wrong language) -> must NOT trigger.
	insertMatchedTorrent(t, f, conn, "src-a", "2001", "Future.Odyssey.2027.FRENCH.CAM", 500<<20, unreleasedMovie, []string{"fr"})
	insertMatchedTorrent(t, f, conn, "src-a", "2002", "Future.Odyssey.2027.1080p.ENG", 4<<30, unreleasedMovie, []string{"en"})
	if err := addon.ProcessReleaseAlerts(f.ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deliveries) != 0 {
		t.Fatalf("expected 0 deliveries for non-matching torrents, got %d", len(deliveries))
	}
	mu.Unlock()

	// 2) Matching 3 GB MULTI torrent arrives, but webhook temporarily returns 500 -> enters retrying without deleting alert.
	mu.Lock()
	failNext = true
	mu.Unlock()
	insertMatchedTorrent(t, f, conn, "src-a", "2003", "Future.Odyssey.2027.1080p.MULTI.WEB-DL", 3<<30, unreleasedMovie, []string{"multi"})
	if _, err := f.db.WakeReleaseAlertsForWorks(f.ctx, []int64{unreleasedMovie}); err != nil {
		t.Fatal(err)
	}
	if err := addon.ProcessReleaseAlerts(f.ctx); err != nil {
		t.Fatal(err)
	}
	retrying, err := f.db.ReleaseAlert(f.ctx, movieAlert.ID, "en")
	if err != nil {
		t.Fatalf("retrying alert was unexpectedly deleted: %v", err)
	}
	if retrying.Status != model.AlertStatusRetrying || retrying.Attempts != 1 || retrying.LastError == "" || strings.Contains(retrying.LastError, "secret-tok") {
		t.Fatalf("unexpected retrying state: %+v", retrying)
	}

	// 3) Webhook recovers -> delivery succeeds, rich embed sent, alert auto-deletes!
	mu.Lock()
	failNext = false
	mu.Unlock()
	if _, err := conn.Exec(f.ctx, `UPDATE aiosources.release_alerts SET next_attempt=now() WHERE id=$1`, movieAlert.ID); err != nil {
		t.Fatal(err)
	}
	if err := addon.ProcessReleaseAlerts(f.ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivered movie notification, got %d", len(deliveries))
	}
	rawMoviePayload, _ := json.Marshal(deliveries[0])
	mu.Unlock()
	if !strings.Contains(string(rawMoviePayload), "Future Odyssey") || !strings.Contains(string(rawMoviePayload), "https://addon.example/browse/search/"+strconv.FormatInt(unreleasedMovie, 10)) {
		t.Fatalf("unexpected discord payload: %s", string(rawMoviePayload))
	}
	if _, err := f.db.ReleaseAlert(f.ctx, movieAlert.ID, "en"); err == nil {
		t.Fatal("movie alert was not auto-deleted after delivery")
	}

	// 4) TV series: S01E03 arrives -> does NOT trigger S01E02 alert. Then S01E02 arrives -> triggers and auto-deletes!
	insertMatchedTorrent(t, f, conn, "src-a", "3001", "Space.Chronicle.S01E03.1080p", 2<<30, tvWork, []string{"en"})
	if _, err := f.db.WakeReleaseAlertsForWorks(f.ctx, []int64{tvWork}); err != nil {
		t.Fatal(err)
	}
	if err := addon.ProcessReleaseAlerts(f.ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deliveries) != 1 {
		t.Fatalf("S01E03 wrongly triggered S01E02 alert: deliveries=%d", len(deliveries))
	}
	mu.Unlock()

	insertMatchedTorrent(t, f, conn, "src-a", "3002", "Space.Chronicle.S01E02.1080p", 2<<30, tvWork, []string{"en"})
	if _, err := f.db.WakeReleaseAlertsForWorks(f.ctx, []int64{tvWork}); err != nil {
		t.Fatal(err)
	}
	if err := addon.ProcessReleaseAlerts(f.ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(deliveries) != 2 {
		t.Fatalf("expected S01E02 delivery, got %d", len(deliveries))
	}
	tvEmbeds, _ := deliveries[1]["embeds"].([]any)
	tvEmbed, _ := tvEmbeds[0].(map[string]any)
	mu.Unlock()
	if !strings.Contains(fmt.Sprint(tvEmbed["title"]), "S01E02") || !strings.Contains(fmt.Sprint(tvEmbed["url"]), "season=1&episode=2") {
		t.Fatalf("unexpected TV embed: %+v", tvEmbed)
	}
	remaining, err := f.db.ListReleaseAlerts(f.ctx, "", "", "en")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("expected 0 remaining alerts after auto-delete, got %+v err=%v", remaining, err)
	}
}

func TestTrustedUserAlertPermissionsChannelIsolationAndCleanup(t *testing.T) {
	f, addon, conn := addonAPI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	addon.NotificationOptions = notifications.Options{Client: srv.Client(), AllowAnyDiscord: true}

	if res := f.request("PUT", "/api/secrets/hook_shared", fmt.Sprintf(`{"value":%q,"revision":0}`, srv.URL+"/api/webhooks/1/shared"), true, ""); res.Code != 204 {
		t.Fatalf("put secret: %d", res.Code)
	}
	if res := f.request("PUT", "/api/secrets/hook_private", fmt.Sprintf(`{"value":%q,"revision":0}`, srv.URL+"/api/webhooks/2/private"), true, ""); res.Code != 204 {
		t.Fatalf("put secret: %d", res.Code)
	}
	sharedResp := f.request("POST", "/api/notifications/channels", `{"name":"Shared Discord","type":"discord","enabled":true,"allow_trusted":true,"secret_ref":"hook_shared"}`, true, "")
	privateResp := f.request("POST", "/api/notifications/channels", `{"name":"Admin Only","type":"discord","enabled":true,"allow_trusted":false,"secret_ref":"hook_private"}`, true, "")
	var sharedCh, privateCh model.NotificationChannel
	_ = json.Unmarshal(sharedResp.Body.Bytes(), &sharedCh)
	_ = json.Unmarshal(privateResp.Body.Bytes(), &privateCh)

	workID := insertTestWork(t, f, conn, "movie", "91001", "Upcoming Indie Film", 2028)

	// Create two trusted users: one with alerts_enabled=true, one with alerts_enabled=false.
	u1Resp := f.request("POST", "/api/trusted-users", `{"email":"alerts@example.invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true,"alerts_enabled":true}`, true, "")
	u2Resp := f.request("POST", "/api/trusted-users", `{"email":"noalerts@example.invalid","password":"synthetic trusted password","enabled":true,"search_enabled":true,"alerts_enabled":false}`, true, "")
	if u1Resp.Code != 201 || u2Resp.Code != 201 {
		t.Fatalf("create trusted users failed: %d %d", u1Resp.Code, u2Resp.Code)
	}
	var u1 model.TrustedUser
	_ = json.Unmarshal(u1Resp.Body.Bytes(), &u1)

	loginUser := func(email string) *http.Cookie {
		res := trustedRequest(f, nil, "POST", "/api/trusted/login", fmt.Sprintf(`{"email":%q,"password":"synthetic trusted password"}`, email))
		if res.Code != 200 {
			t.Fatalf("trusted login failed for %s: %d", email, res.Code)
		}
		for _, c := range res.Result().Cookies() {
			if c.Name == trustedSessionCookie {
				return c
			}
		}
		t.Fatal("missing trusted session cookie")
		return nil
	}
	c1 := loginUser("alerts@example.invalid")
	c2 := loginUser("noalerts@example.invalid")

	// User without alerts_enabled gets 403 on alert endpoints and cannot discover 0-torrent works via include_unavailable.
	if got := trustedRequest(f, c2, "GET", "/api/trusted/alerts", ""); got.Code != 403 {
		t.Fatalf("expected 403 for user without alerts_enabled, got %d", got.Code)
	}
	noUnavail := trustedRequest(f, c2, "GET", "/api/trusted/catalog/search?q=Upcoming+Indie&include_unavailable=true", "")
	var p2 model.List[model.CatalogTitle]
	if noUnavail.Code != 200 || json.Unmarshal(noUnavail.Body.Bytes(), &p2) != nil || len(p2.Items) != 0 {
		t.Fatalf("user without alerts_enabled saw unavailable work: %d %+v", noUnavail.Code, p2)
	}

	// User with alerts_enabled CAN discover 0-torrent works and sees ONLY allow_trusted channels without secrets.
	withUnavail := trustedRequest(f, c1, "GET", "/api/trusted/catalog/search?q=Upcoming+Indie&include_unavailable=true", "")
	var p1 model.List[model.CatalogTitle]
	if withUnavail.Code != 200 || json.Unmarshal(withUnavail.Body.Bytes(), &p1) != nil || len(p1.Items) != 1 || p1.Items[0].ID != workID {
		t.Fatalf("trusted user with alerts_enabled failed unavailable search: %d %+v", withUnavail.Code, p1)
	}
	chList := trustedRequest(f, c1, "GET", "/api/trusted/notifications/channels", "")
	if chList.Code != 200 || strings.Contains(chList.Body.String(), "hook_shared") || strings.Contains(chList.Body.String(), "Admin Only") || !strings.Contains(chList.Body.String(), "Shared Discord") {
		t.Fatalf("unexpected trusted channels response: %d %s", chList.Code, chList.Body.String())
	}

	// Trusted user cannot access admin notification endpoints.
	if got := trustedRequest(f, c1, "GET", "/api/notifications/channels", ""); got.Code != 401 {
		t.Fatalf("trusted user reached admin channel endpoint: %d", got.Code)
	}

	// Trusted user cannot use admin-only channel.
	if got := trustedRequest(f, c1, "POST", "/api/trusted/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{}}`, workID, privateCh.ID)); got.Code != 422 {
		t.Fatalf("trusted user used admin-only channel: %d %s", got.Code, got.Body.String())
	}

	// Admin creates an alert; trusted user also creates their own alert on the same work using shared channel.
	adminAlertResp := f.request("POST", "/api/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{}}`, workID, privateCh.ID), true, "")
	if adminAlertResp.Code != 201 {
		t.Fatalf("admin alert failed: %d %s", adminAlertResp.Code, adminAlertResp.Body.String())
	}
	var adminAlert model.ReleaseAlert
	_ = json.Unmarshal(adminAlertResp.Body.Bytes(), &adminAlert)

	trustedAlertResp := trustedRequest(f, c1, "POST", "/api/trusted/alerts", fmt.Sprintf(`{"work_id":%d,"channel_id":%q,"filters":{"min_size":"500 MB"}}`, workID, sharedCh.ID))
	if trustedAlertResp.Code != 201 {
		t.Fatalf("trusted alert failed: %d %s", trustedAlertResp.Code, trustedAlertResp.Body.String())
	}

	// Trusted user cannot see or delete admin's alert.
	if got := trustedRequest(f, c1, "DELETE", "/api/trusted/alerts/"+adminAlert.ID, `{"revision":1}`); got.Code != 404 {
		t.Fatalf("trusted user deleted admin alert: %d", got.Code)
	}
	tList := trustedRequest(f, c1, "GET", "/api/trusted/alerts", "")
	var tAlerts struct {
		Items []model.ReleaseAlert `json:"items"`
	}
	if tList.Code != 200 || json.Unmarshal(tList.Body.Bytes(), &tAlerts) != nil || len(tAlerts.Items) != 1 {
		t.Fatalf("trusted user should only see their own alert: %d %s", tList.Code, tList.Body.String())
	}

	// Deleting the trusted user cleans up their alerts while keeping admin's alert.
	if del := f.request("DELETE", "/api/trusted-users/"+u1.ID, fmt.Sprintf(`{"revision":%d}`, u1.Revision), true, ""); del.Code != 204 {
		t.Fatalf("delete trusted user failed: %d %s", del.Code, del.Body.String())
	}
	remaining, err := f.db.ListReleaseAlerts(f.ctx, "", "", "en")
	if err != nil || len(remaining) != 1 || remaining[0].ID != adminAlert.ID {
		t.Fatalf("expected only admin alert after trusted user deletion, got %+v err=%v", remaining, err)
	}
}
