package aiosource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func buildSoftwareTarGz(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		body := []byte(files[name])
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	tarSum := sha256.Sum256(tarBuf.Bytes())

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(tarBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes(), hex.EncodeToString(tarSum[:])
}

func TestTrustedSoftwareExportRedirectValidation(t *testing.T) {
	for _, allowed := range []string{
		"https://nvd.nist.gov/feeds/json/cpe/2.0/nvdcpe-2.0.tar.gz",
		"https://services.nvd.nist.gov/rest/json/cpes/2.0?startIndex=0",
		"https://codeload.github.com/microsoft/winget-pkgs/tar.gz/refs/heads/master",
		"https://raw.githubusercontent.com/selfhst/icons/main/index.json",
		"https://formulae.brew.sh/api/cask.json",
	} {
		u, _ := url.Parse(allowed)
		if err := trustedSoftwareExport(&http.Request{URL: u}, nil); err != nil {
			t.Fatalf("trusted URL rejected %q: %v", allowed, err)
		}
	}
	for _, blocked := range []string{
		"http://nvd.nist.gov/feeds/json/cpe/2.0/nvdcpe-2.0.tar.gz",
		"https://evil.example/nvdcpe-2.0.tar.gz",
		"https://nvd.nist.gov:8443/feeds/json/cpe/2.0/nvdcpe-2.0.meta",
		"https://user:pass@formulae.brew.sh/api/cask.json",
		"https://127.0.0.1/index.json",
		"https://github.com/microsoft/winget-pkgs",
	} {
		u, _ := url.Parse(blocked)
		if err := trustedSoftwareExport(&http.Request{URL: u}, nil); err == nil {
			t.Fatalf("untrusted URL accepted %q", blocked)
		}
	}
}

func TestSoftwareFullSyncIncrementalLongPauseFallbackAndReset(t *testing.T) {
	ctx, s, conn := testService(t)
	now := time.Now().UTC()

	nvdChunk := `{
		"resultsPerPage": 3,
		"startIndex": 0,
		"totalResults": 3,
		"products": [
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:a:videolan:vlc_media_player:3.0.21:*:*:*:*:linux:*:*", "lastModified": "2026-11-19T10:00:00.000", "titles": [{"title": "VideoLAN VLC Media Player 3.0.21", "lang": "en"}], "refs": [{"ref": "https://www.videolan.org/vlc/", "type": "Product"}]}},
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:a:yoast:yoast_seo:22.0:*:*:*:*:wordpress:*:*", "lastModified": "2026-11-19T10:00:00.000", "titles": [{"title": "Yoast SEO 22.0 for WordPress", "lang": "en"}]}},
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:o:canonical:ubuntu_linux:24.04:*:*:*:*:*:*:*", "lastModified": "2026-11-19T10:00:00.000", "titles": [{"title": "Canonical Ubuntu Linux 24.04", "lang": "en"}]}}
		]
	}`
	nvdDumpBytes, nvdTarSHA := buildSoftwareTarGz(t, map[string]string{
		"nvdcpe-2.0-chunks/nvdcpe-2.0-chunk-00001.json": nvdChunk,
	})
	wingetBytes, _ := buildSoftwareTarGz(t, map[string]string{
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.yaml":              "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nDefaultLocale: en-US\nManifestType: version\n",
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.locale.en-US.yaml": "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nPackageLocale: en-US\nPublisher: VideoLAN\nPackageName: VLC media player\nShortDescription: Free and open source multimedia player.\nLicense: GPL-2.0\nPackageUrl: https://www.videolan.org/vlc/\nMonikers:\n  - vlc\nManifestType: defaultLocale\n",
	})
	brewJSON := `[{"token":"vlc","name":["VLC media player","VLC"],"desc":"Multimedia player","homepage":"https://www.videolan.org/vlc/","version":"3.0.21","languages":["en","fr"],"disabled":false}]`
	iconsJSON := `[{"Name":"VLC","Reference":"vlc","PNG":"Yes"},{"Name":"SVG Only","Reference":"svg-only","PNG":"No"}]`
	nvdIncrementalJSON := `{
		"resultsPerPage": 1,
		"startIndex": 0,
		"totalResults": 1,
		"products": [
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:a:adobe:photoshop:26.0:*:*:*:*:windows:*:*", "lastModified": "2026-11-20T12:00:00.000", "titles": [{"title": "Adobe Photoshop 26.0", "lang": "en"}]}}
		]
	}`

	var nvdMetaRev atomic.Int32
	var nvdDumpCalls atomic.Int32
	var nvdAPICalls atomic.Int32
	var sawAPIKey atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nvd/nvdcpe-2.0.meta":
			sha := nvdTarSHA
			ts := now.Add(-2 * time.Hour).Format(time.RFC3339)
			if nvdMetaRev.Load() >= 1 {
				ts = now.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "lastModifiedDate:%s\nsize:%d\nsha256:%s\n", ts, len(nvdDumpBytes), sha)
		case "/nvd/nvdcpe-2.0.tar.gz":
			nvdDumpCalls.Add(1)
			_, _ = w.Write(nvdDumpBytes)
		case "/nvd/api":
			nvdAPICalls.Add(1)
			if r.Header.Get("apiKey") == "synthetic-nvd-key" {
				sawAPIKey.Store(true)
			}
			_, _ = w.Write([]byte(nvdIncrementalJSON))
		case "/winget.tar.gz":
			if r.Header.Get("If-None-Match") == `"wg-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"wg-v1"`)
			_, _ = w.Write(wingetBytes)
		case "/cask.json":
			if r.Header.Get("If-None-Match") == `"brew-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"brew-v1"`)
			_, _ = w.Write([]byte(brewJSON))
		case "/icons.json":
			if r.Header.Get("If-None-Match") == `"icons-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"icons-v1"`)
			_, _ = w.Write([]byte(iconsJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	s.SoftwareNVDMetaURL = upstream.URL + "/nvd/nvdcpe-2.0.meta"
	s.SoftwareNVDDumpURL = upstream.URL + "/nvd/nvdcpe-2.0.tar.gz"
	s.SoftwareNVDAPIBase = upstream.URL + "/nvd/api"
	s.SoftwareWinGetURL = upstream.URL + "/winget.tar.gz"
	s.SoftwareHomebrewURL = upstream.URL + "/cask.json"
	s.SoftwareIconsURL = upstream.URL + "/icons.json"
	s.SoftwareHTTP = upstream.Client()

	if err := s.Vault.Put(ctx, "software_nvd_key", "synthetic-nvd-key"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.SoftwareNVDSecret = "software_nvd_key"
	c.SoftwareIncludeNVD = true
	c.SoftwareExcludePlugins = true
	c.SoftwareIncludeOS = false
	c.SoftwareIncludeIcons = true
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	// Secret deletion protection must reject deleting an active NVD secret.
	if err := s.DB.DeleteSecret(ctx, "software_nvd_key"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("configured NVD secret could be deleted: %v", err)
	}

	// 1. Initial sync downloads NVD dump + WinGet + Homebrew + selfh.st/icons and merges VLC.
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("initial software sync failed: %v", err)
	}
	st, err := s.DB.SoftwareStatus(ctx)
	if err != nil || !st.Complete || st.Works != 1 || st.Icons != 1 || st.IconsIndexed != 1 || st.WinGetEntries != 1 || st.HomebrewEntries != 1 {
		t.Fatalf("unexpected software status after initial sync: %+v %v", st, err)
	}

	// 2. Incremental NVD sync (meta timestamp advances, WinGet/Homebrew/Icons return 304) sends apiKey header and adds Photoshop.
	nvdMetaRev.Store(1)
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("incremental software sync failed: %v", err)
	}
	if !sawAPIKey.Load() || nvdAPICalls.Load() != 1 {
		t.Fatalf("expected NVD incremental API call with apiKey header, calls=%d sawKey=%v", nvdAPICalls.Load(), sawAPIKey.Load())
	}
	st, err = s.DB.SoftwareStatus(ctx)
	if err != nil || !st.Complete || st.Works != 2 {
		t.Fatalf("expected 2 software works (VLC + Photoshop) after incremental sync, got %+v %v", st, err)
	}

	// 3. Long pause (> 110 days, e.g. 125 days) automatically falls back to full NVD snapshot while keeping catalog valid.
	if _, err = conn.Exec(ctx, `UPDATE aiosources.sw_sync_state SET nvd_changes_through=$1 WHERE singleton`, now.Add(-125*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	dumpsBefore := nvdDumpCalls.Load()
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("long-pause fallback sync failed: %v", err)
	}
	if nvdDumpCalls.Load() <= dumpsBefore {
		t.Fatal("expected long pause (>110 days) to trigger automatic full NVD dump fallback")
	}
	st, _ = s.DB.SoftwareStatus(ctx)
	if st.Works != 1 {
		t.Fatalf("expected 1 work (VLC) right after full NVD dump pruned stale entries, got %+v", st)
	}

	// 4. Enabling IncludeOS re-projects Ubuntu Linux locally and applies incremental NVD updates.
	c, _ = s.DB.AIOConfig(ctx)
	c.SoftwareIncludeOS = true
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("software re-projection after filter change failed: %v", err)
	}
	st, _ = s.DB.SoftwareStatus(ctx)
	if st.Works != 3 {
		t.Fatalf("expected 3 works (VLC + Ubuntu Linux + incremental Photoshop) with IncludeOS=true, got %+v", st)
	}

	// 5. Isolated software reset preserves unrelated game works.
	if _, err = conn.Exec(ctx, `INSERT INTO aiosources.aio_works(media_type,igdb_id,display_title,years) VALUES('game','777','Keep Game','{2024}')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(ctx, "software"); err != nil {
		t.Fatalf("software reset failed: %v", err)
	}
	var swWorks, gameWorks int
	if err = conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM aiosources.aio_works WHERE media_type='software'),(SELECT count(*) FROM aiosources.aio_works WHERE media_type='game')`).Scan(&swWorks, &gameWorks); err != nil || swWorks != 0 || gameWorks != 1 {
		t.Fatalf("software reset affected wrong works: sw=%d game=%d err=%v", swWorks, gameWorks, err)
	}
}

func TestSoftwareOperationHistoryDiagnosticsUpToDateAndFailures(t *testing.T) {
	ctx, s, conn := testService(t)
	now := time.Now().UTC()

	nvdChunk := `{"resultsPerPage":1,"startIndex":0,"totalResults":1,"products":[{"cpe":{"deprecated":false,"cpeName":"cpe:2.3:a:videolan:vlc_media_player:3.0.21:*:*:*:*:linux:*:*","lastModified":"2026-11-19T10:00:00.000","titles":[{"title":"VideoLAN VLC Media Player 3.0.21","lang":"en"}]}}]}` + "\n"
	contentSum := sha256.Sum256([]byte(nvdChunk))
	nvdContentSHA := hex.EncodeToString(contentSum[:])
	nvdDumpBytes, _ := buildSoftwareTarGz(t, map[string]string{
		"nvdcpe-2.0-chunks/nvdcpe-2.0-chunk-00001.json": nvdChunk,
	})
	wingetBytes, _ := buildSoftwareTarGz(t, map[string]string{
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.locale.en-US.yaml": "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nPackageLocale: en-US\nPublisher: VideoLAN\nPackageName: VLC media player\nManifestType: defaultLocale\n",
	})
	brewJSON := `[{"token":"vlc","name":["VLC media player"],"desc":"Multimedia player","homepage":"https://www.videolan.org/vlc/","version":"3.0.21","disabled":false}]`
	iconsJSON := `[{"Name":"VLC","Reference":"vlc","PNG":"Yes"}]`

	// mode 0 = initial full sync with NIST content SHA-256
	// mode 1 = up-to-date sync (incremental NVD returns 0 changes, others return 304)
	// mode 2 = NVD API key rejected (HTTP 403)
	// mode 3 = NVD checksum mismatch on fresh dump
	var mode atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := mode.Load()
		switch r.URL.Path {
		case "/nvd/meta":
			sha := nvdContentSHA
			if m == 3 {
				sha = strings.Repeat("b", 64)
			}
			ts := now.Add(-1 * time.Hour).Format(time.RFC3339)
			if m >= 1 {
				ts = now.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "lastModifiedDate:%s\nsize:%d\nsha256:%s\n", ts, len(nvdChunk), sha)
		case "/nvd/dump":
			_, _ = w.Write(nvdDumpBytes)
		case "/nvd/api":
			if m == 2 {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "secret-nvd-key-must-never-leak")
				return
			}
			fmt.Fprint(w, `{"resultsPerPage":0,"startIndex":0,"totalResults":0,"products":[]}`)
		case "/winget":
			if m >= 1 && r.Header.Get("If-None-Match") == `"wg-etag"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"wg-etag"`)
			_, _ = w.Write(wingetBytes)
		case "/brew":
			if m >= 1 && r.Header.Get("If-None-Match") == `"brew-etag"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"brew-etag"`)
			_, _ = w.Write([]byte(brewJSON))
		case "/icons":
			if m >= 1 && r.Header.Get("If-None-Match") == `"icons-etag"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"icons-etag"`)
			_, _ = w.Write([]byte(iconsJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	s.SoftwareNVDMetaURL = upstream.URL + "/nvd/meta"
	s.SoftwareNVDDumpURL = upstream.URL + "/nvd/dump"
	s.SoftwareNVDAPIBase = upstream.URL + "/nvd/api"
	s.SoftwareWinGetURL = upstream.URL + "/winget"
	s.SoftwareHomebrewURL = upstream.URL + "/brew"
	s.SoftwareIconsURL = upstream.URL + "/icons"
	s.SoftwareHTTP = upstream.Client()

	if err := s.Vault.Put(ctx, "sw_history_key", "secret-nvd-key-must-never-leak"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.DB.AIOConfig(ctx)
	c.SoftwareNVDSecret = "sw_history_key"
	c.SoftwareEnabled = true
	if err := s.SaveConfig(ctx, c); err != nil {
		t.Fatal(err)
	}

	// 1. Initial sync verifies real NIST content SHA-256 and records clean results & provider calls.
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("initial software sync failed: %v", err)
	}
	runs, err := s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata", Service: "software"})
	if err != nil || runs.Total != 1 {
		t.Fatalf("expected 1 software operation, got %+v %v", runs, err)
	}
	initial := runs.Items[0]
	if initial.Status != "succeeded" || initial.Phase != "completed" || initial.Processed == 0 ||
		initial.Results["nvd_checked"] != 1 || initial.Results["full_import"] != 1 || initial.Results["sources_synced"] != 4 {
		t.Fatalf("unexpected initial software history run: %+v", initial)
	}
	if _, legacy := initial.Results["snapshot_checked"]; legacy {
		t.Fatalf("unexpected snapshot_checked counter on software run: %+v", initial.Results)
	}
	initialEvents, err := s.DB.OperationEvents(ctx, initial.ID, model.ListOptions{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	seenHTTP := map[string]int{}
	for _, ev := range initialEvents.Items {
		if ev.HTTPStatus > 0 {
			seenHTTP[ev.Phase] = ev.HTTPStatus
		}
	}
	for _, wantPhase := range []string{"software_nvd_meta", "software_nvd_dump", "software_winget", "software_homebrew", "software_icons"} {
		if seenHTTP[wantPhase] != http.StatusOK {
			t.Fatalf("missing HTTP 200 provider event for %s: %+v", wantPhase, seenHTTP)
		}
	}
	stAfterInitial, err := s.DB.SoftwareStatus(ctx)
	if err != nil || stAfterInitial.Position == 0 || stAfterInitial.Phase != "idle" {
		t.Fatalf("unexpected status after initial sync: %+v %v", stAfterInitial, err)
	}

	// 2. Up-to-date sync (0 incremental changes, 304 on other sources) preserves status counters,
	// resets phase to idle, persists nvd_changes_through, and omits false nvd_incremental result.
	mode.Store(1)
	if err := s.SyncSoftware(ctx, "manual"); err != nil {
		t.Fatalf("up-to-date software sync failed: %v", err)
	}
	stAfterUpToDate, err := s.DB.SoftwareStatus(ctx)
	if err != nil || stAfterUpToDate.Phase != "idle" || stAfterUpToDate.Position != stAfterInitial.Position {
		t.Fatalf("up-to-date sync corrupted status: before=%+v after=%+v err=%v", stAfterInitial, stAfterUpToDate, err)
	}
	if stAfterUpToDate.NVDChangesThrough == nil || !stAfterUpToDate.NVDChangesThrough.After(*stAfterInitial.NVDChangesThrough) {
		t.Fatalf("up-to-date sync dropped updated nvd_changes_through: before=%v after=%v", stAfterInitial.NVDChangesThrough, stAfterUpToDate.NVDChangesThrough)
	}
	runs, err = s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata", Service: "software"})
	if err != nil || runs.Total != 2 {
		t.Fatalf("expected 2 software operations, got %+v %v", runs, err)
	}
	upToDate := runs.Items[0]
	if upToDate.Status != "succeeded" || upToDate.Phase != "up_to_date" || upToDate.Results["nvd_checked"] != 1 {
		t.Fatalf("unexpected up-to-date operation: %+v", upToDate)
	}
	if _, applied := upToDate.Results["nvd_incremental"]; applied {
		t.Fatalf("zero-change incremental check should not report nvd_incremental: %+v", upToDate.Results)
	}

	// 3. Rejected NVD API key persists safe diagnostic error + authentication event code without leaking secret.
	mode.Store(2)
	if _, err = conn.Exec(ctx, `UPDATE aiosources.sw_sync_state SET nvd_changes_through=$1 WHERE singleton`, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncSoftware(ctx, "manual"); err == nil {
		t.Fatal("expected HTTP 403 NVD API key rejection to fail")
	}
	runs, err = s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata", Service: "software"})
	if err != nil || runs.Total != 3 {
		t.Fatalf("expected 3 software operations, got %+v %v", runs, err)
	}
	failedAuth := runs.Items[0]
	if failedAuth.Status != "failed" || !strings.Contains(failedAuth.Error, "rejected the configured API key") {
		t.Fatalf("expected safe specific failure diagnostic in history, got: %+v", failedAuth)
	}
	encoded, _ := json.Marshal(failedAuth)
	if strings.Contains(string(encoded), "secret-nvd-key-must-never-leak") {
		t.Fatal("API key leaked into operation history")
	}
	authEvents, err := s.DB.OperationEvents(ctx, failedAuth.ID, model.ListOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	foundAuthErr := false
	for _, ev := range authEvents.Items {
		if ev.Phase == "software_nvd_incremental" && ev.HTTPStatus == http.StatusForbidden && ev.ErrorCode == "authentication" && ev.Errors == 1 {
			foundAuthErr = true
		}
	}
	if !foundAuthErr {
		t.Fatalf("expected authentication error event for HTTP 403, got: %+v", authEvents.Items)
	}

	// 4. Automatic run during RetryAt cooldown must not record an empty succeeded operation.
	if err := s.SyncSoftware(ctx, "automatic"); err != nil {
		t.Fatalf("automatic cooldown check returned error: %v", err)
	}
	runs, err = s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata", Service: "software"})
	if err != nil || runs.Total != 3 {
		t.Fatalf("automatic cooldown created unexpected history entry: total=%d", runs.Total)
	}

	// 5. Corrupted NVD dump checksum records safe integrity error in history and logs.
	mode.Store(3)
	if _, err = conn.Exec(ctx, `UPDATE aiosources.sw_sync_state SET needs_full_import=TRUE,retry_at=NULL WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncSoftware(ctx, "manual"); err == nil {
		t.Fatal("expected NVD checksum mismatch to fail")
	}
	runs, err = s.DB.ListOperations(ctx, model.OperationFilter{Kind: "metadata", Service: "software"})
	if err != nil || runs.Total != 4 {
		t.Fatalf("expected 4 software operations, got %+v %v", runs, err)
	}
	if !strings.Contains(runs.Items[0].Error, "integrity verification failed") {
		t.Fatalf("expected safe integrity error message, got: %+v", runs.Items[0])
	}
}
