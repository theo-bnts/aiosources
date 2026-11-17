package connectors

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestTorznabControlReplacementPreservesIdentityArchiveAndContinuation(t *testing.T) {
	fragment := "<item><guid>example-1</guid><title>Example\x01Release</title></item>"
	body := "<rss><channel>" + fragment + "</channel></rss>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("t") == "caps":
			_, _ = io.WriteString(w, protocolCaps)
		case r.URL.Query().Get("offset") == "0":
			_, _ = io.WriteString(w, body)
		default:
			_, _ = io.WriteString(w, "<rss><channel/></rss>")
		}
	}))
	defer server.Close()
	p := protocolProvider("torznab", server.URL)
	p.Options = map[string]any{"replace_invalid_xml_controls": "true"}
	if Validate(p) == nil {
		t.Fatal("XML control policy accepted a non-boolean value")
	}
	p.Options["replace_invalid_xml_controls"] = true
	c := protocolOpen(t, p, model.ModeFull, Environment{})
	defer c.Close()
	first, err := c.Fetch(t.Context(), nil)
	if err != nil || first.Done || !bytes.Equal(first.Body, []byte(body)) {
		t.Fatal("search did not retain the raw response or continue", err)
	}
	found := false
	for _, record := range first.Items {
		if !record.Auxiliary {
			found = true
			if record.SourceID != "example-1" || record.Error != "" || record.Fields["title"] != "Example Release" || !bytes.Equal(record.Raw, []byte(fragment)) {
				t.Fatal("XML repair changed identity, normalized fields or original raw bytes")
			}
		}
	}
	last, err := c.Fetch(t.Context(), first.Next)
	if !found || err != nil || !last.Done {
		t.Fatal("repaired page failed to advance to the confirmed empty page", err)
	}
}
