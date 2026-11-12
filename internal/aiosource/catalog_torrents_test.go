package aiosource

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDirectCatalogResultsShareFormatterWithoutPrivatePayloads(t *testing.T) {
	ctx, s, conn := testService(t)
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('source','one','{"title":"Example.Tales.EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100,"private_note":"synthetic-private-payload","announce":"https://secret.invalid/passkey"}')`); err != nil {
		t.Fatal(err)
	}
	page, err := s.CatalogTorrents(ctx, model.ListOptions{Query: "Example Tales"}, model.CatalogReleaseFilters{}, nil)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || len(page.Items[0].Presentation.Details) == 0 {
		t.Fatalf("direct presentation: %+v %v", page, err)
	}
	encoded, _ := json.Marshal(page)
	for _, private := range []string{"synthetic-private-payload", "secret.invalid", "aaaaaaaaaaaa", "source_id"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private field leaked through direct results")
		}
	}
}

func TestDirectTorrentFilesRequireExplicitChoiceAndDoNotChangeVideoSelection(t *testing.T) {
	files := []model.AIOFile{{Path: "Example.epub", Size: 10}, {Path: "Example.pdf", Size: 20}, {Path: "Example.mkv", Size: 30}}
	r := Resource{Type: "torrent"}
	if _, ok := SelectFile(files, r); ok {
		t.Fatal("direct search silently chose one of several files")
	}
	choice, ok := fileSelectionError(files, r).(*bookFileChoice)
	if !ok || len(choice.Files) != 3 {
		t.Fatal("file choices missing")
	}
	r.File = "Example.pdf"
	if file, ok := SelectFile(files, r); !ok || file.Path != r.File {
		t.Fatal("explicit file lost")
	}
	if file, ok := SelectFile(files, Resource{Type: "movie"}); !ok || file.Path != "Example.mkv" {
		t.Fatal("movie selection changed")
	}
}

func TestDirectPublicFilePredicateMatchesDownloadSelection(t *testing.T) {
	ctx, _, conn := testService(t)
	for _, files := range [][]model.AIOFile{
		nil, {}, {{Path: "sample.mkv", Size: 10}}, {{Path: "docs/readme-first.txt"}},
		{{Path: "docs\\readme.txt"}}, {{Path: "info.nfo"}}, {{Path: "Example.pdf", Size: -1}},
		{{Path: "Example.epub", Size: 10}}, {{Path: "docs/readme.txt"}, {Path: "Example.mkv", Size: 20}},
		{{Path: strings.Repeat("x", 4097), Size: 1}},
	} {
		encoded, _ := json.Marshal(files)
		var allowed bool
		if err := conn.QueryRow(ctx, `SELECT aiosources.catalog_has_download_file($1::jsonb)`, encoded).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed != (len(resourceDownloadFiles(files, Resource{Type: "torrent"})) > 0) {
			t.Fatalf("SQL visibility differs from download eligibility: %+v", files)
		}
	}
}

func TestDirectPublicResultsUseOnlySharedCacheOrVisitorAnnouncements(t *testing.T) {
	ctx, s, conn := testService(t)
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled = true
	access.SearchEnabled = true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.torrents(provider_id,source_id,fields) VALUES('private','one','{"title":"Example.Digital.EPUB","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}'),('private','two','{"title":"Example.Digital.PDF","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}')`); err != nil {
		t.Fatal(err)
	}
	config, _ := s.DB.AIOConfig(ctx)
	config.Trackers["private"] = model.AIOTracker{Private: true}
	if err := s.SaveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO aiosources.aio_shared_availability(service,hash,cached,checked_at,files) VALUES('torbox',$1,true,now(),'[{"path":"Example.epub","size":10}]')`, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	visitor := &PublicInstallation{Services: map[string]string{"torbox": "synthetic-visitor-key"}, Generation: access.Generation, Audience: "https://addon.example"}
	// Existing shared availability must not be retried with an admin credential.
	page, err := s.CatalogTorrents(ctx, model.ListOptions{Query: "Example"}, model.CatalogReleaseFilters{}, visitor)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || len(page.Items[0].Services) != 1 || page.Items[0].Services[0].State != "cached" || page.Items[0].Provider != "private" || page.Items[0].DownloadToken == "" {
		t.Fatalf("public direct safety: %+v %v", page, err)
	}
	if _, err := conn.Exec(ctx, `UPDATE aiosources.aio_shared_availability SET checked_at=$1`, time.Now().Add(-365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	page, err = s.CatalogTorrents(ctx, model.ListOptions{Query: "Example"}, model.CatalogReleaseFilters{}, visitor)
	if err != nil || page.Total != 0 {
		t.Fatal("private stale/unknown content was exposed", err)
	}
}
