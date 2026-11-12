package aiosource

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestVisitorCredentialPoliciesAreIndependentAndNeverEmbedInstanceSecrets(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	const adminKey = "synthetic-instance-service-key"
	const ownKey = "synthetic-personal-service-key"
	const adminAnnounce = "https://tracker.example.invalid/instance-passkey/announce"
	const ownAnnounce = "https://tracker.example.invalid/personal-passkey/announce"
	if err := s.Vault.Put(ctx, "instance_service", adminKey); err != nil {
		t.Fatal(err)
	}
	if err := s.Vault.Put(ctx, "admin_announce", adminAnnounce); err != nil {
		t.Fatal(err)
	}
	config, _ := s.DB.AIOConfig(ctx)
	config.TorBoxSecret = "instance_service"
	if err := s.DB.SetAIOConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/me" || r.Header.Get("Authorization") != "Bearer "+ownKey {
			t.Error("validation used an unselected account or operation")
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"id":42}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	s.Providers.Client = upstream.Client()
	for _, trusted := range []bool{false, true} {
		for _, instanceDebrid := range []bool{false, true} {
			for _, instanceAnnounce := range []bool{false, true} {
				t.Run(fmt.Sprintf("trusted=%t/debrid=%t/announces=%t", trusted, instanceDebrid, instanceAnnounce), func(t *testing.T) {
					access, _ := s.DB.PublicAccess(ctx)
					access.Enabled, access.StremioEnabled, access.TorznabEnabled, access.SearchEnabled = true, true, true, true
					access.CredentialPolicy = model.CredentialPolicy{UseInstanceDebrid: instanceDebrid, UseInstanceAnnounces: instanceAnnounce}
					if err := s.DB.SetPublicAccess(ctx, access); err != nil {
						t.Fatal(err)
					}
					access, _ = s.DB.PublicAccess(ctx)
					changes := PublicCredentialChanges{Services: map[string]string{}, Announces: map[string]string{}}
					if !instanceDebrid {
						changes.Services["torbox"] = ownKey
					}
					if !instanceAnnounce {
						changes.Announces["private-one"] = ownAnnounce
					}
					var personal, visitor *PublicInstallation
					var credential PublicBrowserCredential
					var token string
					var err error
					var grant model.TrustedGrant
					if trusted {
						user, e := s.DB.SaveTrustedUser(ctx, model.TrustedUser{Email: fmt.Sprintf("reader-%t-%t@example.invalid", instanceDebrid, instanceAnnounce), Enabled: true, TrustedPermissions: model.TrustedPermissions{CredentialPolicy: access.CredentialPolicy, SearchEnabled: true, StremioEnabled: true, TorznabEnabled: true}}, "opaque-hash")
						if e != nil {
							t.Fatal(e)
						}
						grant = model.TrustedGrant{UserID: user.ID, Generation: user.Generation}
						personal, err = s.ResolveTrustedCredential(ctx, grant, "https://addon.example", changes)
						if err == nil {
							err = s.ValidatePersonalServices(ctx, personal, true)
						}
						if err == nil {
							credential, err = s.TrustedBrowserCredential(ctx, grant, "https://addon.example", personal)
						}
						if err == nil {
							token, err = s.CreateTrustedConnectionWithCredentials(ctx, grant, "https://addon.example", "stremio", model.StreamPreferences{}, model.PublicTorznabOptions{}, model.PublicCatalogOptions{}, personal)
						}
						if err == nil {
							visitor, err = s.TrustedVisitorWithCredentials(ctx, grant, "https://addon.example", "stremio", model.StreamPreferences{}, personal.Services, personal.Announces)
						}
					} else {
						personal, err = s.ResolvePublicCredential(ctx, "https://addon.example", changes)
						if err == nil {
							credential, err = s.PublicBrowserCredential(ctx, "https://addon.example", personal)
						}
						if err == nil {
							token, err = s.CreatePublicInstallation(ctx, "https://addon.example", personal.Services, personal.Announces)
						}
						if err == nil {
							visitor, err = s.OpenPublicInstallation(ctx, "https://addon.example", token)
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					expectedKey, expectedAnnounce := ownKey, ownAnnounce
					if instanceDebrid {
						expectedKey = adminKey
					}
					if instanceAnnounce {
						expectedAnnounce = adminAnnounce
					}
					if visitor.Services["torbox"] != expectedKey {
						t.Fatal("wrong debrid credential source")
					}
					announces, e := s.visitorAnnounces(ctx, config, visitor)
					if e != nil || announces["private-one"] != expectedAnnounce {
						t.Fatal("wrong announce credential source", e)
					}
					clear(announces)
					purpose := publicInstallationPurpose
					if trusted {
						purpose = trustedConnectionPurpose
					}
					for _, sealedToken := range []string{token, credential.Credential} {
						data, e := base64.RawURLEncoding.DecodeString(sealedToken)
						if e != nil {
							t.Fatal(e)
						}
						plain, e := s.Vault.Open(purpose, data)
						if e != nil || strings.Contains(string(plain), adminKey) || strings.Contains(string(plain), adminAnnounce) {
							t.Fatal("instance secret embedded in visitor URL", e)
						}
						clear(plain)
					}
					if credential.InstanceServices != instanceDebrid {
						t.Fatal("browser credential lost source policy")
					}
					// Torznab's connection contains announces only; debrid keys never belong in it.
					var tz *PublicTorznabInstallation
					if trusted {
						key, e := s.CreateTrustedConnectionWithCredentials(ctx, grant, "https://addon.example", "torznab", model.StreamPreferences{}, model.PublicTorznabOptions{}, model.PublicCatalogOptions{}, personal)
						if e != nil {
							t.Fatal(e)
						}
						opened, e := s.OpenTrustedConnection(ctx, "https://addon.example", "torznab", key)
						if e != nil || len(opened.Services) != 0 {
							t.Fatal("Torznab retained a debrid key", e)
						}
						tz = &PublicTorznabInstallation{Trusted: &grant, Generation: grant.Generation, Announces: opened.Announces}
					} else {
						key, e := s.CreatePublicTorznab(ctx, "https://addon.example", model.PublicTorznabOptions{}, personal.Announces)
						if e != nil {
							t.Fatal(e)
						}
						tz, e = s.OpenPublicTorznab(ctx, "https://addon.example", key)
						if e != nil {
							t.Fatal(e)
						}
					}
					_, announces, e = s.PublicTorznabPolicy(ctx, tz)
					if e != nil || announces["private-one"] != expectedAnnounce {
						t.Fatal("Torznab lost selected announce", e)
					}
					clear(announces)
					if trusted {
						other, e := s.DB.SaveTrustedUser(ctx, model.TrustedUser{Email: fmt.Sprintf("other-%t-%t@example.invalid", instanceDebrid, instanceAnnounce), Enabled: true}, "opaque-hash")
						if e != nil {
							t.Fatal(e)
						}
						if _, e = s.ResolveTrustedCredential(ctx, model.TrustedGrant{UserID: other.ID, Generation: other.Generation}, "https://addon.example", PublicCredentialChanges{Credential: credential.Credential}); e == nil {
							t.Fatal("personal configuration crossed accounts")
						}
					} else {
						access.UseInstanceDebrid = !instanceDebrid
						if e = s.DB.SetPublicAccess(ctx, access); e != nil {
							t.Fatal(e)
						}
						if _, e = s.OpenPublicInstallation(ctx, "https://addon.example", token); e == nil {
							t.Fatal("credential policy change retained old authorization")
						}
						if e = s.ValidatePublicTorznab(ctx, tz); e == nil {
							t.Fatal("credential policy change retained old Torznab authorization")
						}
					}
				})
			}
		}
	}
	assertNoVisitorData(t, ctx, conn, ownKey, ownAnnounce, "personal-passkey")
}

func TestPublicInstanceCredentialLinkStaysValidWithNoConfiguredService(t *testing.T) {
	ctx, s, _ := testService(t)
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.StremioEnabled, access.UseInstanceDebrid = true, true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	token, err := s.CreatePublicInstallation(ctx, "https://addon.example", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.OpenPublicInstallation(ctx, "https://addon.example", token)
	if err != nil {
		t.Fatal(err)
	}
	streams, err := s.PublicStreams(ctx, "https://addon.example", token, "movie", "tt1234567", visitor)
	if err != nil || len(streams) != 0 {
		t.Fatal("valid empty instance connection was rejected", err)
	}
}
