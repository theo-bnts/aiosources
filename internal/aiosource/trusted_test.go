package aiosource

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTrustedPlaybackUsesCurrentInstanceCredentialsWithoutEmbeddingThem(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const key = "synthetic-instance-torbox-key"
	const announce = "https://tracker.example.invalid/announce?passkey=synthetic-owner-passkey"
	seedMovie(t, ctx, s, conn, "private-one", "one", hash)
	if err := s.Vault.Put(ctx, "trusted_test_debrid", key); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "admin_announce", announce); err != nil {
		t.Fatal(err)
	}
	config, _ := s.DB.AIOConfig(ctx)
	config.TorBoxSecret = "trusted_test_debrid"
	if err := s.DB.SetAIOConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	user, err := s.DB.SaveTrustedUser(ctx, model.TrustedUser{Email: "reader@example.invalid", Enabled: true, TrustedPermissions: model.TrustedPermissions{CredentialPolicy: model.CredentialPolicy{UseInstanceDebrid: true, UseInstanceAnnounces: true}, SearchEnabled: true, StremioEnabled: true, TorznabEnabled: true}}, "synthetic-opaque-password-hash")
	if err != nil {
		t.Fatal(err)
	}
	grant := model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
	var checks, adds atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Error("trusted request used another account")
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/torrents/checkcached":
			checks.Add(1)
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		case "/torrents/createtorrent":
			_ = r.ParseMultipartForm(1 << 20)
			magnet, e := url.Parse(r.FormValue("magnet"))
			if e != nil || magnet.Query().Get("tr") != announce || r.FormValue("add_only_if_cached") == "true" {
				t.Error("trusted uncached playback did not use its authorized instance announce")
			}
			adds.Add(1)
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":true,"download_present":true,"files":[{"id":3,"name":"Example.Movie.2021.mp4","size":100}]}}`, hash)
		case "/torrents/requestdl":
			fmt.Fprint(w, `{"success":true,"data":"https://files.example/trusted-download"}`)
		default:
			t.Error("unexpected trusted provider request", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer upstream.Close()
	s.Providers.TBBase, s.Providers.Client = upstream.URL, upstream.Client()
	token, err := s.CreateTrustedConnection(ctx, grant, "https://addon.example", "stremio", model.StreamPreferences{}, model.PublicTorznabOptions{}, model.PublicCatalogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.RawURLEncoding.DecodeString(token)
	plain, err := s.Vault.Open(trustedConnectionPurpose, sealed)
	if err != nil || strings.Contains(string(plain), key) || strings.Contains(string(plain), announce) || strings.Contains(string(plain), user.Email) {
		t.Fatal("trusted URL contains private account data", err)
	}
	clear(plain)
	if _, err = s.OpenTrustedConnection(ctx, "https://another.example", "stremio", token); err == nil {
		t.Fatal("trusted token accepted another audience")
	}
	if _, err = s.OpenTrustedConnection(ctx, "https://addon.example", "torznab", token); err == nil {
		t.Fatal("trusted token accepted another feature")
	}
	visitor, err := s.TrustedVisitor(ctx, grant, "https://addon.example", "stremio", model.StreamPreferences{})
	if err != nil {
		t.Fatal(err)
	}
	defer clear(visitor.Services)
	streams, err := s.PublicStreams(ctx, "https://addon.example", token, "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 1 || checks.Load() != 1 {
		t.Fatal("trusted Stremio did not check the instance account", err, len(streams), checks.Load())
	}
	data, _ := json.Marshal(streams)
	if strings.Contains(string(data), key) || strings.Contains(string(data), announce) || !strings.Contains(string(data), "Example Library") || !strings.Contains(streams[0].URL, "/trusted/stremio/") {
		t.Fatal("trusted presentation exposed credentials or omitted its source name")
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(streams[0].URL, "https://addon.example"), "/"), "/")
	if len(parts) != 8 {
		t.Fatal("unexpected trusted playback route")
	}
	link, err := s.PublicPlaybackSource(ctx, parts[5], "torbox", "movie", "tt1234567", visitor)
	if err != nil || link != "https://files.example/trusted-download" || adds.Load() != 1 {
		t.Fatal("trusted playback failed", err, adds.Load())
	}
	installation := &PublicTorznabInstallation{Trusted: &grant, Generation: grant.Generation}
	_, announces, err := s.PublicTorznabPolicy(ctx, installation)
	if err != nil || announces["private-one"] != announce {
		t.Fatal("trusted Torznab lost authorized private access", err)
	}
	clear(announces)
	user.Enabled = false
	if _, err = s.DB.SaveTrustedUser(ctx, user, ""); err != nil {
		t.Fatal(err)
	}
	if s.CheckPublicStremio(ctx, visitor) == nil || s.ValidatePublicTorznab(ctx, installation) == nil {
		t.Fatal("disabled account retained permission")
	}
}
