package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestShareMetadataUsesConfiguredOriginWithoutReflectingVisitorURLs(t *testing.T) {
	data, err := os.ReadFile("../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	frontend := fstest.MapFS{"index.html": &fstest.MapFile{Data: data}, "share-preview.png": &fstest.MapFile{Data: []byte("synthetic-image")}}
	handler := staticFrontend(frontend, "https://catalog.example", "2.2.0-test")
	for _, path := range []string{"/", "/debrid/settings?private-test-key=do-not-reflect", "/public/setup?announce=do-not-reflect"} {
		request := httptest.NewRequest("GET", path, nil)
		request.Host = "untrusted.example"
		request.Header.Set("X-Forwarded-Host", "untrusted.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != 200 || !strings.Contains(body, `content="https://catalog.example/share-preview.png"`) || !strings.Contains(body, `property="og:title"`) || !strings.Contains(body, `name="twitter:card"`) || !strings.Contains(body, `name="application-version" content="2.2.0-test"`) {
			t.Fatal("social metadata missing")
		}
		for _, private := range []string{"untrusted.example", "do-not-reflect", "__AIOSOURCES_PUBLIC_ORIGIN__", "__AIOSOURCES_VERSION__"} {
			if strings.Contains(body, private) {
				t.Fatal("request data entered share metadata")
			}
		}
		if response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("instance HTML cached")
		}
	}
	request := httptest.NewRequest(http.MethodHead, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("crawler HEAD response invalid")
	}
	for _, bad := range []string{"https://user:password@example.test", "https://example.test/?key=private", "javascript:alert(1)", "https://example.test/#fragment", "https://example.test/path"} {
		if sharePreviewOrigin(bad) != "" {
			t.Fatal("invalid preview origin accepted")
		}
	}
}

func TestApplicationVersionIsEscapedInPublicHTML(t *testing.T) {
	frontend := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(`<meta name="application-version" content="__AIOSOURCES_VERSION__">`)}}
	for _, tc := range []struct{ version, expected string }{
		{"", "dev"},
		{"2.2.0-test", "2.2.0-test"},
		{`test"><script>unexpected()</script>`, `test&#34;&gt;&lt;script&gt;unexpected()&lt;/script&gt;`},
	} {
		response := httptest.NewRecorder()
		staticFrontend(frontend, "https://catalog.example", tc.version).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusOK || response.Body.String() != `<meta name="application-version" content="`+tc.expected+`">` {
			t.Fatal("application version was not safely embedded")
		}
	}
}
