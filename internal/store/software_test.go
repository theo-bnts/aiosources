package store

import (
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/software"
)

func TestSoftwareSelectionKeyChangesOnlyOnMeaningfulFilterChanges(t *testing.T) {
	base := model.AIOConfig{
		SoftwareIncludeNVD:     true,
		SoftwareExcludePlugins: true,
		SoftwareIncludeOS:      false,
		SoftwareIncludeIcons:   true,
		SoftwarePlatforms:      []string{"windows", "macos", "linux", "android", "ios"},
	}
	reordered := base
	reordered.SoftwarePlatforms = []string{"iOS", "LINUX", "windows", "Android", "macOS"}
	if SoftwareSelectionKey(base) != SoftwareSelectionKey(reordered) {
		t.Fatalf("reordered platforms produced different keys: %q vs %q", SoftwareSelectionKey(base), SoftwareSelectionKey(reordered))
	}

	noIcons := base
	noIcons.SoftwareIncludeIcons = false
	if SoftwareSelectionKey(base) == SoftwareSelectionKey(noIcons) {
		t.Fatal("toggling icons index kept the same selection key")
	}

	withOS := base
	withOS.SoftwareIncludeOS = true
	if SoftwareSelectionKey(base) == SoftwareSelectionKey(withOS) {
		t.Fatal("toggling operating systems kept the same selection key")
	}

	noNVD := base
	noNVD.SoftwareIncludeNVD = false
	if SoftwareSelectionKey(base) == SoftwareSelectionKey(noNVD) {
		t.Fatal("toggling NVD CPE catalog kept the same selection key")
	}
}

func TestSoftwareStagingMergeProjectionAndCandidateSearch(t *testing.T) {
	ctx, db := sharingDatabase(t)
	cfg, err := db.AIOConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sel := SoftwareSelection(cfg)
	if err = db.PrepareSoftwareImport(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	gen, err := db.BeginSoftwareSync(ctx, sel.Key(), "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	entries := []software.SourceEntry{
		{
			Source:        "winget",
			ExternalID:    "VideoLAN.VLC",
			Part:          "a",
			Name:          "VLC media player",
			Vendor:        "VideoLAN",
			VendorNorm:    "videolan",
			ProductNorm:   "vlc media player",
			Description:   "Free and open source multimedia player.",
			License:       "GPL-2.0",
			LatestVersion: "3.0.21",
			HomepageKey:   "host:videolan.org",
			Platforms:     []string{"windows"},
			Tags:          []string{"media", "video"},
			Aliases:       []string{"vlc"},
		},
		{
			Source:        "homebrew",
			ExternalID:    "vlc",
			Part:          "a",
			Name:          "VLC media player",
			ProductNorm:   "vlc media player",
			Description:   "Multimedia player",
			LatestVersion: "3.0.21",
			HomepageKey:   "host:videolan.org",
			Platforms:     []string{"macos"},
			Aliases:       []string{"VLC"},
		},
		{
			Source:         "nvd",
			ExternalID:     "cpe:2.3:a:videolan:vlc_media_player",
			Part:           "a",
			Name:           "VideoLAN VLC Media Player",
			Vendor:         "VideoLAN",
			VendorNorm:     "videolan",
			ProductNorm:    "vlc media player",
			LatestVersion:  "3.0.21",
			HomepageKey:    "host:videolan.org",
			Platforms:      []string{"linux", "windows"},
			LastModifiedAt: &now,
		},
	}
	if err = db.SaveSoftwareSourceBatch(ctx, gen, entries, int64(len(entries)), 0, false); err != nil {
		t.Fatal(err)
	}
	icons := []software.IconEntry{
		{Reference: "vlc", Name: "VLC", Normalized: "vlc"},
	}
	if err = db.SaveSoftwareIcons(ctx, gen, icons, 0); err != nil {
		t.Fatal(err)
	}
	loadedEntries, loadedIcons, err := db.LoadSoftwareStaging(ctx, sel.IncludeNVD, sel.IncludeIcons)
	if err != nil {
		t.Fatal(err)
	}
	merged, stats := software.MergeCatalog(loadedEntries, loadedIcons, sel)
	if len(merged) != 1 || stats.IconsMatched != 1 {
		t.Fatalf("unexpected merge output: len=%d stats=%+v", len(merged), stats)
	}
	if err = db.ProjectSoftwareCatalog(ctx, gen, merged, stats.Filtered); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishSoftwareSync(ctx, gen, SoftwareSyncMeta{
		NVDSnapshot:       "2026-11-20T05:00:00Z",
		NVDSnapshotAt:     &now,
		NVDChangesThrough: &now,
		WinGetSyncedAt:    &now,
		HomebrewSyncedAt:  &now,
		IconsSyncedAt:     &now,
		FullImport:        true,
	}); err != nil {
		t.Fatal(err)
	}

	st, err := db.SoftwareStatus(ctx)
	if err != nil || !st.Complete || st.Works != 1 || st.NVDEntries != 1 || st.WinGetEntries != 1 || st.HomebrewEntries != 1 || st.Icons != 1 || st.IconsIndexed != 1 {
		t.Fatalf("unexpected software status: %+v err=%v", st, err)
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	works, overflow, err := searchAIOCandidates(ctx, tx, model.AIOCandidateFilter{
		Type:      "software",
		Title:     "vlc media player",
		ExactOnly: true,
	})
	if err != nil || overflow || len(works) != 1 || works[0].SoftwareKey != "winget:videolan.vlc" || works[0].PosterPath != "vlc" {
		t.Fatalf("software candidate search failed: works=%+v overflow=%v err=%v", works, overflow, err)
	}
	byCPE, _, err := searchAIOCandidates(ctx, tx, model.AIOCandidateFilter{
		Type:   "software",
		NVDCPE: "cpe:2.3:a:videolan:vlc_media_player",
	})
	if err != nil || len(byCPE) != 1 || byCPE[0].SoftwareKey != "winget:videolan.vlc" {
		t.Fatalf("software CPE candidate search failed: works=%+v err=%v", byCPE, err)
	}
}
