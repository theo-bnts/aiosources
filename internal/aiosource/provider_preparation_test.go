package aiosource

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestProviderPreparationIsBoundToServiceAccountAndTorrent(t *testing.T) {
	_, s, _ := testService(t)
	hash := strings.Repeat("a", 40)
	for _, service := range []string{"realdebrid", "premiumize", "debridlink", "deepbrid"} {
		token, err := s.sealPreparation(service, "synthetic-account-key", hash, "17")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(token, "synthetic-account-key") {
			t.Fatal("key entered preparation")
		}
		if id, err := s.openPreparation(service, "synthetic-account-key", hash, token); err != nil || id != "17" {
			t.Fatalf("continuation lost: %v", err)
		}
		for _, other := range []struct{ service, key, hash string }{{service, "other-account-key", hash}, {service, "synthetic-account-key", strings.Repeat("b", 40)}, {"torbox", "synthetic-account-key", hash}} {
			if _, err := s.openPreparation(other.service, other.key, other.hash, token); err == nil {
				t.Fatal("preparation crossed its account/resource boundary")
			}
		}
		data, _ := json.Marshal(providerPreparation{service, fingerprint("synthetic-account-key"), hash, "17", time.Now().Add(-time.Second).Unix()})
		sealed, err := s.Vault.Seal("provider-preparation-v1", data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.openPreparation(service, "synthetic-account-key", hash, base64.RawURLEncoding.EncodeToString(sealed)); err == nil {
			t.Fatal("expired continuation accepted")
		}
	}
}

func TestVisitorPreparationIsBoundedAndStripsDownloadCredentials(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("a", 40)
	s.rememberVisitorPreparation("deepbrid", "visitor-key", hash, "17", []model.AIOFile{{Path: "Example.mkv", Size: 100, ID: 9, Link: "https://cdn.example/visitor-private-link"}}, 1)
	item := s.visitorPreparation("deepbrid", "visitor-key", hash)
	if len(item.Files) != 1 || item.Files[0].Link != "" || item.Files[0].ID != 0 {
		t.Fatal("account-bound link retained in visitor metadata")
	}
	if s.visitorPreparation("deepbrid", "other-key", hash).ID != "" {
		t.Fatal("visitor account crossover")
	}
	for i := range 520 {
		s.rememberVisitorPreparation("deepbrid", itoa64(int64(i)), hash, "17", nil, 0)
	}
	if len(s.visitorPreparations) > 512 {
		t.Fatal("visitor state grew without a bound")
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_preparations`).Scan(&count); err != nil || count != 0 {
		t.Fatal("visitor preparation persisted in PostgreSQL")
	}
}
