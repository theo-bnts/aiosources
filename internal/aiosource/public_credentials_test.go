package aiosource

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestBrowserCredentialsShareOnlyPersonalAnnouncesAndServices(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.TorznabEnabled = true, true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	const origin = "https://public.example"
	const own = "https://announce.example.invalid/visitor-browser-passkey/announce"
	const replacement = "https://announce.example.invalid/visitor-new-passkey/announce"
	const key = "visitor-browser-service-key"
	// A new source defaults to private without a separate playback-settings save.
	data, err := s.ResolvePublicCredential(ctx, origin, PublicCredentialChanges{Announces: map[string]string{"unspecified": own}})
	if err != nil {
		t.Fatal(err)
	}
	torznab, err := s.CreatePublicTorznab(ctx, origin, model.PublicTorznabOptions{}, data.Announces)
	if err != nil {
		t.Fatal("Default-private source rejected its own announce", err)
	}
	browser, err := s.PublicBrowserCredential(ctx, origin, data)
	if err != nil || len(browser.Services) != 0 || len(browser.Announces) != 1 || strings.Contains(browser.Credential, "visitor") {
		t.Fatal("Announce-only browser configuration was not sealed", err)
	}
	if _, err := s.OpenPublicInstallation(ctx, origin, browser.Credential); !errors.Is(err, ErrAuth) {
		t.Fatal("An announce-only configuration authorized Stremio or Search", err)
	}
	if _, err := s.ResolvePublicCredential(ctx, origin, PublicCredentialChanges{Credential: torznab}); !errors.Is(err, ErrAuth) {
		t.Fatal("A Torznab API key crossed credential purposes", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key || r.URL.Path != "/user/me" {
			t.Error("Unexpected account or endpoint")
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	merged, err := s.ResolvePublicCredential(ctx, origin, PublicCredentialChanges{Credential: browser.Credential, Services: map[string]string{"torbox": key}})
	if err != nil || merged.Announces["unspecified"] != own {
		t.Fatal("Adding a service lost the remembered announce", err)
	}
	installation, err := s.CreatePublicInstallation(ctx, origin, merged.Services, merged.Announces)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.OpenPublicInstallation(ctx, origin, installation)
	if err != nil || visitor.Services["torbox"] != key || visitor.Announces["unspecified"] != own {
		t.Fatal("Shared personal configuration did not reach Stremio", err)
	}
	browser, err = s.PublicBrowserCredential(ctx, origin, visitor)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.ResolvePublicCredential(ctx, origin, PublicCredentialChanges{Credential: browser.Credential, RemoveAnnounces: []string{"unspecified"}, Announces: map[string]string{"private-one": replacement}})
	if err != nil || len(changed.Announces) != 1 || changed.Announces["private-one"] != replacement || changed.Services["torbox"] != key {
		t.Fatal("Announce removal or replacement affected the service key", err)
	}
	// The Torznab connection contains no debrid service keys, even when its
	// announces came from the common browser configuration.
	torznab, err = s.CreatePublicTorznab(ctx, origin, model.PublicTorznabOptions{}, merged.Announces)
	feed, openErr := s.OpenPublicTorznab(ctx, origin, torznab)
	if err != nil || openErr != nil || feed.Announces["unspecified"] != own {
		t.Fatal("Shared announce was not usable by Torznab", err, openErr)
	}
	if _, err := s.OpenPublicInstallation(ctx, origin, torznab); !errors.Is(err, ErrAuth) {
		t.Fatal("Torznab authorized a debrid connection")
	}
	for _, input := range []PublicCredentialChanges{{Credential: browser.Credential + "x"}, {Credential: browser.Credential, RemoveServices: []string{"unknown"}}, {Credential: browser.Credential, RemoveAnnounces: []string{"../source"}}} {
		if _, err := s.ResolvePublicCredential(ctx, origin, input); err == nil {
			t.Fatal("Invalid remembered configuration accepted")
		}
	}
	if _, err := s.ResolvePublicCredential(ctx, "https://another.example", PublicCredentialChanges{Credential: browser.Credential}); !errors.Is(err, ErrAuth) {
		t.Fatal("Remembered personal configuration crossed origins", err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.ExposeSources = false
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	retired, err := s.ResolvePublicCredential(ctx, origin, PublicCredentialChanges{Credential: browser.Credential})
	if err != nil || len(retired.Announces) != 0 || retired.Services["torbox"] != key {
		t.Fatal("Withdrawn permission retained an announce or removed the service key", err)
	}
	if _, err := s.CreatePublicInstallation(ctx, origin, retired.Services, retired.Announces); err != nil {
		t.Fatal("Withdrawal prevented cached-only setup", err)
	}
	assertNoVisitorData(t, ctx, conn, own, replacement, key, browser.Credential, installation, torznab)
}
