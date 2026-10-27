package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moodiness/ingest/torznab"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestBooksTorznabUsesStandardSearchAndNeverLeaksAdministratorCredentials(t *testing.T) {
	f, addon, conn := addonAPI(t)
	raw := `{"version":1,"id":"book-fixture","name":"Example books","adapter":"json","url":"https://source.example.invalid/api","enabled":false}`
	payload, _ := json.Marshal(map[string]string{"json": raw})
	if r := f.request("PUT", "/api/providers/book-fixture", string(payload), true, ""); r.Code != 200 {
		t.Fatal("source fixture", r.Code)
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.Trackers = map[string]model.AIOTracker{"book-fixture": {Private: true, AnnounceSecret: "admin_book_announce"}}
	if err := addon.Vault.Put(f.ctx, "admin_book_announce", "https://private.example.invalid/administrator-passkey/announce"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.ol_authors(id,name,normalized) VALUES('OL200A','Morgan Vale','morgan vale');
 INSERT INTO ingest.aio_works(media_type,tmdb_id,openlibrary_id,years,display_title,book_normalized,book_data) VALUES('book',NULL,'OL100W',ARRAY[2024],'The Amber Orchard','the amber orchard','{"author_keys":["OL200A"]}');
 INSERT INTO ingest.ol_editions(id,work_keys,title,normalized,isbn,year) VALUES('OL300M',ARRAY['OL100W'],'The Amber Orchard','the amber orchard',ARRAY['9780306406157'],2025);
 INSERT INTO ingest.ol_editions(id,work_keys,title,normalized) VALUES('OL301M',ARRAY['OL100W'],'Le Verger Ambré','le verger ambre');
 INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('book-fixture','one','{"title":"Morgan Vale - The Amber Orchard EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","isbn":"9780306406157","size":1024}');
 INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','verified ISBN' FROM ingest.torrents t CROSS JOIN ingest.aio_works w;
 DELETE FROM ingest.aio_match_queue;
 INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',true,now(),'[{"path":"The Amber Orchard.epub","size":1024}]');`); err != nil {
		t.Fatal(err)
	}
	var credential model.ShareCredential
	response := f.request("POST", "/api/torznab/endpoints", `{"name":"Book client","scope":"all","enabled":true,"media_types":["ebook"]}`, true, "")
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &credential) != nil {
		t.Fatal("book access rejected", response.Code)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	client, err := torznab.Open(f.ctx, torznab.Config{URL: server.URL + "/torznab/" + credential.Share.ID + "/api", APIKey: credential.Password, DisableRetries: true})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Capabilities().Searches[torznab.ModeBook].Available {
		t.Fatal("book search not advertised")
	}
	page, err := client.Search(f.ctx, torznab.Query{Mode: torznab.ModeBook, Categories: []int{7000}, Params: url.Values{"title": {"The Amber Orchard"}, "author": {"Morgan Vale"}, "isbn": {"0306406152"}, "year": {"2025"}}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("SDK book search failed: %+v %v", page, err)
	}
	item := page.Items[0]
	if len(item.Attributes["author"]) != 1 || item.Attributes["author"][0] != "Morgan Vale" || item.Attributes["isbn"][0] != "9780306406157" {
		t.Fatal("standard book attributes missing")
	}
	page, err = client.Search(f.ctx, torznab.Query{Mode: torznab.ModeBook, Params: url.Values{"title": {"Le.Verger.Ambre"}}})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("translated edition title could not find the source release", err)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled = true
	access.TorznabEnabled = true
	access.ExposeSources = false
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	key, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{MediaTypes: []string{"book"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := f.request("GET", "/public/torznab/api?t=book&title=Orchard&cat=7000&apikey="+url.QueryEscape(key), "", false, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), `total="1"`) {
		t.Fatal("public book search failed", r.Code)
	}
	for _, private := range []string{"administrator-passkey", "private.example.invalid", "tr=", "Example books", "book-fixture"} {
		if strings.Contains(r.Body.String(), private) {
			t.Fatal("public book feed exposed private data")
		}
	}
	for _, query := range []string{"t=book&isbn=9780306406158", "t=search&author=Morgan", "t=book&season=1"} {
		if r := f.request("GET", "/public/torznab/api?"+query+"&apikey="+url.QueryEscape(key), "", false, ""); r.Code != 400 {
			t.Fatal("invalid book request accepted", r.Code)
		}
	}
}
