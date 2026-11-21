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

func TestMusicTorznabQueryParsingCapsAndCSP(t *testing.T) {
	rgID := "11111111-2222-3333-4444-555555555555"
	q, _, err := torznabQuery("t=music&artist=Synthetic+Duo&album=Neon+Horizon&label=Orbit&track=Opening&mbid=" + rgID + "&cat=3040")
	if err != nil || q.Mode != "music" || q.Artist != "Synthetic Duo" || q.Album != "Neon Horizon" || q.Label != "Orbit" || q.Track != "Opening" || q.MBID != rgID {
		t.Fatalf("t=music parsing failed: %+v %v", q, err)
	}
	// Music parameters are rejected on non-music modes.
	if _, _, err = torznabQuery("t=movie&artist=Synthetic+Duo"); err == nil {
		t.Fatal("artist parameter accepted on movie search")
	}
	if _, _, err = torznabQuery("t=music&mbid=not-a-uuid"); err == nil {
		t.Fatal("invalid MBID accepted on music search")
	}

	// Verify caps XML merges Music (3010/3040/3050) and Audiobook (3030) under a single 3000 Audio category.
	caps := torznabCaps(model.Share{}, "https://addon.example")
	rawXML, err := xml.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	xmlText := string(rawXML)
	if strings.Count(xmlText, `id="3000"`) != 1 {
		t.Fatalf("expected exactly one 3000 Audio category, got:\n%s", xmlText)
	}
	for _, want := range []string{`<audio-search available="yes"`, `id="3010"`, `id="3030"`, `id="3040"`, `id="3050"`} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("missing %s in caps XML:\n%s", want, xmlText)
		}
	}

	// Music-only grant excludes 3030 Audiobook and advertises audio-search=yes, book-search=no.
	musicOnly := torznabCaps(model.Share{MediaTypes: []string{"music"}}, "https://addon.example")
	musicOnlyXML, _ := xml.Marshal(musicOnly)
	if !strings.Contains(string(musicOnlyXML), `<audio-search available="yes"`) ||
		!strings.Contains(string(musicOnlyXML), `<book-search available="no"`) ||
		strings.Contains(string(musicOnlyXML), `id="3030"`) {
		t.Fatalf("music-only caps leaked audiobook or wrong modes:\n%s", string(musicOnlyXML))
	}

	// CSP header allows Cover Art Archive & Archive.org remote images.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
	csp := rec.Header().Get("Content-Security-Policy")
	for _, host := range []string{"https://coverartarchive.org", "https://archive.org", "https://*.archive.org"} {
		if !strings.Contains(csp, host) {
			t.Fatalf("CSP missing %s: %s", host, csp)
		}
	}
}

func TestMusicSearchAndTorznabRespectPublicVisibilityAndIdentifiers(t *testing.T) {
	f, addon, conn := addonAPI(t)
	rgID := "11111111-2222-3333-4444-555555555555"
	raw := `{"version":1,"id":"music-fixture","name":"Example music","adapter":"json","url":"https://source.example.invalid/api","enabled":false}`
	payload, _ := json.Marshal(map[string]string{"json": raw})
	if r := f.request("PUT", "/api/providers/music-fixture", string(payload), true, ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.Trackers = map[string]model.AIOTracker{"music-fixture": {Private: true, AnnounceSecret: "music_admin_announce"}}
	if err := addon.Vault.Put(f.ctx, "music_admin_announce", "https://private.example.invalid/administrator-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `UPDATE aiosources.mb_state SET complete=TRUE,works=1 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_works(media_type,musicbrainz_id,display_title,poster_path,years,music_normalized,music_data)
 VALUES('music',$1,'Neon Horizon',$1,'{2024}','neon horizon','{"primary_type":"Album","artists":["Synthetic Duo"],"labels":["Orbit Records"],"catalog_numbers":["ORB-042"],"barcodes":["0724349691728"],"discogs_ids":["master:26647"],"wikidata_id":"Q43228","has_cover":true}')`, rgID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,'Neon Horizon','neon horizon' FROM aiosources.aio_works WHERE musicbrainz_id=$1`, rgID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('music-fixture','one','{"title":"Synthetic Duo - Neon Horizon (2024) [FLAC]","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","categories":[3000,3040],"size":524288000}')`); err != nil {
		t.Fatal(err)
	}
	if r := f.request("POST", "/api/shares", `{"name":"Catalog recipient","scope":"all","enabled":true,"fields":["title","info_hash"],"catalog_entities":{"metadata":true}}`, true, ""); r.Code != 201 {
		t.Fatal("catalog sharing could not coexist with music", r.Code)
	}
	if err := addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.request("GET", "/api/catalog/search?q=Neon%20Horizon&type=music", "", true, "")
	var titles model.List[model.CatalogTitle]
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &titles) != nil || titles.Total != 1 || titles.Items[0].Type != "music" || titles.Items[0].MusicBrainz != rgID || titles.Items[0].Cover != "https://coverartarchive.org/release-group/"+rgID+"/front-500" {
		t.Fatalf("music search failed: code=%d body=%s", r.Code, r.Body.String())
	}

	var credential model.ShareCredential
	r = f.request("POST", "/api/torznab/endpoints", `{"name":"Lidarr client","scope":"all","enabled":true,"media_types":["music"]}`, true, "")
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &credential) != nil {
		t.Fatal("music Torznab grant", r.Code)
	}
	q := url.Values{"t": {"music"}, "artist": {"Synthetic Duo"}, "album": {"Neon Horizon"}, "cat": {"3040"}, "apikey": {credential.Password}}
	r = f.request("GET", "/torznab/"+credential.Share.ID+"/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) || !strings.Contains(r.Body.String(), "3040") || !strings.Contains(r.Body.String(), rgID) {
		t.Fatalf("music Torznab feed failed: code=%d body=%s", r.Code, r.Body.String())
	}
	for _, private := range []string{"administrator-passkey", "private.example.invalid", "tr=", "tmdbid"} {
		if strings.Contains(r.Body.String(), private) {
			t.Fatal("music feed exposed private or misnamed data")
		}
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled, access.TorznabEnabled = true, true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{MediaTypes: []string{"movie", "tv", "game", "music", "book", "ebook", "audiobook", "other"}}, nil); err != nil {
		t.Fatalf("an explicit selection of all supported media types was rejected: %v", err)
	}
}
