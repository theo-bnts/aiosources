package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestCacheStatisticsAreAdminOnlyAndContainNoPrivateIdentifiers(t *testing.T) {
	f, _, _ := addonAPI(t)
	const route = "/api/debrid/statistics"
	if got := f.request(http.MethodGet, route, "", false, ""); got.Code != http.StatusUnauthorized {
		t.Fatal("cache statistics are public")
	}
	g, err := f.db.AIOCacheGeneration(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 40)
	if err := f.db.SaveSharedAvailability(f.ctx, "torbox", model.AIOAvailability{Generation: g, Hash: hash, Cached: true, CheckedAt: time.Now().Add(-time.Minute), Files: []model.AIOFile{{Path: "synthetic-private-filename", Size: 100}}}); err != nil {
		t.Fatal(err)
	}
	response := f.request(http.MethodGet, route, "", true, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cached":1`) {
		t.Fatalf("missing cache counts: %d %s", response.Code, response.Body.String())
	}
	for _, private := range []string{hash, "synthetic-private-filename", "account", "files", "secret"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("cache statistics disclose %s", private)
		}
	}
}
