package aiosource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicCatalogPrivateAnnouncementsAreOptionalPersonalAndRevocable(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	hash := strings.Repeat("a", 40)
	seedMovie(t, ctx, s, conn, "private-one", "native-private", hash)
	var work int64
	if err := conn.QueryRow(ctx, `SELECT id FROM ingest.aio_works`).Scan(&work); err != nil {
		t.Fatal(err)
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.SearchEnabled, access.ExposeSources = true, true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-own-search-key" {
			t.Error("Administrator key used")
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/user/me" {
			t.Error("Cached availability should not be re-requested")
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"id":1}}`)
	}))
	defer upstream.Close()
	s.Providers.TBBase = upstream.URL
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: false, CheckedAt: time.Now(), Generation: generation}); err != nil {
		t.Fatal(err)
	}
	announce := "https://announce.example.invalid/visitor-own-passkey/announce"
	configuration, err := s.CreatePublicInstallation(ctx, "https://addon.example", map[string]string{"torbox": "synthetic-own-search-key"}, map[string]string{"private-one": announce})
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := s.OpenPublicInstallation(ctx, "https://addon.example", configuration)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || rows.Total != 1 || rows.Items[0].ProviderName != "Example Library" || !rows.Items[0].Services[0].CanAdd {
		t.Fatal("Own announce did not authorize an uncached version", err)
	}
	browser, err := s.PublicBrowserCredential(ctx, "https://addon.example", visitor)
	if err != nil || len(browser.Services) != 1 || len(browser.Announces) != 1 {
		t.Fatal(err)
	}
	remembered, err := s.OpenPublicInstallation(ctx, "https://addon.example", browser.Credential)
	if err != nil || remembered.Announces["private-one"] != announce || len(remembered.Preferences.Sources) != 0 {
		t.Fatal("Browser credential lost its personal announce or retained filters", err)
	}
	rows, err = s.PublicCatalogReleases(ctx, remembered, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || rows.Total != 1 {
		t.Fatal("Remembered personal announce was not reused", err)
	}
	keysOnly := *remembered
	keysOnly.Announces = nil
	rows, err = s.PublicCatalogReleases(ctx, &keysOnly, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || rows.Total != 0 {
		t.Fatal("Keys-only configuration inherited an announce", err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.ExposeSources = false
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	rows, err = s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || rows.Total != 0 {
		t.Fatal("Withdrawn announcement permission remained active", err)
	}
	if err = s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Movie.2021.mkv", Size: 100}}}); err != nil {
		t.Fatal(err)
	}
	rows, err = s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || rows.Total != 1 || rows.Items[0].Provider != "" || rows.Items[0].ProviderName != "" {
		t.Fatal("Cached private release lost or identity revealed", err)
	}
	for _, files := range [][]model.AIOFile{nil, {{Path: "readme.txt", Size: 100}}} {
		if err = s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: files}); err != nil {
			t.Fatal(err)
		}
		hidden, e := s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
		if e != nil || hidden.Total != 0 {
			t.Fatal("Private cached release had no usable video", e)
		}
	}
	claim := rows.Items[0].DownloadToken
	input := model.CatalogDownloadInput{DownloadToken: claim, Service: "torbox"}
	if _, err = s.PublicCatalogDownload(ctx, visitor, work+1, input); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("Source capability crossed titles", err)
	}
	input.DownloadToken = configuration
	if _, err = s.PublicCatalogDownload(ctx, visitor, work, input); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("Installation capability crossed purposes", err)
	}
	if _, err = s.OpenPublicInstallation(ctx, "https://another.example", browser.Credential); err == nil {
		t.Fatal("Saved keys crossed origins")
	}
	access, _ = s.DB.PublicAccess(ctx)
	access.Enabled = false
	if err = s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicCatalogSearch(ctx, visitor, model.ListOptions{Query: "Example"}, model.CatalogSearchFilters{}); !errors.Is(err, ErrAuth) {
		t.Fatal("Disabled public Search remained accessible", err)
	}
}

func TestPublicCatalogCachedOnlyDoesNotOfferUncachedActions(t *testing.T) {
	ctx, s, conn := testService(t)
	visitorSourceRegistry(t, s)
	hash := strings.Repeat("c", 40)
	for _, provider := range []string{"private-one", "public-one"} {
		seedMovie(t, ctx, s, conn, provider, "one", hash)
	}
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.SearchEnabled, access.ExposeSources = true, true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{
		Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation,
		Files: []model.AIOFile{{Path: "Example.Movie.2021.mkv", Size: 100}},
	}); err != nil {
		t.Fatal(err)
	}
	var work int64
	if err := conn.QueryRow(ctx, `SELECT id FROM ingest.aio_works`).Scan(&work); err != nil {
		t.Fatal(err)
	}
	visitor := &PublicInstallation{
		Generation: access.Generation, Audience: "https://addon.example",
		Services:  map[string]string{"torbox": "synthetic-cache-only-account"},
		Announces: map[string]string{"private-one": "https://announce.example.invalid/visitor/announce"},
	}
	for _, cachedOnly := range []bool{false, true} {
		visitor.Preferences.CachedOnly = cachedOnly
		rows, err := s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
		if err != nil || rows.Total != 2 || len(rows.Items) != 2 {
			t.Fatal("cached releases changed with the uncached permission", rows, err)
		}
		for _, row := range rows.Items {
			if len(row.Services) != 1 || row.Services[0].State != "cached" || row.Services[0].CanAdd == cachedOnly {
				t.Fatal("download controls contradict cached-only access", cachedOnly, row.Services)
			}
		}
	}
}

func TestPublicCatalogDownloadContinuationCannotCrossVisitorAccounts(t *testing.T) {
	ctx, s, conn := testService(t)
	hash := strings.Repeat("b", 40)
	seedMovie(t, ctx, s, conn, "private", "one", hash)
	access, _ := s.DB.PublicAccess(ctx)
	access.Enabled, access.SearchEnabled = true, true
	if err := s.DB.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = s.DB.PublicAccess(ctx)
	generation, _ := s.DB.AIOCacheGeneration(ctx)
	if err := s.DB.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation, Files: []model.AIOFile{{Path: "Example.Movie.mkv", Size: 100}}}); err != nil {
		t.Fatal(err)
	}
	var calls int
	s.Providers = providerTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Path {
		case "/torrents/createtorrent":
			fmt.Fprintf(w, `{"success":true,"data":{"torrent_id":7,"hash":%q}}`, hash)
		case "/torrents/mylist":
			fmt.Fprintf(w, `{"success":true,"data":{"hash":%q,"download_finished":false,"download_present":false,"files":[]}}`, hash)
		default:
			t.Error("Unexpected provider call", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	visitor := &PublicInstallation{Generation: access.Generation, Audience: "https://addon.example", Services: map[string]string{"torbox": "synthetic-first-account"}}
	titles, err := s.PublicCatalogSearch(ctx, visitor, model.ListOptions{Query: "Example"}, model.CatalogSearchFilters{})
	if err != nil || titles.Total != 1 {
		t.Fatal(err)
	}
	work := titles.Items[0].ID
	releases, err := s.PublicCatalogReleases(ctx, visitor, work, model.CatalogSelection{}, model.ListOptions{}, model.CatalogReleaseFilters{})
	if err != nil || releases.Total != 1 {
		t.Fatal(err)
	}
	pending, err := s.PublicCatalogDownload(ctx, visitor, work, model.CatalogDownloadInput{DownloadToken: releases.Items[0].DownloadToken, Service: "torbox"})
	if !errors.Is(err, ErrPending) || pending.PreparationID == "" {
		t.Fatal("Expected a client-carried preparation", err)
	}
	before := calls
	other := *visitor
	other.Services = map[string]string{"torbox": "synthetic-second-account"}
	if _, err = s.PublicCatalogDownload(ctx, &other, work, model.CatalogDownloadInput{PreparationID: pending.PreparationID}); !errors.Is(err, model.ErrConflict) || calls != before {
		t.Fatal("Continuation crossed accounts", err)
	}
	if _, err = conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{size}','1')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicCatalogDownload(ctx, visitor, work, model.CatalogDownloadInput{PreparationID: pending.PreparationID}); (!errors.Is(err, model.ErrConflict) && !errors.Is(err, model.ErrNotFound)) || calls != before {
		t.Fatal("Stale preparation contacted provider", err)
	}
	var phase string
	if err = conn.QueryRow(context.Background(), `SELECT phase FROM ingest.operation_runs WHERE id=$1`, pending.OperationID).Scan(&phase); err != nil || phase != "failed" {
		t.Fatal("Stale operation remains Preparing", phase, err)
	}
}
