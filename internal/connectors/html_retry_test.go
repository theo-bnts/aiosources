package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestHTMLMissingDetailRetriesOnlyThatReadAndKeepsAtomicPage(t *testing.T) {
	for _, recover := range []bool{true, false} {
		t.Run(map[bool]string{true: "temporary", false: "persistent"}[recover], func(t *testing.T) {
			var listing, detail, retained atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "0")
				if r.URL.Path == "/detail/1" {
					attempt := detail.Add(1)
					if !recover || attempt == 1 {
						htmlResponse(w, `<main>Temporarily unavailable</main>`)
						return
					}
					htmlResponse(w, `<main id="detail"><span class="hash">`+strings.Repeat("a", 40)+`</span></main>`)
					return
				}
				listing.Add(1)
				htmlResponse(w, htmlListing(`<article data-id="1"><span class="title">Example Movie</span><a class="detail" href="/detail/1">Details</a></article>`))
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.HTML.Details = &model.HTMLDetails{URL: model.HTMLField{Selector: "a.detail", Attribute: "href"}, PageSelector: "#detail", Fields: map[string]model.HTMLField{"info_hash": {Selector: ".hash", Format: "info_hash", Required: true}}}
			c := protocolOpen(t, p, model.ModeFull, Environment{TransientFailure: func(_ context.Context, response Response, retryAt time.Time, attempt int, retry bool) error {
				retained.Add(1)
				if !response.InvalidDocument || response.StatusCode != 200 || retryAt.IsZero() || retry != (attempt <= maxHTTPRetries) {
					t.Fatal("HTML retry bypassed the ordinary response boundary")
				}
				return nil
			}})
			defer c.Close()
			page, err := c.Fetch(t.Context(), nil)
			if listing.Load() != 1 {
				t.Fatal("retry downloaded the complete listing again")
			}
			if recover {
				if err != nil || !page.Done || len(page.Items) != 1 || detail.Load() != 2 || retained.Load() != 1 || page.Items[0].Fields["info_hash"] != strings.Repeat("a", 40) {
					t.Fatalf("temporary HTML did not recover atomically: details=%d retained=%d err=%v", detail.Load(), retained.Load(), err)
				}
			} else if err == nil || page.Done || len(page.Next) > 0 || detail.Load() != 4 || retained.Load() != 4 || FailureCode(err) != "parse" {
				t.Fatalf("persistent missing page was skipped or retried without a bound: next=%s details=%d err=%v", page.Next, detail.Load(), err)
			}
			if encoded, _ := json.Marshal(page.Metadata); strings.Contains(string(encoded), server.URL) {
				t.Fatal("HTML diagnostics retained a request URL")
			}
		})
	}
}
