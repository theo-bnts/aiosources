package aiosource

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

func TestPrivateInstallationPoliciesSupportSavedSourceSetsAndBlockPlaybackBypass(t *testing.T) {
	ctx, service, conn := testService(t)
	seedMovie(t, ctx, service, conn, "alpha", "one", strings.Repeat("a", 40))
	sources := []string{"alpha"}
	for i := range 120 {
		sources = append(sources, fmt.Sprintf("example-%d", i))
	}
	grant, err := service.DB.CreateShare(ctx, model.ShareInput{Format: "stremio", Name: "Example", Enabled: true, Scope: "selected", SourceIDs: sources, Fields: store.CatalogFields(), StreamPreferences: &model.StreamPreferences{Language: "de", Mode: "preferred", Services: []string{"torbox"}}})
	if err != nil {
		t.Fatal(err)
	}
	token := "s1." + grant.Share.ID + "." + grant.Password
	p, err := service.PrivatePreferences(ctx, token)
	if err != nil || len(p.Sources) != len(sources) || !model.ValidPlaybackPreferences(p) || ValidStreamPreferences(p) {
		t.Fatal("saved policy was limited by URL filter size", err)
	}
	if _, err = service.PlaybackWithPreferences(ctx, "torbox", "alpha", "one", "movie", "tt1234567", p); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("manually constructed playback bypassed the language policy", err)
	}
	for _, malformed := range []string{"s1.", "s1." + grant.Share.ID, token + "~fr~all", token + ".suffix", "s1." + grant.Share.ID + ".bad"} {
		if err = service.ValidateToken(ctx, malformed); !errors.Is(err, ErrAuth) {
			t.Fatal("malformed named installation accepted", err)
		}
	}
	if _, _, named, err := PrivateConnectionKey(strings.Repeat("A", 43)); err != nil || named {
		t.Fatal("legacy token identified as a named connection")
	}
}
