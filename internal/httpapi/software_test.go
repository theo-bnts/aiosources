package httpapi

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSoftwareTorznabCapsCSPAndStremioExclusion(t *testing.T) {
	// Default caps include PC (4000) with both Games (4050) and Software subcategories (4010..4040, 4060..4070).
	caps := torznabCaps(model.Share{}, "https://addon.example")
	rawXML, err := xml.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	xmlText := string(rawXML)
	if strings.Count(xmlText, `id="4000"`) != 1 {
		t.Fatalf("expected exactly one 4000 PC category, got:\n%s", xmlText)
	}
	for _, want := range []string{`id="4010"`, `id="4020"`, `id="4030"`, `id="4040"`, `id="4050"`, `id="4060"`, `id="4070"`} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("missing %s in caps XML:\n%s", want, xmlText)
		}
	}

	// Software-only Torznab grant MUST exclude 4050 (PC-Games) and 1000 (Console).
	swOnly := torznabCaps(model.Share{MediaTypes: []string{"software"}}, "https://addon.example")
	swOnlyXML, _ := xml.Marshal(swOnly)
	if strings.Contains(string(swOnlyXML), `id="4050"`) || strings.Contains(string(swOnlyXML), `id="1000"`) {
		t.Fatalf("software-only caps leaked games category:\n%s", string(swOnlyXML))
	}
	for _, want := range []string{`id="4000"`, `id="4010"`, `id="4020"`, `id="4030"`, `id="4040"`, `id="4060"`, `id="4070"`} {
		if !strings.Contains(string(swOnlyXML), want) {
			t.Fatalf("software-only caps missing %s:\n%s", want, string(swOnlyXML))
		}
	}

	// CSP header allows jsDelivr selfh.st/icons remote PNGs.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://cdn.jsdelivr.net") {
		t.Fatalf("CSP missing https://cdn.jsdelivr.net: %s", csp)
	}
}

func TestSoftwareSearchTorznabIsolationAndNoSecretLeak(t *testing.T) {
	f, addon, conn := addonAPI(t)
	swKey := "winget:videolan.vlc"
	raw := `{"version":1,"id":"software-fixture","name":"Example software","adapter":"json","url":"https://source.example.invalid/api","enabled":false}`
	payload, _ := json.Marshal(map[string]string{"json": raw})
	if r := f.request("PUT", "/api/providers/software-fixture", string(payload), true, ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if err := addon.Vault.Put(f.ctx, "nvd_api_secret", "super-secret-nvd-key-value"); err != nil {
		t.Fatal(err)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.SoftwareNVDSecret = "nvd_api_secret"
	c.Trackers = map[string]model.AIOTracker{"software-fixture": {Private: true, AnnounceSecret: "sw_admin_announce"}}
	if err := addon.Vault.Put(f.ctx, "sw_admin_announce", "https://private.example.invalid/administrator-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}

	// Controls endpoint must never leak decrypted secret value.
	ctrl := f.request("GET", "/api/aiosource/controls", "", true, "")
	if ctrl.Code != 200 || strings.Contains(ctrl.Body.String(), "super-secret-nvd-key-value") {
		t.Fatalf("controls endpoint leaked secret or failed: code=%d", ctrl.Code)
	}

	// Seed software catalog + matching torrent + PC game (4050) using single-statement parameterized Execs.
	if _, err := conn.Exec(f.ctx, `UPDATE aiosources.sw_sync_state SET complete=TRUE,works=1 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.sw_catalog(software_key,name,normalized,vendor,vendor_norm,part,description,license,latest_version,platforms,nvd_cpe,winget_id,homebrew_cask,icon_ref,sw_generation)
 VALUES($1,'VLC media player','vlc media player','VideoLAN','videolan','a','Multimedia player','GPL-2.0','3.0.21','{windows,macos,linux}','cpe:2.3:a:videolan:vlc_media_player','VideoLAN.VLC','vlc','vlc',1)`, swKey); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.sw_external_ids(kind,external_id,external_lower,software_key) VALUES('winget_id','VideoLAN.VLC','videolan.vlc',$1)`, swKey); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_works(media_type,software_key,display_title,years,poster_path,software_normalized,software_data,sw_generation)
 VALUES('software',$1,'VLC media player','{}','vlc','vlc media player','{"vendor":"VideoLAN","description":"Multimedia player","license":"GPL-2.0","latest_version":"3.0.21","platforms":["windows","macos","linux"],"nvd_cpe":"cpe:2.3:a:videolan:vlc_media_player","winget_id":"VideoLAN.VLC","homebrew_cask":"vlc","icon_ref":"vlc","has_icon":true}',1)`, swKey); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,'VLC media player','vlc media player' FROM aiosources.aio_works WHERE software_key=$1`, swKey); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years) VALUES('game','999','Half-Life 2','{2004}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,'Half-Life 2','half life 2' FROM aiosources.aio_works WHERE igdb_id='999'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('software-fixture','sw-1','{"title":"VLC.Media.Player.3.0.21.Win64.Portable","info_hash":"cccccccccccccccccccccccccccccccccccccccc","categories":[4000,4010],"size":44000000}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('software-fixture','gm-1','{"title":"Half-Life 2","info_hash":"dddddddddddddddddddddddddddddddddddddddd","categories":[4050],"size":4000000000}')`); err != nil {
		t.Fatal(err)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}

	// Catalog Search returns software entry with jsDelivr selfh.st/icons PNG URL and external IDs.
	r := f.request("GET", "/api/catalog/search?q=VLC&type=software", "", true, "")
	var titles model.List[model.CatalogTitle]
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &titles) != nil || titles.Total != 1 || titles.Items[0].Type != "software" || titles.Items[0].SoftwareKey != swKey || titles.Items[0].Cover != "https://cdn.jsdelivr.net/gh/selfhst/icons@main/png/vlc.png" {
		t.Fatalf("software search failed: code=%d body=%s", r.Code, r.Body.String())
	}

	// Torznab software grant returns ONLY VLC (4010) and strictly excludes Half-Life 2 (4050).
	var credential model.ShareCredential
	r = f.request("POST", "/api/torznab/endpoints", `{"name":"Software client","scope":"all","enabled":true,"media_types":["software"]}`, true, "")
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &credential) != nil {
		t.Fatalf("software Torznab grant failed: %d %s", r.Code, r.Body.String())
	}
	q := url.Values{"t": {"search"}, "cat": {"4000"}, "apikey": {credential.Password}}
	r = f.request("GET", "/torznab/"+credential.Share.ID+"/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) || !strings.Contains(r.Body.String(), "VLC.Media.Player") || strings.Contains(r.Body.String(), "Half-Life 2") {
		t.Fatalf("software Torznab feed failed or leaked 4050 game: code=%d body=%s", r.Code, r.Body.String())
	}

	// Public Torznab accepts all 9 media types including software.
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled, access.TorznabEnabled = true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{MediaTypes: []string{"movie", "tv", "game", "software", "music", "book", "ebook", "audiobook", "other"}}, nil); err != nil {
		t.Fatalf("all 9 supported media types rejected: %v", err)
	}

	// Stremio must reject software streams.
	if _, err := addon.Streams(f.ctx, "https://addon.example", "", "software", "software:"+swKey); err == nil {
		t.Fatal("Stremio exposed software stream")
	}
}
