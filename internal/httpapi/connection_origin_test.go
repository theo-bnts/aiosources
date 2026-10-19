package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPrivateConnectionsSupportConfiguredInternalHTTP(t *testing.T) {
	for _, origin := range []string{"http://192.0.2.10:7000", "http://aiosources:7000"} {
		t.Run(origin, func(t *testing.T) {
			f := sharingAPI(t, origin)
			response := f.request("POST", "/api/torznab/endpoints", `{"name":"Internal client","scope":"all","enabled":true}`, true, "")
			var grant model.ShareCredential
			if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &grant) != nil || grant.Share.URL != origin+"/torznab/"+grant.Share.ID+"/api" {
				t.Fatal("private Torznab did not preserve the configured origin", response.Code, response.Body.String())
			}
			path := "/torznab/" + grant.Share.ID + "/api?t=caps"
			if response = f.request("GET", path, "", true, ""); response.Code != 401 {
				t.Fatal("admin session replaced the Torznab key", response.Code)
			}
			response = f.request("GET", path+"&apikey="+grant.Password, "", false, "")
			if response.Code != 200 || !strings.Contains(response.Body.String(), origin) || strings.Contains(response.Body.String(), grant.Password) {
				t.Fatal("internal capabilities were unavailable or exposed credentials", response.Code)
			}
			response = f.request("GET", "/api/torznab/endpoints", "", true, "")
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"public_url":"`+origin+`"`) {
				t.Fatal("management returned a different origin", response.Code)
			}
			stremio := createStremioAccess(t, f, model.ShareInput{Name: "Internal Stremio", Scope: "all", Enabled: true, StreamPreferences: &model.StreamPreferences{Mode: "all"}})
			if !strings.HasPrefix(stremio.Share.URL, origin+"/") {
				t.Fatal("private Stremio did not preserve internal HTTP")
			}
			response = f.request("POST", "/api/shares", `{"name":"Catalog","scope":"all","enabled":true,"fields":["title"]}`, true, "")
			if response.Code != 422 {
				t.Fatal("internal connection support weakened catalog sharing HTTPS", response.Code)
			}
		})
	}
}

func TestConnectionOriginRejectsAmbiguousConfiguredURLs(t *testing.T) {
	for _, origin := range []string{"", "/relative", "ftp://host.test", "http://user@host.test", "https://host.test/prefix", "https://host.test?key=value", "https://host.test#fragment"} {
		s := server{options: Options{PublicURL: origin}}
		if s.connectionOrigin() != "" || s.sharingPublicURL() != "" {
			t.Fatalf("ambiguous configured origin accepted: %q", origin)
		}
	}
}
