package aiosource

import (
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSoftwareIdentificationMatchesAndPreventsCollisions(t *testing.T) {
	photoshop := model.AIOWork{
		ID:           100,
		Type:         "software",
		SoftwareKey:  "cpe:a:adobe:photoshop",
		DisplayTitle: "Adobe Photoshop",
		Titles:       []string{"Adobe Photoshop", "Photoshop"},
		Software: model.SoftwareInformation{
			Vendor:    "Adobe",
			Platforms: []string{"windows", "macos"},
			NVDCPE:    "cpe:2.3:a:adobe:photoshop",
		},
	}
	vlc := model.AIOWork{
		ID:           101,
		Type:         "software",
		SoftwareKey:  "winget:videolan.vlc",
		DisplayTitle: "VLC media player",
		Titles:       []string{"VLC media player", "VLC"},
		Software: model.SoftwareInformation{
			Vendor:       "VideoLAN",
			Platforms:    []string{"windows", "macos", "linux"},
			WinGetID:     "VideoLAN.VLC",
			HomebrewCask: "vlc",
			NVDCPE:       "cpe:2.3:a:videolan:vlc_media_player",
			IconRef:      "vlc",
			HasIcon:      true,
		},
	}
	proQ3 := model.AIOWork{
		ID:           102,
		Type:         "software",
		SoftwareKey:  "cpe:a:fabfilter:pro_q_3",
		DisplayTitle: "FabFilter Pro-Q 3",
		Titles:       []string{"FabFilter Pro-Q 3"},
		Software: model.SoftwareInformation{
			Vendor:    "FabFilter",
			Platforms: []string{"windows", "macos"},
			NVDCPE:    "cpe:2.3:a:fabfilter:pro_q_3",
		},
	}
	proQ2 := model.AIOWork{
		ID:           103,
		Type:         "software",
		SoftwareKey:  "cpe:a:fabfilter:pro_q_2",
		DisplayTitle: "FabFilter Pro-Q 2",
		Titles:       []string{"FabFilter Pro-Q 2"},
		Software: model.SoftwareInformation{
			Vendor:    "FabFilter",
			Platforms: []string{"windows", "macos"},
			NVDCPE:    "cpe:2.3:a:fabfilter:pro_q_2",
		},
	}

	for _, tc := range []struct {
		name   string
		record model.AIORecord
		works  []model.AIOWork
		want   string
		wantID int64
	}{
		{
			name:   "year-bearing Adobe Photoshop matches software instead of video",
			record: model.AIORecord{Title: "Adobe.Photoshop.2025.v26.0.Multilingual.x64"},
			works:  []model.AIOWork{photoshop},
			want:   "matched",
			wantID: 100,
		},
		{
			name:   "verified WinGet ID matches directly",
			record: model.AIORecord{Title: "VLC Setup.exe", SoftwareHints: model.SoftwareHints{Kind: "pc", WinGetID: "VideoLAN.VLC"}},
			works:  []model.AIOWork{vlc},
			want:   "matched",
			wantID: 101,
		},
		{
			name:   "verified Homebrew Cask matches directly",
			record: model.AIORecord{Title: "VLC macOS DMG", SoftwareHints: model.SoftwareHints{Kind: "mac", HomebrewCask: "vlc"}},
			works:  []model.AIOWork{vlc},
			want:   "matched",
			wantID: 101,
		},
		{
			name:   "audio plugin with AAX matches software Pro-Q 3 and not Pro-Q 2",
			record: model.AIORecord{Title: "FabFilter.Pro-Q.3.v3.24.VST3.AU.AAX.macOS"},
			works:  []model.AIOWork{proQ2, proQ3},
			want:   "matched",
			wantID: 102,
		},
		{
			name:   "bundle collection requires manual review",
			record: model.AIORecord{Title: "Adobe.Master.Collection.2025.Win64"},
			works:  []model.AIOWork{photoshop},
			want:   "unmatched",
		},
		{
			name:   "software information pending waits without guessing",
			record: model.AIORecord{Title: "Adobe.Photoshop.2025.v26.0.Multilingual.x64", SoftwareInformationPending: true},
			works:  []model.AIOWork{photoshop},
			want:   "unmatched",
		},
		{
			name:   "contradicting explicit vendor is rejected",
			record: model.AIORecord{Title: "Photoshop.v26.0.x64", SoftwareHints: model.SoftwareHints{Vendor: "OtherCorp"}},
			works:  []model.AIOWork{photoshop},
			want:   "unmatched",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := decideAIORecord(tc.record, func(filter model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
				if filter.Type != "software" {
					t.Fatalf("expected software filter type, got %q", filter.Type)
				}
				return tc.works, false, nil
			})
			if err != nil || res.Status != tc.want {
				t.Fatalf("status=%q want=%q reason=%q err=%v", res.Status, tc.want, res.Reason, err)
			}
			if tc.want == "matched" && (res.Work == nil || res.Work.ID != tc.wantID) {
				t.Fatalf("unexpected winner: %+v wantID=%d", res.Work, tc.wantID)
			}
		})
	}

	// Verify PC game (4050 & FitGirl) NEVER gets routed to software.
	for _, gameRec := range []model.AIORecord{
		{Title: "Cyberpunk.2077.v2.1.x64-FitGirl"},
		{Title: "Half.Life.2.Win64", GameHints: model.GameHints{Categories: []int{4050}}, SoftwareHints: model.SoftwareHints{Categories: []int{4050}}},
	} {
		if parsed := ParseSoftwareRelease(gameRec); parsed.Kind != "" {
			t.Fatalf("PC game falsely parsed as software: %q -> %+v", gameRec.Title, parsed)
		}
	}

	// Verify ParseID for software resources and multi-file selection.
	res, err := ParseID("software:winget:videolan.vlc", "software")
	if err != nil || res.Kind != "software" || res.ID != "winget:videolan.vlc" || res.Type != "software" {
		t.Fatalf("ParseID software failed: %+v %v", res, err)
	}
	if _, err = ParseID("software:../evil", "software"); err == nil {
		t.Fatal("invalid software key accepted by ParseID")
	}
	files := []model.AIOFile{
		{Path: "vlc-3.0.21-win64.exe", Size: 42_000_000},
		{Path: "README.txt", Size: 1024},
	}
	chosen, ok := SelectFile(files, res)
	if !ok || chosen.Path != "vlc-3.0.21-win64.exe" {
		t.Fatalf("single installer file was not selected: %+v %v", chosen, ok)
	}
}
