package software

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/books"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/music"
)

func TestIdentifiersAndCPEParsing(t *testing.T) {
	c, ok := ParseCPE23(`cpe:2.3:a:adobe:photoshop:26.0:*:*:*:*:windows:x64:*`)
	if !ok {
		t.Fatal("expected valid CPE 2.3")
	}
	if c.CanonicalID() != "cpe:2.3:a:adobe:photoshop" {
		t.Fatalf("unexpected canonical CPE: %q", c.CanonicalID())
	}
	if plats := PlatformsFromCPE(c, "Adobe Photoshop 26.0 for Windows"); !slices.Equal(plats, []string{"windows"}) {
		t.Fatalf("unexpected platforms: %v", plats)
	}
	if title := CleanCPETitle("Adobe Photoshop 26.0 for Windows", c.Vendor, c.Product); title != "Adobe Photoshop" {
		t.Fatalf("unexpected cleaned CPE title: %q", title)
	}

	// Escaped colons in CPE 2.3 must not shift fields.
	esc, ok := ParseCPE23(`cpe:2.3:a:vendor\:inc:my\:app:1.2.3:*:*:*:*:macos:*:*`)
	if !ok || esc.Vendor != "vendorinc" && esc.Product != "myapp" {
		t.Fatalf("unexpected escaped CPE parse: ok=%v %+v", ok, esc)
	}
	if esc.TargetSW != "macos" {
		t.Fatalf("expected target_sw=macos after escaped colons, got %q", esc.TargetSW)
	}

	// Hardware and web plugins / developer libraries filtering.
	wpPlugin, _ := ParseCPE23(`cpe:2.3:a:yoast:yoast_seo:22.0:*:*:*:*:wordpress:*:*`)
	if !IsPluginOrLibrary(wpPlugin, "Yoast SEO 22.0 for WordPress", nil) {
		t.Fatal("expected WordPress plugin to be classified as plugin")
	}
	wpCore, _ := ParseCPE23(`cpe:2.3:a:wordpress:wordpress:6.6:*:*:*:*:*:*:*`)
	if IsPluginOrLibrary(wpCore, "WordPress 6.6", nil) {
		t.Fatal("expected WordPress core itself NOT to be filtered as plugin")
	}
	npmLib, _ := ParseCPE23(`cpe:2.3:a:lodash:lodash:4.17.21:*:*:*:*:node.js:*:*`)
	if !IsPluginOrLibrary(npmLib, "Lodash 4.17.21 for Node.js", nil) {
		t.Fatal("expected Node.js library to be classified as library")
	}
	fw, _ := ParseCPE23(`cpe:2.3:o:netgear:r7000_firmware:1.0.9:*:*:*:*:*:*:*`)
	if !IsPluginOrLibrary(fw, "Netgear R7000 Firmware 1.0.9", nil) {
		t.Fatal("expected firmware to be excluded by ExcludePlugins")
	}
	osCPE, _ := ParseCPE23(`cpe:2.3:o:canonical:ubuntu_linux:24.04:*:*:*:*:*:*:*`)
	if IsPluginOrLibrary(osCPE, "Canonical Ubuntu Linux 24.04", nil) {
		t.Fatal("expected Ubuntu OS NOT to be filtered as plugin/firmware")
	}
	if plats := PlatformsFromCPE(osCPE, "Canonical Ubuntu Linux 24.04"); !slices.Equal(plats, []string{"linux"}) {
		t.Fatalf("expected linux platform for Ubuntu OS, got %v", plats)
	}

	// Homepage normalization & multi-product host exclusion.
	if got := CanonicalHomepageKey("https://www.videolan.org/vlc/"); got != "host:videolan.org" {
		t.Fatalf("unexpected homepage key: %q", got)
	}
	if got := CanonicalHomepageKey("https://github.com/OBSProject/obs-studio/releases"); got != "repo:github.com/obsproject/obs-studio" {
		t.Fatalf("unexpected repo homepage key: %q", got)
	}
	if got := CanonicalHomepageKey("https://sourceforge.net/projects/sevenzip/"); got != "repo:sourceforge.net/sevenzip" {
		t.Fatalf("unexpected sourceforge homepage key: %q", got)
	}
	if got := CanonicalHomepageKey("https://www.adobe.com/products/photoshop.html"); got != "" {
		t.Fatalf("expected multi-product vendor host adobe.com to return empty key, got %q", got)
	}
	if got := CanonicalHomepageKey("https://github.com/microsoft"); got != "" {
		t.Fatalf("expected org-only github URL to return empty key, got %q", got)
	}

	// Natural version comparison.
	if CompareVersions("1.10.0", "1.9.8") <= 0 {
		t.Fatal("expected 1.10.0 > 1.9.8")
	}
	if CompareVersions("2.0.0", "2.0.0-beta.1") <= 0 {
		t.Fatal("expected stable 2.0.0 > prerelease 2.0.0-beta.1")
	}
	if ValidIconRef("visual-studio-code") != "visual-studio-code" || ValidIconRef("../etc/passwd") != "" {
		t.Fatal("unexpected ValidIconRef validation behavior")
	}
}

func buildTarGz(t *testing.T, files map[string]string) ([]byte, string, string) {
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
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
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
	gzSum := sha256.Sum256(gzBuf.Bytes())
	return gzBuf.Bytes(), hex.EncodeToString(tarSum[:]), hex.EncodeToString(gzSum[:])
}

func TestStreamImportersAndMeta(t *testing.T) {
	metaText := "lastModifiedDate:2026-11-20T05:00:01-05:00\nsize:12345\ngzSize:456\nsha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"
	meta, err := ParseNVDMeta(strings.NewReader(metaText))
	if err != nil {
		t.Fatalf("ParseNVDMeta failed: %v", err)
	}
	if meta.LastModifiedAt == nil || meta.SHA256 != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	nvdChunk := `{
		"resultsPerPage": 4,
		"startIndex": 0,
		"totalResults": 4,
		"products": [
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:a:videolan:vlc_media_player:3.0.20:*:*:*:*:windows:*:*", "lastModified": "2026-11-18T10:00:00.000", "titles": [{"title": "VideoLAN VLC Media Player 3.0.20", "lang": "en"}], "refs": [{"ref": "https://www.videolan.org/vlc/", "type": "Product"}]}},
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:a:videolan:vlc_media_player:3.0.21:*:*:*:*:linux:*:*", "lastModified": "2026-11-19T10:00:00.000", "titles": [{"title": "VideoLAN VLC Media Player 3.0.21", "lang": "en"}]}},
			{"cpe": {"deprecated": true,  "cpeName": "cpe:2.3:a:old:deprecated_app:1.0:*:*:*:*:*:*:*", "lastModified": "2020-01-01T00:00:00.000"}},
			{"cpe": {"deprecated": false, "cpeName": "cpe:2.3:h:acme:router_x1:1.0:*:*:*:*:*:*:*", "lastModified": "2026-11-19T10:00:00.000"}}
		]
	}`
	nvdBytes, tarSHA, _ := buildTarGz(t, map[string]string{
		"nvdcpe-2.0-chunks/nvdcpe-2.0-chunk-00001.json": nvdChunk,
	})
	var nvdEntries []SourceEntry
	nvdStats, err := StreamNVDDump(context.Background(), bytes.NewReader(nvdBytes), tarSHA, 10, func(batch []SourceEntry) error {
		nvdEntries = append(nvdEntries, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamNVDDump failed: %v", err)
	}
	if nvdStats.SkippedDeprecated != 1 || nvdStats.SkippedHardware != 1 || len(nvdEntries) != 1 {
		t.Fatalf("unexpected NVD stats=%+v entries=%+v", nvdStats, nvdEntries)
	}
	if nvdEntries[0].LatestVersion != "3.0.21" || !slices.Equal(nvdEntries[0].Platforms, []string{"linux", "windows"}) {
		t.Fatalf("unexpected aggregated NVD entry: %+v", nvdEntries[0])
	}
	if _, err := StreamNVDDump(context.Background(), bytes.NewReader(nvdBytes), strings.Repeat("a", 64), 10, func([]SourceEntry) error { return nil }); !errors.Is(err, ErrNVDChecksumMismatch) {
		t.Fatalf("expected ErrNVDChecksumMismatch, got %v", err)
	}

	// Real NIST nvdcpe-2.0.meta publishes the SHA-256 of the concatenated
	// uncompressed JSON chunk payloads (including trailing newlines), not the tar
	// wrapper bytes. Verify multi-chunk content SHA-256 and NUL sanitization.
	chunk1 := `{"products":[{"cpe":{"deprecated":false,"cpeName":"cpe:2.3:a:videolan:vlc_media_player:3.0.21:*:*:*:*:linux:*:*","lastModified":"2026-11-19T10:00:00.000","titles":[{"title":"VLC\u0000 Media Player 3.0.21","lang":"en"}]}}]}` + "\n"
	chunk2 := `{"products":[{"cpe":{"deprecated":false,"cpeName":"cpe:2.3:a:blender:blender:4.2.0:*:*:*:*:linux:*:*","lastModified":"2026-11-20T10:00:00.000","titles":[{"title":"Blender 4.2.0","lang":"en"}]}}]}` + "\n"
	contentHash := sha256.Sum256([]byte(chunk1 + chunk2))
	contentSHA := hex.EncodeToString(contentHash[:])
	multiBytes, multiTarSHA, multiGzSHA := buildTarGz(t, map[string]string{
		"nvdcpe-2.0-chunks/nvdcpe-2.0-chunk-00001.json": chunk1,
		"nvdcpe-2.0-chunks/nvdcpe-2.0-chunk-00002.json": chunk2,
	})
	if contentSHA == multiTarSHA || contentSHA == multiGzSHA {
		t.Fatal("expected content SHA-256 to differ from tar and gzip digests")
	}
	var multiEntries []SourceEntry
	multiStats, err := StreamNVDDump(context.Background(), bytes.NewReader(multiBytes), contentSHA, 10, func(batch []SourceEntry) error {
		multiEntries = append(multiEntries, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamNVDDump rejected real NIST content SHA-256: %v", err)
	}
	if multiStats.ContentSHA256 != contentSHA || len(multiEntries) != 2 {
		t.Fatalf("unexpected multi-chunk stats=%+v entries=%+v", multiStats, multiEntries)
	}
	for _, entry := range multiEntries {
		if strings.ContainsRune(entry.Name, 0) {
			t.Fatalf("NUL byte leaked into sanitized software entry: %q", entry.Name)
		}
	}

	// WinGet streaming: keeps only latest version, ignores .installer.yaml and IconUrl.
	wingetBytes, _, _ := buildTarGz(t, map[string]string{
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.20/VideoLAN.VLC.locale.en-US.yaml": "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.20\nPackageLocale: en-US\nPublisher: VideoLAN\nPackageName: VLC media player\nShortDescription: Old description\nManifestType: defaultLocale\n",
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.yaml":              "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nDefaultLocale: en-US\nManifestType: version\n",
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.installer.yaml":    "PackageIdentifier: VideoLAN.VLC\nInstallers: []\nManifestType: installer\n",
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.locale.en-US.yaml": "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nPackageLocale: en-US\nPublisher: VideoLAN\nPackageName: VLC media player\nShortDescription: Free and open source cross-platform multimedia player.\nLicense: GPL-2.0\nPackageUrl: https://www.videolan.org/vlc/\nMonikers:\n  - vlc\nTags:\n  - media\n  - video\nIcons:\n  - IconUrl: https://evil.example/icon.png\nManifestType: defaultLocale\n",
		"winget-pkgs-master/manifests/v/VideoLAN/VLC/3.0.21/VideoLAN.VLC.locale.fr-FR.yaml": "PackageIdentifier: VideoLAN.VLC\nPackageVersion: 3.0.21\nPackageLocale: fr-FR\nPackageName: Lecteur multimédia VLC\nManifestType: locale\n",
	})
	var wingetEntries []SourceEntry
	wgStats, err := StreamWinGetArchive(context.Background(), bytes.NewReader(wingetBytes), 10, func(batch []SourceEntry) error {
		wingetEntries = append(wingetEntries, batch...)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamWinGetArchive failed: %v", err)
	}
	if len(wingetEntries) != 1 || wgStats.PackagesEmitted != 1 {
		t.Fatalf("unexpected winget result: stats=%+v entries=%+v", wgStats, wingetEntries)
	}
	if wingetEntries[0].LatestVersion != "3.0.21" || wingetEntries[0].License != "GPL-2.0" || !slices.Contains(wingetEntries[0].Aliases, "Lecteur multimédia VLC") {
		t.Fatalf("unexpected winget entry: %+v", wingetEntries[0])
	}

	// Homebrew Cask streaming: ignores disabled=true.
	brewJSON := `[
		{"token":"vlc","old_tokens":["vlc-old"],"name":["VLC media player","VLC"],"desc":"Multimedia player","homepage":"https://www.videolan.org/vlc/","version":"3.0.21","languages":["en","fr"],"disabled":false},
		{"token":"dead-app","name":["Dead App"],"desc":"Removed","homepage":"https://dead.example","version":"1.0","disabled":true}
	]`
	var brewEntries []SourceEntry
	brewStats, err := StreamHomebrewCasks(context.Background(), strings.NewReader(brewJSON), 10, func(batch []SourceEntry) error {
		brewEntries = append(brewEntries, batch...)
		return nil
	})
	if err != nil || brewStats.SkippedDisabled != 1 || len(brewEntries) != 1 {
		t.Fatalf("unexpected homebrew result: err=%v stats=%+v entries=%+v", err, brewStats, brewEntries)
	}

	// selfh.st/icons parsing.
	iconsJSON := `[
		{"Name":"VLC","Reference":"vlc","PNG":"Yes"},
		{"Name":"SVG Only","Reference":"svg-only","PNG":"No"},
		{"Name":"Bad Ref","Reference":"../evil","PNG":"Yes"}
	]`
	icons, skippedIcons, err := ParseSelfhstIcons(strings.NewReader(iconsJSON))
	if err != nil || len(icons) != 1 || skippedIcons != 2 || icons[0].Reference != "vlc" {
		t.Fatalf("unexpected icons parse: err=%v skipped=%d icons=%+v", err, skippedIcons, icons)
	}

	// Cross-source merge across WinGet + Homebrew + NVD + selfh.st/icons.
	allEntries := append(append(nvdEntries, wingetEntries...), brewEntries...)
	merged, mStats := MergeCatalog(allEntries, icons, DefaultImportSelection())
	if len(merged) != 1 || mStats.IconsMatched != 1 {
		t.Fatalf("expected 1 merged VLC entry with 1 icon, got len=%d stats=%+v merged=%+v", len(merged), mStats, merged)
	}
	vlc := merged[0]
	if vlc.SoftwareKey != "winget:videolan.vlc" {
		t.Fatalf("unexpected canonical software_key: %q", vlc.SoftwareKey)
	}
	if vlc.Info.WinGetID != "VideoLAN.VLC" || vlc.Info.HomebrewCask != "vlc" || vlc.Info.NVDCPE != "cpe:2.3:a:videolan:vlc_media_player" {
		t.Fatalf("expected all 3 external IDs fused, got %+v", vlc.Info)
	}
	if !slices.Equal(vlc.Info.Platforms, []string{"linux", "macos", "windows"}) {
		t.Fatalf("expected platform union [linux macos windows], got %v", vlc.Info.Platforms)
	}
	if vlc.Info.Description != "Free and open source cross-platform multimedia player." {
		t.Fatalf("expected WinGet description priority, got %q", vlc.Info.Description)
	}
	if vlc.Info.IconRef != "vlc" || !vlc.Info.HasIcon {
		t.Fatalf("expected selfh.st icon ref 'vlc', got %+v", vlc.Info)
	}
}

func TestMergeCatalogFiltersAndNoFalseMerges(t *testing.T) {
	entries := []SourceEntry{
		{
			Source: "nvd", ExternalID: "cpe:2.3:a:adobe:photoshop", Part: "a",
			Name: "Adobe Photoshop", Vendor: "Adobe", VendorNorm: "adobe", ProductNorm: "photoshop",
			Platforms: []string{"windows", "macos"},
		},
		{
			Source: "nvd", ExternalID: "cpe:2.3:a:adobe:acrobat", Part: "a",
			Name: "Adobe Acrobat", Vendor: "Adobe", VendorNorm: "adobe", ProductNorm: "acrobat",
			Platforms: []string{"windows"},
		},
		{
			Source: "nvd", ExternalID: "cpe:2.3:a:yoast:yoast_seo", Part: "a",
			Name: "Yoast SEO", Vendor: "Yoast", VendorNorm: "yoast", ProductNorm: "yoast seo",
			IsPlugin: true,
		},
		{
			Source: "nvd", ExternalID: "cpe:2.3:o:canonical:ubuntu_linux", Part: "o",
			Name: "Ubuntu Linux", Vendor: "Canonical", VendorNorm: "canonical", ProductNorm: "ubuntu linux",
			Platforms: []string{"linux"},
		},
	}

	sel := DefaultImportSelection()
	merged, stats := MergeCatalog(entries, nil, sel)
	if len(merged) != 2 || stats.Filtered != 2 {
		t.Fatalf("expected 2 entries (Photoshop + Acrobat) and 2 filtered (plugin + OS), got len=%d stats=%+v", len(merged), stats)
	}

	sel.IncludeOS = true
	sel.Platforms = []string{"linux"}
	mergedLinux, _ := MergeCatalog(entries, nil, sel)
	if len(mergedLinux) != 1 || mergedLinux[0].Name != "Ubuntu Linux" {
		t.Fatalf("expected only Ubuntu Linux when IncludeOS=true and Platforms=[linux], got %+v", mergedLinux)
	}

	sel.IncludeNVD = false
	mergedNoNVD, _ := MergeCatalog(entries, nil, sel)
	if len(mergedNoNVD) != 0 {
		t.Fatalf("expected 0 entries when IncludeNVD=false, got %+v", mergedNoNVD)
	}
}

func TestSoftwareCatalogDistinguishesProductsOnTheSameWebsite(t *testing.T) {
	entries := []SourceEntry{
		{Source: "winget", ExternalID: "Serif.AffinityDesigner", Name: "Affinity Designer", VendorNorm: "serif", ProductNorm: "affinity designer", HomepageKey: "host:affinity.serif.com"},
		{Source: "winget", ExternalID: "Serif.AffinityPhoto", Name: "Affinity Photo", VendorNorm: "serif", ProductNorm: "affinity photo", HomepageKey: "host:affinity.serif.com"},
		{Source: "nvd", ExternalID: "cpe:2.3:a:serif:affinity_photo", Name: "Affinity Photo", VendorNorm: "serif", ProductNorm: "affinity photo", HomepageKey: "host:affinity.serif.com"},
	}
	works, _ := MergeCatalog(entries, nil, DefaultImportSelection())
	if len(works) != 2 || works[0].Info.NVDCPE != "" || works[1].Info.NVDCPE != entries[2].ExternalID {
		t.Fatalf("the wrong product absorbed the NVD identifier: %+v", works)
	}
}

func TestNVDRejectsIncompletePagination(t *testing.T) {
	for _, body := range []string{
		`{"resultsPerPage":0,"totalResults":0,"products":[]}`,
		`{"resultsPerPage":0,"startIndex":0,"totalResults":0}`,
		`{"resultsPerPage":10,"startIndex":0,"totalResults":10,"products":[]}`,
		`{"resultsPerPage":10,"startIndex":11,"totalResults":10,"products":[]}`,
		`{"resultsPerPage":0,"startIndex":0,"totalResults":0,"products":[]} trailing`,
	} {
		if _, err := ParseNVDAPIResponse(strings.NewReader(body)); err == nil {
			t.Fatalf("accepted invalid paginated response: %s", body)
		}
	}
	const lastPage = `{"resultsPerPage":2,"startIndex":2,"totalResults":3,"products":[{"cpe":{"cpeName":"cpe:2.3:h:example:device:*:*:*:*:*:*:*:*"}}]}`
	page, err := ParseNVDAPIResponse(strings.NewReader(lastPage))
	if err != nil || page.ProductCount != 1 || len(page.Entries) != 0 || page.SkippedHardware != 1 {
		t.Fatalf("pagination should count even filtered source records: %+v, %v", page, err)
	}
	const deprecatedPage = `{"resultsPerPage":1,"startIndex":0,"totalResults":1,"products":[{"cpe":{"deprecated":true,"cpeName":"cpe:2.3:a:example:app:1.2:*:*:*:*:*:*:*","lastModified":"2026-11-20T12:00:00.000"}}]}`
	page, err = ParseNVDAPIResponse(strings.NewReader(deprecatedPage))
	if err != nil || page.LatestDeprecation == nil || page.LatestDeprecation.Format(time.RFC3339) != "2026-11-20T12:00:00Z" || len(page.Entries) != 0 {
		t.Fatalf("deprecation must be distinguishable from a page of filtered records: %+v, %v", page, err)
	}
}

func TestParseRecordAndAntiCollision(t *testing.T) {
	// Year-bearing software must be captured as software and extract canonical title + variant.
	ps := ParseRecord(model.AIORecord{Title: "Adobe.Photoshop.2025.v26.0.Multilingual.x64"})
	if ps.Kind != "pc" || ps.Title != "Adobe Photoshop" || ps.Version != "26.0" || ps.Architecture != "x64" {
		t.Fatalf("unexpected Adobe Photoshop parse: %+v", ps)
	}
	if !slices.Contains(ps.Variants, "Adobe Photoshop 2025") || !slices.Contains(ps.Tags, "Multilingual") {
		t.Fatalf("expected 2025 variant and Multilingual tag: %+v", ps)
	}

	// Audio plugin with AAX must NOT be captured by Books or Music, and MUST be captured by Software.
	pluginRec := model.AIORecord{Title: "FabFilter.Pro-Q.3.v3.24.VST3.AU.AAX.macOS"}
	if b := books.ParseRecord(pluginRec); b.Kind != "" {
		t.Fatalf("books parser falsely captured audio plugin: %+v", b)
	}
	if m := music.ParseRecord(pluginRec); m.Kind != "" {
		t.Fatalf("music parser falsely captured audio plugin: %+v", m)
	}
	plug := ParseRecord(pluginRec)
	if plug.Kind != "mac" || plug.Title != "FabFilter Pro-Q 3" || plug.Version != "3.24" || !slices.Equal(plug.Platforms, []string{"macos"}) {
		t.Fatalf("unexpected audio plugin parse: %+v", plug)
	}
	if !slices.Contains(plug.Tags, "VST3") || !slices.Contains(plug.Tags, "AU") || !slices.Contains(plug.Tags, "AAX") {
		t.Fatalf("expected VST3/AU/AAX tags: %+v", plug.Tags)
	}

	// Portable Windows app.
	vlc := ParseRecord(model.AIORecord{Title: "VLC.Media.Player.3.0.21.Win64.Portable"})
	if vlc.Kind != "pc" || vlc.Title != "VLC Media Player" || vlc.Version != "3.0.21" || !slices.Contains(vlc.Tags, "Portable") {
		t.Fatalf("unexpected VLC parse: %+v", vlc)
	}

	// OS ISO image.
	ubuntu := ParseRecord(model.AIORecord{
		Title:         "Ubuntu.24.04.1.LTS.Desktop.amd64.ISO",
		SoftwareHints: model.SoftwareHints{Categories: []int{4020}},
	})
	if ubuntu.Kind != "iso" || ubuntu.Title != "Ubuntu" || ubuntu.Version != "24.04.1" || !slices.Equal(ubuntu.Platforms, []string{"linux"}) {
		t.Fatalf("unexpected Ubuntu ISO parse: %+v", ubuntu)
	}

	// Android APK.
	apk := ParseRecord(model.AIORecord{Title: "Spotify.v8.9.84.ARM64.APK"})
	if apk.Kind != "android" || apk.Title != "Spotify" || apk.Architecture != "arm64" || !slices.Equal(apk.Platforms, []string{"android"}) {
		t.Fatalf("unexpected APK parse: %+v", apk)
	}

	// Anti-collision rejections: PC Games (4050 + scene groups), Movies, Books, Music, bare titles.
	rejected := []model.AIORecord{
		{Title: "Cyberpunk.2077.v2.1.x64-FitGirl"},
		{Title: "Elden.Ring.v1.12.x64-RUNE"},
		{Title: "Half.Life.2.Win64", SoftwareHints: model.SoftwareHints{Categories: []int{4050}}},
		{Title: "Matrix.2025.1080p.WEB-DL.x264"},
		{Title: "Dune.2021.EPUB"},
		{Title: "Random Access Memories [2013] [FLAC]"},
		{Title: "The Matrix (1999)"},
		{Title: "Project Hail Mary [AAX]"},
	}
	for _, rec := range rejected {
		if got := ParseRecord(rec); got.Kind != "" {
			t.Errorf("expected software parser to reject %q, got %+v", rec.Title, got)
		}
	}

	// Ensure Music also ignores software categories 4000..4070.
	if m := music.ParseRecord(model.AIORecord{
		Title:      "Audacity.3.6.0.Win64",
		MusicHints: model.MusicHints{Categories: []int{4010}},
	}); m.Kind != "" {
		t.Fatalf("music parser should ignore category 4010, got %+v", m)
	}
}
