package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/aiosource"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicTorznabPrivacyURLScopesAndRevocation(t *testing.T) {
	f, addon, conn := addonAPI(t)
	for _, id := range []string{"open-library", "private-library", "other-private"} {
		definition := `{"version":1,"id":"` + id + `","name":"Display ` + id + `","adapter":"http_json","url":"https://source.example.invalid/api","enabled":false}`
		payload, _ := json.Marshal(map[string]string{"json": definition})
		if r := f.request("PUT", "/api/providers/"+id, string(payload), true, ""); r.Code != 200 {
			t.Fatal("fixture source", r.Code)
		}
	}
	c, _ := f.db.AIOConfig(f.ctx)
	c.Trackers = map[string]model.AIOTracker{"open-library": {Private: false}, "private-library": {Private: true, AnnounceSecret: "admin-announce"}, "other-private": {Private: true}}
	if err := addon.Vault.Put(f.ctx, "admin-announce", "https://admin.example.invalid/never-export-this-announcement"); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetAIOConfig(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(f.ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('open-library','open','{"title":"Example Public Film","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'),
 ('private-library','cached','{"title":"Example Cached Film","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}'),
 ('private-library','uncached','{"title":"Example Uncached Film","info_hash":"cccccccccccccccccccccccccccccccccccccccc"}'),
 ('other-private','other','{"title":"Example Other Film","info_hash":"dddddddddddddddddddddddddddddddddddddddd"}');
 INSERT INTO ingest.aio_shared_availability(service,hash,cached,checked_at,files) VALUES ('torbox','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',true,now(),'[]');`); err != nil {
		t.Fatal(err)
	}
	access, _ := f.db.PublicAccess(f.ctx)
	access.Enabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{}, nil); err == nil {
		t.Fatal("Torznab was implicitly enabled")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.TorznabEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	key, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	feed := func(k, q string) *httptest.ResponseRecorder {
		return f.request("GET", "/public/torznab/api?apikey="+url.QueryEscape(k)+"&t=search"+q, "", false, "")
	}
	response := feed(key, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `total="2"`) {
		t.Fatal("public visibility is incorrect", response.Code, response.Body.String())
	}
	for _, secret := range []string{"Example Uncached Film", "Example Other Film", "admin.example.invalid", "Display private-library", `name="indexer"`, "tr="} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("hidden source or uncached private data escaped", secret)
		}
	}
	response = feed(key, "&cached_only=true&services=torbox")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `total="1"`) {
		t.Fatal("known cache filter ignored", response.Code)
	}
	response = feed(key, "&cached_only=true&services=alldebrid")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `total="0"`) {
		t.Fatal("service caches contaminated")
	}
	if feed(key, "&sources=private-library").Code != 400 {
		t.Fatal("source filter accepted while sources hidden")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.ExposeSources = true
	if err := f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	own := "https://visitor.example.invalid/own-private-passkey/announce"
	ownKey, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{Sources: []string{"private-library"}}, map[string]string{"private-library": own})
	if err != nil {
		t.Fatal(err)
	}
	response = feed(ownKey, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `total="2"`) || !strings.Contains(response.Body.String(), "tr="+url.QueryEscape(own)) || !strings.Contains(response.Body.String(), "Display private-library") || strings.Contains(response.Body.String(), "admin.example.invalid") {
		t.Fatal("personal announce or source selection lost", response.Code)
	}
	for _, query := range []string{"&cached_only=true", "&cached_only=true&services=torbox"} {
		cached := feed(ownKey, query)
		if cached.Code != 200 || !strings.Contains(cached.Body.String(), `total="1"`) || strings.Contains(cached.Body.String(), "tr=") || strings.Contains(cached.Body.String(), "own-private-passkey") {
			t.Fatal("cached-only public Torznab included a personal announce", cached.Code)
		}
	}
	cachedKey, err := aiosource.WithPublicTorznabOptions(strings.Split(ownKey, "~")[0], model.PublicTorznabOptions{Sources: []string{"private-library"}, Services: []string{"torbox"}, CachedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if cached := feed(cachedKey, "&cached_only=false"); cached.Code != 200 || !strings.Contains(cached.Body.String(), `total="1"`) || strings.Contains(cached.Body.String(), "tr=") {
		t.Fatal("query widened the installation's cached-only option")
	}
	if _, err = conn.Exec(f.ctx, `UPDATE ingest.aio_shared_availability SET checked_at=now()-($1::int * interval '1 minute')-interval '1 minute'`, c.CacheMinutes); err != nil {
		t.Fatal(err)
	}
	if stale := feed(cachedKey, ""); stale.Code != 200 || !strings.Contains(stale.Body.String(), `total="0"`) || strings.Contains(stale.Body.String(), "tr=") {
		t.Fatal("expired cache fell back to an announce")
	}
	if _, err = conn.Exec(f.ctx, `UPDATE ingest.aio_shared_availability SET checked_at=now()`); err != nil {
		t.Fatal(err)
	}
	if feed(ownKey, "&sources=open-library").Code != 200 || !strings.Contains(feed(ownKey, "&sources=open-library").Body.String(), `total="0"`) {
		t.Fatal("URL filter widened its configuration")
	}
	base := strings.Split(ownKey, "~")[0]
	changed, e := aiosource.WithPublicTorznabOptions(base, model.PublicTorznabOptions{Sources: []string{"private-library"}, Services: []string{"torbox"}})
	if e != nil || !strings.Contains(feed(changed, "").Body.String(), "tr="+url.QueryEscape(own)) {
		t.Fatal("filter edits lost encrypted personal announces", e)
	}
	other, e := aiosource.WithPublicTorznabOptions(base, model.PublicTorznabOptions{Sources: []string{"other-private"}})
	if e != nil || !strings.Contains(feed(other, "").Body.String(), `total="0"`) {
		t.Fatal("changing filters attached an announce to another private source", e)
	}
	if _, err = addon.OpenPublicInstallation(f.ctx, "https://addon.example", ownKey); err == nil {
		t.Fatal("Torznab credential accepted as Stremio installation")
	}
	adminToken, _ := testPrivateInstallation(t, f.ctx, addon)
	if feed(adminToken, "").Code != 401 {
		t.Fatal("administrator token accepted as public Torznab")
	}
	if _, err = addon.OpenPublicTorznab(f.ctx, "https://other.example", ownKey); err == nil {
		t.Fatal("configuration crossed origins")
	}
	if r := f.request("GET", "/api/providers", "", false, ownKey); r.Code != 401 {
		t.Fatal("public key opened administration")
	}
	access, _ = f.db.PublicAccess(f.ctx)
	access.ExposeSources = false
	if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
		t.Fatal(err)
	}
	response = feed(ownKey, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `total="2"`) || strings.Contains(response.Body.String(), "tr=") || strings.Contains(response.Body.String(), `name="indexer"`) {
		t.Fatal("withdrawing visibility did not restore anonymous cached-only private access", response.Code)
	}
	for _, table := range []string{"settings", "shares", "share_requests", "activity_logs"} {
		var present bool
		if err = conn.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM ingest."+table+" record WHERE position($1 in to_jsonb(record)::text)>0)", "own-private-passkey").Scan(&present); err != nil || present {
			t.Fatal("visitor secret persisted", table, err)
		}
	}
	var grants, leases int
	if err = conn.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM ingest.shares),(SELECT count(*) FROM ingest.share_requests)`).Scan(&grants, &leases); err != nil || grants != 1 || leases != 0 {
		t.Fatal("public setup created visitor records", err)
	}
	generation := access.Generation
	for _, enabled := range []bool{false, true} {
		access, _ = f.db.PublicAccess(f.ctx)
		access.TorznabEnabled = enabled
		if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
			t.Fatal(err)
		}
		if feed(ownKey, "").Code != 401 {
			t.Fatal("old public URL revived")
		}
	}
	access, _ = f.db.PublicAccess(f.ctx)
	if access.Generation != generation {
		t.Fatal("Torznab toggle revoked Stremio installations")
	}
	fresh, err := addon.CreatePublicTorznab(f.ctx, "https://addon.example", model.PublicTorznabOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		access, _ = f.db.PublicAccess(f.ctx)
		access.Enabled = enabled
		if err = f.db.SetPublicAccess(f.ctx, access); err != nil {
			t.Fatal(err)
		}
		if feed(fresh, "").Code != 401 {
			t.Fatal("global public revocation did not invalidate Torznab configuration")
		}
	}
}

func TestPublicTorznabSetupOriginAndCredentialBoundaries(t *testing.T) {
	f, addon, _ := addonAPI(t)
	a, _ := f.db.PublicAccess(f.ctx)
	a.Enabled = true
	a.TorznabEnabled = true
	if err := f.db.SetPublicAccess(f.ctx, a); err != nil {
		t.Fatal(err)
	}
	setup := func(origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/public/torznab/install", strings.NewReader(body)).WithContext(f.ctx)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	if r := setup("https://unrelated.example", `{"options":{}}`); r.Code != http.StatusForbidden {
		t.Fatal("cross-origin configuration accepted", r.Code)
	}
	for _, body := range []string{`{"services":{"torbox":"must-not-accept-provider-credentials"}}`, `{"options":{"sources":["hidden-source"]}}`, `{"options":{"services":["unsupported"]}}`, `{"announces":{"hidden-source":"https://announce.example.invalid/own"}}`} {
		if r := setup("https://addon.example", body); r.Code != 400 {
			t.Fatal("invalid or hidden configuration accepted", r.Code)
		}
	}
	r := setup("https://addon.example", `{"options":{"services":["torbox"],"cached_only":true}}`)
	var output struct {
		URL string `json:"url"`
		Key string `json:"api_key"`
	}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &output) != nil || output.URL != "https://addon.example/public/torznab/api" {
		t.Fatal("invalid public setup", r.Code)
	}
	installation, err := addon.OpenPublicTorznab(f.ctx, "https://addon.example", output.Key)
	if err != nil || !installation.Options.CachedOnly {
		t.Fatal("URL lost its filters", err)
	}
	base := strings.Split(output.Key, "~")[0]
	for _, options := range []string{`{"announces":{"hidden-source":"https://announce.example.invalid/injected"}}`, `{"services":["direct"]}`, `{"media":["unknown"]}`, `{"cached_only":"true"}`, `{} {}`} {
		key := base + "~v1~" + base64.RawURLEncoding.EncodeToString([]byte(options))
		if _, err = addon.OpenPublicTorznab(f.ctx, "https://addon.example", key); err == nil {
			t.Fatal("malformed URL preferences accepted")
		}
	}
	for _, key := range []string{output.Key + "~extra", base + "~v2~e30", base + "~v1~!", strings.Repeat("a", aiosource.MaxPublicTorznabSize+1)} {
		if _, err = addon.OpenPublicTorznab(f.ctx, "https://addon.example", key); err == nil {
			t.Fatal("invalid or oversized public credential accepted")
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if r = f.request(method, "/public/torznab/api?t=caps&apikey="+output.Key, "", false, ""); r.Code != 405 {
			t.Fatal("public write route accepted", r.Code)
		}
	}
	if r = f.request("HEAD", "/public/torznab/api?t=caps&apikey="+output.Key, "", false, ""); r.Code != 200 || r.Body.Len() != 0 {
		t.Fatal("HEAD not supported", r.Code)
	}
}
