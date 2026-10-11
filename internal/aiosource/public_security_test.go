package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAllDebridDemoResponsesCannotValidateAServiceAccount(t *testing.T) {
	for _, flag := range []string{`true`, `"true"`} {
		t.Run(flag, func(t *testing.T) {
			p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"status":"success","demo":%s,"data":{"user":{"isPremium":true}}}`, flag)
			})
			if err := p.ValidateKey(context.Background(), "alldebrid", "synthetic-key"); !errors.Is(err, ErrAuth) {
				t.Fatalf("demo response authenticated an account: %v", err)
			}
		})
	}
	for _, flag := range []string{`false`, `"false"`, `null`} {
		t.Run("real_"+flag, func(t *testing.T) {
			p := providerTest(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"status":"success","demo":%s,"data":{"user":{"isPremium":true}}}`, flag)
			})
			if err := p.ValidateKey(context.Background(), "alldebrid", "synthetic-key"); err != nil {
				t.Fatalf("real account was rejected: %v", err)
			}
		})
	}
}

func TestPublicInstallationRejectsDemoKeysAndPreviouslyIssuedDemoLinks(t *testing.T) {
	ctx, s, _ := testService(t)
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	access.Enabled = true
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"success","demo":"true","data":{"user":{"isPremium":true}}}`)
	}))
	defer upstream.Close()
	s.Providers.ADBase = upstream.URL
	for _, key := range []string{"staticDemoApikeyPrem", "staticDemoApikeyTria", "staticDemoApikeyFree"} {
		t.Run(key, func(t *testing.T) {
			keys := map[string]string{"alldebrid": key}
			if token, err := s.CreatePublicInstallation(ctx, "https://public.example", keys); token != "" || (!errors.Is(err, ErrAuth) && !errors.Is(err, model.ErrInvalid)) {
				t.Errorf("demonstration key created an installation: %v", err)
			}
			// Reproduce a link issued by the old validator. The guard must also
			// revoke that stateless link without rotating legitimate installations.
			data, _ := json.Marshal(PublicInstallation{Version: 1, Generation: access.Generation, Audience: "https://public.example", Services: keys})
			sealed, err := s.Vault.Seal(publicInstallationPurpose, data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.OpenPublicInstallation(ctx, "https://public.example", base64.RawURLEncoding.EncodeToString(sealed)); !errors.Is(err, ErrAuth) {
				t.Fatalf("previously issued demonstration link remains valid: %v", err)
			}
		})
	}
}

func TestPublicKeyValidationNeverFollowsProviderRedirects(t *testing.T) {
	var leaked atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leaked.Add(1)
		fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
	}))
	defer sink.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	p := NewProviderHTTP()
	p.ADBase, p.TBBase = upstream.URL, upstream.URL
	for _, service := range []string{"alldebrid", "torbox"} {
		if err := p.ValidateKey(context.Background(), service, "synthetic-visitor-key"); err == nil {
			t.Fatal("redirecting provider authenticated an account")
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("visitor credentials followed an upstream redirect")
	}
}
