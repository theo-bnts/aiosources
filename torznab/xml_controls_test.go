package torznab

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSearchControlReplacementIsOptInAndRetainsOriginalBytes(t *testing.T) {
	fragment := "<item><guid>example-1</guid><title>Example\x01Release</title></item>"
	body := []byte("<rss><channel>" + fragment + "</channel></rss>")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			_, _ = io.WriteString(w, transportCapsXML)
		} else {
			_, _ = w.Write(body)
		}
	}))
	defer server.Close()
	for _, enabled := range []bool{false, true} {
		client, err := Open(t.Context(), Config{URL: server.URL, RequestInterval: time.Nanosecond, ReplaceInvalidXMLControls: enabled})
		if err != nil {
			t.Fatal(err)
		}
		page, searchErr := client.Search(t.Context(), Query{})
		raw, rawErr := client.SearchRaw(t.Context(), Query{})
		if !bytes.Equal(raw.Body, body) {
			t.Fatal("search replaced the archival response")
		}
		if !enabled {
			if searchErr == nil || rawErr == nil {
				t.Fatal("strict XML parsing changed without explicit authorization")
			}
			continue
		}
		if searchErr != nil || rawErr != nil || len(page.Items) != 1 || len(raw.Items) != 1 || raw.Items[0].Error != nil {
			t.Fatal("control replacement did not recover the complete response", searchErr, rawErr)
		}
		if page.Items[0].Title != "Example Release" || raw.Items[0].Item.Title != page.Items[0].Title || !bytes.Equal(raw.Items[0].Body, []byte(fragment)) {
			t.Fatal("normalization diverged or archival item bytes changed")
		}
	}
}

func TestControlReplacementKeepsEveryOtherXMLAndPaginationCheck(t *testing.T) {
	for _, body := range []string{
		`<!DOCTYPE rss><rss><channel/></rss>`,
		`<rss><channel><item><title>Unknown &unsafe;</title></item></channel></rss>`,
		`<rss><channel><item><title>Unclosed</item></channel></rss>`,
		`<rss><channel><response offset="0" offset="0"/></channel></rss>`,
		`<rss><channel><response offset="1"/></channel></rss>`,
		"<rss><channel><item><title>Invalid\xffUTF8</title></item></channel></rss>",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") == "caps" {
				_, _ = io.WriteString(w, transportCapsXML)
			} else {
				_, _ = io.WriteString(w, body)
			}
		}))
		client, err := Open(t.Context(), Config{URL: server.URL, RequestInterval: time.Nanosecond, ReplaceInvalidXMLControls: true})
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		_, err = client.Search(t.Context(), Query{})
		_, rawErr := client.SearchRaw(t.Context(), Query{})
		server.Close()
		if err == nil || rawErr == nil {
			t.Fatal("control replacement bypassed a document or pagination check")
		}
	}
}

func TestControlReplacementPreservesWhitespaceAndDoesNotMutateInput(t *testing.T) {
	var original []byte
	for value := byte(0); value < 32; value++ {
		original = append(original, value)
	}
	original = append(original, []byte("é普通")...)
	before := bytes.Clone(original)
	cleaned := replaceInvalidXMLControls(original)
	if !bytes.Equal(original, before) || len(cleaned) != len(original) || !bytes.Equal(cleaned[32:], original[32:]) {
		t.Fatal("replacement changed archival bytes, offsets or valid Unicode")
	}
	for value := byte(0); value < 32; value++ {
		want := byte(' ')
		if value == '\t' || value == '\r' || value == '\n' {
			want = value
		}
		if cleaned[value] != want {
			t.Fatal("XML control or legal whitespace was not handled correctly")
		}
	}
}
