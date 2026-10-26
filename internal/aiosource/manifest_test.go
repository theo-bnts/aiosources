package aiosource

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestAddonManifestsAdvertiseTheSamePublicLogoAndVersion(t *testing.T) {
	base := Manifest()
	filtered := InstallationManifest(model.StreamPreferences{Types: []string{"movie"}, Services: []string{"torbox"}})
	for _, manifest := range []map[string]any{base, filtered} {
		if manifest["version"] != Version || Version != "2.2.0" || manifest["logo"] != LogoURL {
			t.Fatal("addon identity does not include the current version and logo")
		}
		logo, err := url.Parse(manifest["logo"].(string))
		if err != nil || logo.Scheme != "https" || logo.Host != "raw.githubusercontent.com" || logo.User != nil || logo.RawQuery != "" || logo.Fragment != "" || logo.Path != "/theo-bnts/aiosources/main/web/public/logo.png" {
			t.Fatal("logo is not a public PNG independent of instance credentials")
		}
	}
	if base["id"] != "org.aiosource.v2" || filtered["id"] == base["id"] || !reflect.DeepEqual(filtered["types"], []string{"movie"}) {
		t.Fatal("branding changed installation identity or media filters")
	}
	if Manifest()["id"] != base["id"] || !reflect.DeepEqual(Manifest()["types"], []string{"movie", "series", "anime"}) {
		t.Fatal("filtered branding mutated the shared manifest")
	}
}
