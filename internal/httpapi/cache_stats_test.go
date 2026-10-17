package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func TestCacheStatisticsIncludePrivateNegativeChecksForOnlyTheCurrentAccount(t *testing.T) {
	f, addon, _ := addonAPI(t)
	if err := addon.Vault.Put(f.ctx, "test-debrid", "synthetic-negative-statistics-key"); err != nil {
		t.Fatal(err)
	}
	config, _ := f.db.AIOConfig(f.ctx)
	config.AllDebridSecret = "test-debrid"
	if err := addon.SaveConfig(f.ctx, config); err != nil {
		t.Fatal(err)
	}
	generation, _ := f.db.AIOCacheGeneration(f.ctx)
	// A private AllDebrid result is deliberately absent from the shared cache.
	account := sha256.Sum256([]byte("synthetic-negative-statistics-key"))
	for i, cached := range []bool{true, false, false} {
		if err := f.db.SaveAIOAvailability(f.ctx, "alldebrid", hex.EncodeToString(account[:]), model.AIOAvailability{Generation: generation, Hash: strings.Repeat(string(rune('a'+i)), 40), Cached: cached, CheckedAt: time.Now().Add(-20 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.SaveAIOAvailability(f.ctx, "alldebrid", "different-account", model.AIOAvailability{Generation: generation, Hash: strings.Repeat("d", 40), Cached: true, CheckedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	response := f.request("GET", "/api/debrid/statistics", "", true, "")
	var result struct {
		Items []model.SharedCacheStats `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Checks == nil || result.Items[0].Checks.Cached != 1 || result.Items[0].Checks.Uncached != 2 {
		t.Fatal("negative or account isolation counts incorrect", response.Code, response.Body)
	}
	for _, private := range []string{"different-account", "synthetic-negative-statistics-key", hex.EncodeToString(account[:])} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatal("private statistics identity leaked")
		}
	}
}
