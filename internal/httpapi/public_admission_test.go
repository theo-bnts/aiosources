package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/theo-bnts/aiosources/internal/inbound"
	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicQuotasUseTrustedVisitorAddressesAndSeparateRequestFamilies(t *testing.T) {
	resolver, err := inbound.New(&model.IncomingNetwork{Enabled: true, IPHeader: "CF-Connecting-IP", Format: "single", TrustedProxies: []string{"10.0.0.2"}})
	if err != nil {
		t.Fatal(err)
	}
	a := &publicAdmission{policy: func(context.Context) (requestPolicy, error) {
		return requestPolicy{clients: resolver, limits: model.PublicLimits{SetupPerMinute: 2, RequestsPerMinute: 3, Concurrent: 5}}, nil
	}}
	for _, peer := range []string{"10.0.0.2:1234", "192.0.2.5:1234"} {
		for i := range 6 {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = peer
			r.Header.Set("CF-Connecting-IP", fmt.Sprintf("203.0.113.%d", i%2))
			release, retry, err := a.reserve(r, publicSetup)
			want := i < 4
			if peer == "192.0.2.5:1234" {
				want = i < 2
			}
			if err != nil || (release != nil) != want {
				t.Fatal("visitor quotas were shared or spoofed", i, peer)
			}
			if release != nil {
				release()
			} else if retry < 1 || retry > 60 {
				t.Fatal("invalid retry deadline")
			}
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.0")
	for range 20 {
		release, _, err := a.reserve(r, publicConfig)
		if err != nil || release == nil {
			t.Fatal("homepage refresh consumed setup quota")
		}
		release()
	}
	for i := range 4 {
		release, _, err := a.reserve(r, publicAddon)
		if err != nil || (release != nil) != (i < 3) {
			t.Fatal("addon and setup quotas are not independent")
		}
		if release != nil {
			release()
		}
	}
}

func TestPublicConcurrencyUpdatesDoNotDropActiveSlots(t *testing.T) {
	limits := model.DefaultPublicLimits()
	a := &publicAdmission{policy: func(context.Context) (requestPolicy, error) { return requestPolicy{limits: limits}, nil }}
	r := httptest.NewRequest("GET", "/", nil)
	var releases []func()
	for range 50 {
		release, _, _ := a.reserve(r, publicConfig)
		if release == nil {
			t.Fatal("default capacity is below 50")
		}
		releases = append(releases, release)
	}
	if release, retry, _ := a.reserve(r, publicConfig); release != nil || retry != 1 {
		t.Fatal("capacity was exceeded")
	}
	limits.Concurrent = 1
	for _, release := range releases[:49] {
		release()
		release()
	}
	if release, _, _ := a.reserve(r, publicConfig); release != nil {
		t.Fatal("lowering the limit released an active request")
	}
	releases[49]()
	release, _, _ := a.reserve(r, publicConfig)
	if release == nil {
		t.Fatal("capacity did not recover after release")
	}
	release()
	limits.Concurrent = 2
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			release, _, _ := a.reserve(r, publicConfig)
			if release != nil {
				release()
			}
		})
	}
	wg.Wait()
	if a.inflight != 0 {
		t.Fatal("concurrent admission leaked slots")
	}
}

func TestPublicAdmissionFailsClosedWhenSettingsCannotBeRead(t *testing.T) {
	a := &publicAdmission{policy: func(context.Context) (requestPolicy, error) {
		return requestPolicy{}, errors.New("private database diagnostic")
	}}
	w := httptest.NewRecorder()
	if _, ok := a.enter(w, httptest.NewRequest("GET", "/", nil), publicAddon); ok || w.Code != 503 {
		t.Fatal("unavailable policy was ignored")
	}
	if a.inflight != 0 || len(a.windows) != 0 {
		t.Fatal("failed admission consumed a slot")
	}
}
