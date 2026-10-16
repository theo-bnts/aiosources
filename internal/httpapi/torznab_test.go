package httpapi

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/moodiness/ingest/torznab"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestTorznabAuthenticationCapabilitiesAndClientRoundTrip(t *testing.T) {
	f := sharingAPI(t, "https://owner.example.test")
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	if _, err = conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('fixture','one','{"title":"Example.Film.2024","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1024,"attributes":{"language":["French"],"unknown":"do-not-export"},"download_url":"https://private.invalid/do-not-export"}')`); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"External client","scope":"all","source_ids":[],"enabled":true}`
	if w := f.request("POST", "/api/torznab/endpoints", body, false, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := f.request("POST", "/api/torznab/endpoints", body, true, "")
	var grant model.ShareCredential
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &grant) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/torznab/" + grant.Share.ID + "/api"
	for _, query := range []string{"?t=caps", "?t=search&apikey=invalid"} {
		w = f.request("GET", path+query, "", true, "")
		if w.Code != 401 || !strings.Contains(w.Body.String(), `code="100"`) {
			t.Fatal("credentials bypassed", w.Code, w.Body.String())
		}
	}
	w = f.request("GET", path+"?t=caps&apikey="+grant.Password, "", false, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<tv-search available="yes"`) || strings.Contains(w.Body.String(), grant.Password) {
		t.Fatal("invalid capabilities", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("feed can be cached")
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	client, err := torznab.Open(f.ctx, torznab.Config{URL: server.URL + path, APIKey: grant.Password, DisableRetries: true})
	if err != nil {
		t.Fatal("SDK rejected endpoint capabilities", err)
	}
	page, err := client.Search(f.ctx, torznab.Query{Text: "Example Film", Categories: []int{8000}, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Total == nil || *page.Total != 1 {
		t.Fatal("SDK rejected RSS result", page, err)
	}
	if page.Items[0].InfoHash != strings.Repeat("a", 40) || page.Items[0].MagnetURL == "" || !strings.HasPrefix(page.Items[0].GUID, "urn:aiosources:") || strings.Contains(page.Items[0].Link, "private.invalid") {
		t.Fatal("SDK lost safe release metadata")
	}
	if language := page.Items[0].Attributes["language"]; len(language) != 1 || language[0] != "fr" {
		t.Fatal("normalized native audio language was lost")
	}
	if w = f.request("GET", "/api/shares", "", true, ""); strings.Contains(w.Body.String(), grant.Share.ID) {
		t.Fatal("Torznab key listed as catalog share")
	}
	if w = f.request("GET", "/api/catalogs/"+grant.Share.ID, "", false, grant.Password); w.Code != 401 {
		t.Fatal("protocol credential crossover", w.Code)
	}
	if w = f.request("POST", "/api/shares/"+grant.Share.ID+"/rotate", `{"revision":1}`, true, ""); w.Code != 404 {
		t.Fatal("protocol management crossover", w.Code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if w = f.request(method, path+"?t=search&apikey="+grant.Password, "", false, ""); w.Code != 405 {
			t.Fatal("write accepted", w.Code)
		}
	}
	for _, q := range []string{"t=get", "t=search&offset=-1", "t=search&limit=1&LIMIT=2", "t=search&cat=2000,,5000", "t=search&media=invalid", "t=search&sources=bad%2Fsource", "t=tvsearch&season=1&ep=2", "t=search&o=json", "t=search&apikey=other"} {
		w = f.request("GET", path+"?"+q+"&apikey="+grant.Password, "", false, "")
		if w.Code != 400 {
			t.Fatal("invalid query accepted", q, w.Code)
		}
	}
	if w = f.request("GET", path+"?t=search&cat=2000,5000&media=movie&sources=fixture&apikey="+grant.Password, "", false, ""); w.Code != 200 {
		t.Fatal("Prowlarr shaped request rejected", w.Code, w.Body.String())
	}
	if w = f.request("HEAD", path+"?t=search&apikey="+grant.Password, "", false, ""); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD body", w.Code, w.Body.String())
	}
}

func TestTorznabXMLProjectionEscapesTextAndOmitsNativeURLs(t *testing.T) {
	v := torznabResults(model.List[model.TorznabRecord]{Total: 1, Items: []model.TorznabRecord{{AIORecord: model.AIORecord{Provider: "alpha", Title: `Example & <Film> "Test"`, Hash: strings.Repeat("a", 40), IMDb: "tt1234567", Size: 100}, OccurrenceID: "stable", Category: 2000}}}, map[string]string{"alpha": "Alpha & source"}, "https://owner.example.test")
	data, err := xml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Items []struct {
			Title string `xml:"title"`
			Attrs []struct {
				XMLName xml.Name
				Name    string `xml:"name,attr"`
				Value   string `xml:"value,attr"`
			} `xml:",any"`
		} `xml:"channel>item"`
	}
	if xml.Unmarshal(data, &decoded) != nil || len(decoded.Items) != 1 || decoded.Items[0].Title != `Example & <Film> "Test"` {
		t.Fatal("XML escaping failed", string(data))
	}
	for _, needle := range []string{"announce", "apikey", "seeders", "peers", "source_id", "download_url"} {
		if strings.Contains(string(data), needle) {
			t.Fatal("private or obsolete data exported", needle)
		}
	}
	if !strings.Contains(string(data), "urn:btih:") || !strings.Contains(string(data), "isPermaLink=\"false\"") {
		t.Fatal("missing stable GUID or magnet")
	}
}

func TestTorznabHTTPSDisabledExpiryAndRollingLimits(t *testing.T) {
	f := sharingAPI(t, "https://owner.example.test")
	create := func(body string) model.ShareCredential {
		t.Helper()
		w := f.request("POST", "/api/torznab/endpoints", body, true, "")
		var v model.ShareCredential
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return v
	}
	for _, body := range []string{`{"name":"Disabled","scope":"all"}`, `{"name":"Expired","scope":"all","enabled":true,"expires_at":"2000-01-01T00:00:00Z"}`} {
		g := create(body)
		w := f.request("GET", "/torznab/"+g.Share.ID+"/api?t=caps&apikey="+g.Password, "", false, "")
		if w.Code != 401 {
			t.Fatal("disabled or expired access accepted", w.Code)
		}
	}
	g := create(`{"name":"Limited","scope":"all","enabled":true,"requests_per_minute":1,"media_types":["movie"]}`)
	path := "/torznab/" + g.Share.ID + "/api?t=caps&apikey=" + g.Password
	w := f.request("GET", path, "", false, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `id="5000"`) || strings.Contains(w.Body.String(), `<audio-search available="yes"`) {
		t.Fatal("capabilities exceeded grant", w.Body.String())
	}
	w = f.request("GET", path, "", false, "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), `code="500"`) {
		t.Fatal("rolling limit not enforced", w.Code)
	}
	local := sharingAPI(t, "")
	if w = local.request("POST", "/api/torznab/endpoints", `{"name":"Unsafe","scope":"all","enabled":true}`, true, ""); w.Code != 422 {
		t.Fatal("enabled without HTTPS public origin", w.Code)
	}
	if w = local.request("GET", "/torznab/anything/api?t=caps&apikey=anything", "", false, ""); w.Code != 503 {
		t.Fatal("feed ignored deployment boundary", w.Code)
	}
}

func TestTorznabEpisodePagesUsePacksAndCurrentManualNumbering(t *testing.T) {
	f := sharingAPI(t, "https://owner.example.test")
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	if _, err = conn.Exec(f.ctx, `INSERT INTO ingest.aio_works(media_type,tmdb_id,imdb_id,years) VALUES('tv','123','tt7654321',ARRAY[2024]);
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
('fixture','episode','{"title":"Example.Series.S01E02","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'),
('fixture','pack','{"title":"Example.Series.S01.COMPLETE","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}'),
('fixture','other','{"title":"Example.Series.S02E02","info_hash":"cccccccccccccccccccccccccccccccccccccccc"}'),
('fixture','unknown','{"title":"Example.Series.2024","info_hash":"dddddddddddddddddddddddddddddddddddddddd"}'),
('fixture','review','{"title":"Example.Series.S01E02","season":1,"episode":2,"info_hash":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}');
INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason) SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','fixture' FROM ingest.torrents t CROSS JOIN ingest.aio_works w;
DELETE FROM ingest.aio_match_queue;
INSERT INTO ingest.aio_match_reviews(provider_id,source_id,fingerprint,evidence_fingerprint,media_type,tmdb_id,override_episode,season,episode) SELECT provider_id,source_id,fields_fingerprint,md5(ingest.aio_review_evidence(fields)::text),'tv','123',true,3,NULL FROM ingest.torrents WHERE source_id='review';`); err != nil {
		t.Fatal(err)
	}
	g, err := f.db.CreateShare(f.ctx, model.ShareInput{Format: "torznab", Name: "Client", Scope: "all", Enabled: true, Fields: store.CatalogFields()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query        string
		total, count int
	}{{"&season=1&ep=2&limit=1", 2, 1}, {"&season=1&ep=2&limit=1&offset=1", 2, 1}, {"&season=1&ep=2&offset=2", 2, 0}, {"&season=3&ep=9", 1, 1}, {"&season=2", 1, 1}} {
		w := f.request("GET", "/torznab/"+g.Share.ID+"/api?t=tvsearch&imdbid=7654321&apikey="+g.Password+tc.query, "", false, "")
		var response struct {
			Response struct {
				Total int `xml:"total,attr"`
			} `xml:"channel>response"`
			Items []struct {
				Title string `xml:"title"`
			} `xml:"channel>item"`
		}
		if w.Code != 200 || xml.Unmarshal(w.Body.Bytes(), &response) != nil || response.Response.Total != tc.total || len(response.Items) != tc.count {
			t.Fatal("episode pagination or review failed", tc.query, w.Code, w.Body.String())
		}
	}
}
