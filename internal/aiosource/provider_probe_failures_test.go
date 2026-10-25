package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDebridLinkStopsAfterDetailFailuresAndKeepsVerifiedResults(t *testing.T) {
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)}
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{"rate limit", `{"success":false,"error":"floodDetected"}`, "rate_limit", 429},
		{"authentication", `{"success":false,"error":"badToken"}`, "authentication", 401},
		{"account limit", `{"success":false,"error":"maxTorrent"}`, "account_limit", 200},
		{"subscription", `{"success":false,"error":"access_denied"}`, "subscription", 200},
		{"unavailable", `{"success":false,"error":"internalError"}`, "provider_unavailable", 503},
		{"removed item", `{"success":false,"error":"badId"}`, "not_found", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			adds, details := 0, 0
			p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/seedbox/add":
					adds++
					if adds == 2 {
						fmt.Fprint(w, `{"success":true,"value":{"id":"pending"}}`)
						return
					}
					if adds > len(hashes) {
						t.Error("unexpected additional provider request")
						w.WriteHeader(500)
						return
					}
					fmt.Fprintf(w, `{"success":true,"value":{"id":"ready","hashString":"%s","downloadPercent":100,"files":[{"name":"Example.mkv","size":100,"downloadUrl":"https://cdn.example/video","downloadPercent":100}]}}`, hashes[adds-1])
				case "/seedbox/list":
					details++
					w.Header().Set("Retry-After", "17")
					w.WriteHeader(test.status)
					fmt.Fprint(w, test.body)
				default:
					t.Error("unexpected provider endpoint")
					w.WriteHeader(404)
				}
			})
			items, err := p.DLCheck(t.Context(), "synthetic-key", hashes)
			if test.code == "not_found" {
				if err != nil || adds != 3 || details != 1 || len(items) != 2 || items[1].Hash != hashes[2] {
					t.Fatalf("a removed item prevented other checks: adds=%d details=%d items=%d err=%v", adds, details, len(items), err)
				}
			} else if providerFailureCode(err) != test.code || adds != 2 || details != 1 || len(items) != 1 {
				t.Fatalf("detail failure was ignored: adds=%d details=%d items=%d code=%s", adds, details, len(items), providerFailureCode(err))
			}
			if len(items) == 0 || items[0].Hash != hashes[0] || !items[0].Cached {
				t.Fatal("earlier verified availability was discarded")
			}
			if test.code == "rate_limit" {
				var failure *ProviderError
				if !errors.As(err, &failure) || failure.RetryAfter != 17 {
					t.Fatal("provider retry deadline was discarded")
				}
			}
		})
	}
}

func TestDebridLinkPropagatesCancellationWithoutStartingMoreChecks(t *testing.T) {
	p := extendedProviderTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a cancelled availability check contacted the provider")
		w.WriteHeader(500)
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err := p.DLCheck(ctx, "synthetic-key", []string{strings.Repeat("a", 40), strings.Repeat("b", 40)})
	if !errors.Is(err, context.Canceled) || len(items) != 0 {
		t.Fatalf("cancellation was lost: items=%d code=%s", len(items), providerFailureCode(err))
	}
}
