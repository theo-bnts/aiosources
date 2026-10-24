package aiosource

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPublicProviderDeadlineRetainsOnlyAlreadyVerifiedEvidence(t *testing.T) {
	ctx, service, _ := testService(t)
	request, cancel := context.WithCancel(ctx)
	defer cancel()
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cache/check":
			fmt.Fprint(w, `{"status":"success","response":[true,true]}`)
		case "/transfer/directdl":
			_ = r.ParseForm()
			if strings.Contains(r.Form.Get("src"), hashes[1]) {
				cancel()
				return
			}
			fmt.Fprint(w, `{"status":"success","content":[{"path":"Example.Movie.2021.mkv","size":100,"link":"https://cdn.example/ready.mkv"}]}`)
		default:
			t.Error("unexpected upstream request")
			w.WriteHeader(400)
		}
	})
	service.Providers = p
	items, err := service.checkPublicService(request, "premiumize", "synthetic-key", "example-deadline", hashes)
	if err == nil || len(items) != 1 || items[0].Hash != hashes[0] || !items[0].Cached || len(items[0].Files) != 1 {
		t.Fatalf("verified partial result lost or failed request presented as evidence: %+v %v", items, err)
	}
	saved, err := service.DB.SharedAvailability(ctx, "premiumize", hashes, time.Now().Add(-time.Minute))
	if err != nil || len(saved) != 1 || saved[0].Hash != hashes[0] || saved[0].Files[0].Link != "" {
		t.Fatal("safe shared evidence was not retained", err)
	}
}

func TestPublicProviderResetFencesOutInFlightRequestResults(t *testing.T) {
	ctx, service, _ := testService(t)
	hash := strings.Repeat("a", 40)
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cache/check":
			fmt.Fprint(w, `{"status":"success","response":[true]}`)
		case "/transfer/directdl":
			if err := service.DB.ResetAIOCache(ctx); err != nil {
				t.Error(err)
			}
			fmt.Fprint(w, `{"status":"success","content":[{"path":"Example.Movie.2021.mkv","size":100,"link":"https://cdn.example/ready.mkv"}]}`)
		default:
			t.Error("unexpected upstream request")
			w.WriteHeader(400)
		}
	})
	service.Providers = p
	items, err := service.checkPublicService(ctx, "premiumize", "synthetic-key", "example-reset", []string{hash})
	if err != nil || len(items) != 0 {
		t.Fatalf("an observation from before the reset was returned: %+v %v", items, err)
	}
	saved, err := service.DB.SharedAvailability(ctx, "premiumize", []string{hash}, time.Now().Add(-time.Minute))
	if err != nil || len(saved) != 0 {
		t.Fatal("an observation from before the reset was persisted", err)
	}
}
