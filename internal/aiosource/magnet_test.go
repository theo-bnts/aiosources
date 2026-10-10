package aiosource

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestMagnetRetainsV1ExactTopicAndEscapesDisplayNames(t *testing.T) {
	for _, title := range []string{"", "Example Movie 2021", "A+B & 100% ? # = : /", "Été 日本語"} {
		t.Run(title, func(t *testing.T) {
			hash := strings.Repeat("A", 40)
			s := &Service{}
			magnet, sources, err := s.magnet(context.Background(), model.AIORecord{Hash: hash, Title: title}, model.AIOConfig{})
			if err != nil {
				t.Fatal(err)
			}
			// v1 sends the literal exact topic first, before escaped display names
			// or announce URLs. Form encoding happens only at the API boundary.
			if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:"+strings.ToLower(hash)+"&") {
				t.Fatal("magnet no longer uses the v1 exact-topic wire format")
			}
			parsed, err := url.Parse(magnet)
			if err != nil || parsed.Query().Get("dn") != title || len(parsed.Query()["xt"]) != 1 || parsed.Query().Get("xt") != "urn:btih:"+strings.ToLower(hash) || len(sources) != 0 {
				t.Fatal("magnet fields were changed, duplicated or not escaped")
			}
		})
	}
}
