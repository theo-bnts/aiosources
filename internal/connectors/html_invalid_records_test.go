package connectors

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestHTMLInvalidRecordPolicyPreservesValidRowsAndReportsRejections(t *testing.T) {
	for _, policy := range []string{"", "fail", "reject"} {
		t.Run("policy_"+policy, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				htmlResponse(w, htmlListing(`<article data-id="one"><span class="title">First example</span><span class="hash">`+strings.Repeat("a", 40)+`</span></article><article data-id="two"><span class="title">Second example</span><span class="hash">invalid-upstream-value</span></article><article data-id="three"><span class="title">Third example</span><span class="hash">`+strings.Repeat("b", 40)+`</span></article>`))
			}))
			t.Cleanup(server.Close)
			p := htmlProvider(server.URL)
			p.HTML.InvalidRecordPolicy = policy
			p.HTML.Fields["info_hash"] = model.HTMLField{Selector: ".hash", Format: "info_hash", Required: true}
			source := protocolOpen(t, p, model.ModeFull, Environment{})
			defer source.Close()
			page, err := source.Fetch(t.Context(), nil)
			if policy != "reject" {
				if err == nil || page.Done || len(page.Next) > 0 {
					t.Fatal("strict policy advanced an invalid page")
				}
				return
			}
			if err != nil || !page.Done || len(page.Items) != 3 || len(page.Next) == 0 || page.Items[1].Error == "" || len(page.Items[1].Fields) != 0 || page.Items[1].SourceID != "two" || page.Items[0].Error != "" || page.Items[2].Error != "" {
				t.Fatalf("record rejection lost valid rows or progress: items=%d done=%v err=%v", len(page.Items), page.Done, err)
			}
			if strings.Contains(page.Items[1].Error, "invalid-upstream-value") {
				t.Fatal("rejected field leaked upstream data")
			}
		})
	}
}

func TestHTMLRecordRejectionStillStopsEmptyInvalidPagesAndMissingIdentities(t *testing.T) {
	for _, body := range []string{htmlListing(htmlRow("one", "invalid")), htmlListing(`<article data-id=""><span class="title">Example</span></article>`), `<form>Sign in</form>`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { htmlResponse(w, body) }))
		p := htmlProvider(server.URL)
		p.HTML.InvalidRecordPolicy = "reject"
		p.HTML.Fields["info_hash"] = model.HTMLField{Selector: ".hash", Format: "info_hash", Required: true}
		source := protocolOpen(t, p, model.ModeFull, Environment{})
		page, err := source.Fetch(t.Context(), nil)
		source.Close()
		server.Close()
		if err == nil || page.Done || len(page.Next) > 0 {
			t.Fatal("rejection bypassed page or identity validation")
		}
	}
}
