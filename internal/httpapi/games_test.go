package httpapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestGamesSearchAndTorznabRespectPublicVisibilityAndIdentifiers(t *testing.T) {
	f, addon, conn := addonAPI(t)
	raw := `{"version":1,"id":"game-fixture","name":"Example games","adapter":"json","url":"https://source.example.invalid/api","enabled":false}`
	payload, _ := json.Marshal(map[string]string{"json": raw})
	if r := f.request("PUT", "/api/providers/game-fixture", string(payload), true, ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.Trackers = map[string]model.AIOTracker{"game-fixture": {Private: true, AnnounceSecret: "game_admin_announce"}}
	if err := addon.Vault.Put(f.ctx, "game_admin_announce", "https://private.example.invalid/administrator-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	_, err := conn.Exec(f.ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,localized_titles,years,game_data) VALUES('game','101','Synthetic Odyssey','{"en":"Synthetic Odyssey","fr":"Odyssée synthétique"}','{2024}','{"platforms":[{"id":6,"name":"PC (Microsoft Windows)","slug":"win"}]}');
INSERT INTO aiosources.aio_titles(work_id,title,normalized) SELECT id,'Synthetic Odyssey','synthetic odyssey' FROM aiosources.aio_works WHERE igdb_id='101';
INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('game-fixture','one','{"title":"Synthetic.Odyssey.PC","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","categories":[4000,4050],"size":1024}');`)
	if err != nil {
		t.Fatal(err)
	}
	if r := f.request("POST", "/api/shares", `{"name":"Catalog recipient","scope":"all","enabled":true,"fields":["title","info_hash"],"catalog_entities":{"metadata":true}}`, true, ""); r.Code != 201 {
		t.Fatal("catalog sharing could not coexist with games", r.Code)
	}
	if err = addon.Match(f.ctx); err != nil {
		t.Fatal(err)
	}
	r := f.request("GET", "/api/catalog/search?q=Synthetic%20Odyssey&type=game&language=fr", "", true, "")
	var titles model.List[model.CatalogTitle]
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &titles) != nil || titles.Total != 1 || titles.Items[0].Type != "game" || titles.Items[0].TMDB != "" || titles.Items[0].IGDB != "101" || titles.Items[0].Title != "Odyssée synthétique" {
		t.Fatal("game search", r.Code)
	}
	var credential model.ShareCredential
	r = f.request("POST", "/api/torznab/endpoints", `{"name":"Game client","scope":"all","enabled":true,"media_types":["game"]}`, true, "")
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &credential) != nil {
		t.Fatal("game Torznab grant", r.Code)
	}
	q := url.Values{"t": {"search"}, "q": {"Synthetic Odyssey"}, "media": {"game"}, "cat": {"4050"}, "apikey": {credential.Password}}
	r = f.request("GET", "/torznab/"+credential.Share.ID+"/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) || !strings.Contains(r.Body.String(), "4050") {
		t.Fatal("game feed failed", r.Code)
	}
	q.Set("cat", "4000")
	r = f.request("GET", "/torznab/"+credential.Share.ID+"/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) || !strings.Contains(r.Body.String(), `name="category" value="4000"`) {
		t.Fatal("parent PC category omitted a game", r.Code)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled, access.TorznabEnabled = true, true
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{MediaTypes: []string{"movie", "tv", "book", "ebook", "audiobook", "game", "other"}}, nil); err != nil {
		t.Fatal("an explicit selection of all supported media was rejected", err)
	}
	key, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{MediaTypes: []string{"game"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	q.Set("apikey", key)
	r = f.request("GET", "/public/torznab/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="0"`) {
		t.Fatal("uncached private game exposed", r.Code)
	}
	if _, err = conn.Exec(f.ctx, `INSERT INTO aiosources.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',TRUE,now(),'[{"path":"Synthetic.Game.iso","size":1024}]')`); err != nil {
		t.Fatal(err)
	}
	r = f.request("GET", "/public/torznab/api?"+q.Encode(), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) {
		t.Fatal("cached public game unavailable", r.Code)
	}
	for _, private := range []string{"administrator-passkey", "private.example.invalid", "tr=", "tmdbid"} {
		if strings.Contains(r.Body.String(), private) {
			t.Fatal("game feed exposed private or misnamed data")
		}
	}
}
