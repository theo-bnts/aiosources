package aiosource

import (
	"slices"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestGameIdentificationKeepsPlatformsSequelsAndOtherMediaSeparate(t *testing.T) {
	game := model.AIOWork{ID: 1, IGDB: "101", Type: "game", Titles: []string{"Synthetic Odyssey"}, Years: []int{2024}, Game: model.GameInformation{Platforms: []model.GamePlatform{{ID: 6, Name: "PC (Microsoft Windows)", Slug: "win"}}}}
	for _, tc := range []struct {
		title  string
		native string
		kind   string
		want   string
	}{
		{"Synthetic.Odyssey.PC.v1.2.3", "", "game", "matched"},
		{"Synthetic.Odyssey.PC.ISO", "", "game", "matched"},
		{"Synthetic.Odyssey.PS5", "", "game", "unmatched"},
		{"Synthetic.Odyssey.2.PC", "", "game", "unmatched"},
		{"Synthetic.Odyssey.PC", "102", "game", "unmatched"},
		{"Synthetic.Odyssey.PC", "101", "game", "matched"},
		{"Synthetic.Odyssey.Collection.PC", "", "game", "ambiguous"},
		{"Synthetic.Odyssey.DLC.PC", "", "game", "unmatched"},
		{"Synthetic.Odyssey.(2020).PC", "", "game", "unmatched"},
		{"Synthetic.Odyssey.PC", "", "movie", "unmatched"},
	} {
		t.Run(tc.title+tc.native+tc.kind, func(t *testing.T) {
			r := model.AIORecord{Title: tc.title, IGDB: tc.native, GameHints: model.GameHints{Kind: "game", IGDB: tc.native}}
			if tc.kind == "movie" {
				r.VideoType = "movie"
			}
			p := ParseGameRelease(r)
			result, err := decideGame(r, p, func(filter model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
				if filter.Type != "game" || !filter.ExactOnly {
					t.Fatal("game discovery changed video safety")
				}
				return []model.AIOWork{game}, false, nil
			})
			if err != nil || result.Status != tc.want {
				t.Fatalf("status %s, expected %s (%s): %v", result.Status, tc.want, p.Normalized, err)
			}
		})
	}
	if p := ParseGameRelease(model.AIORecord{Title: "Synthetic Odyssey S01E02 1080p", GameHints: model.GameHints{Categories: []int{5000}}}); p.Game {
		t.Fatal("ordinary series became a game")
	}
	for _, title := range []string{"The.Switch.2024.1080p", "Example.Switch.2.2026.1080p", "Synthetic.Reloaded.2024.720p", "An.Ancient.Rune.2024", "Synthetic.Windows.2024"} {
		if p := ParseGameRelease(model.AIORecord{Title: title}); p.Game {
			t.Fatal("an ordinary title word became game evidence", title)
		}
	}
	if p := ParseGameRelease(model.AIORecord{Title: "Synthetic.Odyssey-RELOADED", VideoType: "movie"}); p.Game {
		t.Fatal("inferred game evidence displaced an explicit video type")
	}
	if p := ParseGameRelease(model.AIORecord{Title: "Synthetic.Odyssey-TENOKE"}); !p.Game || p.Normalized != "synthetic odyssey" {
		t.Fatal("a game release suffix was not recognized")
	}
	if p := ParseGameRelease(model.AIORecord{Title: "Synthetic.Odyssey.PC", GameHints: model.GameHints{Categories: []int{4000, 4050}}}); !p.Game || p.Conflict {
		t.Fatal("the parent PC category contradicted its Games subcategory")
	}
	if p := ParseGameRelease(model.AIORecord{Title: "Synthetic 2024 PC", GameHints: model.GameHints{Kind: "game"}}); !slices.Contains([]string{p.Normalized}, "synthetic 2024") {
		t.Fatal("a numbered game title lost its year")
	}
	for _, title := range []string{"Synthetic.Odyssey.Switch.NSP", "Synthetic.Odyssey.NDS.ROM", "Synthetic.Odyssey.PSP.ISO"} {
		p := ParseGameRelease(model.AIORecord{Title: title, GameHints: model.GameHints{Kind: "game"}})
		if p.Normalized != "synthetic odyssey" || len(p.Platforms) != 1 {
			t.Fatal("game package format displaced the title or platform", title, p)
		}
	}
}

func TestGameArchivesUseFileSelectionWithoutBecomingVideos(t *testing.T) {
	r, err := ParseID("igdb:101", "game")
	if err != nil {
		t.Fatal(err)
	}
	files := []model.AIOFile{{Path: "Synthetic.Game.iso", Size: 100}, {Path: "readme.txt", Size: 2}}
	file, ok := SelectFile(files, r)
	if !ok || file.Path != "Synthetic.Game.iso" {
		t.Fatal("game archive was rejected")
	}
	files = append(files, model.AIOFile{Path: "Synthetic.Game.part2.rar", Size: 50})
	if _, ok = SelectFile(files, r); ok {
		t.Fatal("multiple archives silently selected")
	}
	if _, ok = SelectFile(files, Resource{Type: "movie"}); ok {
		t.Fatal("archives became a video")
	}
	if _, err = ParseID("igdb:101:1", "game"); err == nil {
		t.Fatal("game accepted video numbering")
	}
}

func TestGameReleaseTechnicalLabelsPreserveExactIdentity(t *testing.T) {
	work := model.AIOWork{Type: "game", IGDB: "101", Titles: []string{"Example Odyssey"}, Game: model.GameInformation{Platforms: []model.GamePlatform{{Name: "PC (Microsoft Windows)"}}}}
	for _, suffix := range []string{
		".v.1.2.3.Portable.FRENCH.MULTI10.x86_64-InsaneRamZes",
		" (V12345678) [WIN X64 MULTI PORTABLE] [PREACTIVATED]",
		".Windows.x64.Multi.Repack-FitGirl",
		".v1.2.3-[FitGirl-Repack]",
		".[FitGirl.Repack]",
		".MULTI14.REPACK-KaOs",
		"-Mephisto",
	} {
		r := model.AIORecord{Title: "Example.Odyssey" + suffix, GameHints: model.GameHints{Kind: "game"}}
		parsed := ParseGameRelease(r)
		result, err := decideGame(r, parsed, func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
			return []model.AIOWork{work}, false, nil
		})
		if err != nil || result.Status != "matched" || parsed.Normalized != "example odyssey" {
			t.Fatalf("technical suffix %q changed the identity: %+v, %s, %v", suffix, parsed, result.Status, err)
		}
	}
	for _, suffix := range []string{".2.WIN.X64", ".Deluxe.Edition.WIN.X64", ".Update.v1.2.3-ElAmigos", ".DLC.v1.2.3-RUNE", "-UnknownGroup", ".Linux.x64"} {
		r := model.AIORecord{Title: "Example.Odyssey" + suffix, GameHints: model.GameHints{Kind: "game"}}
		result, err := decideGame(r, ParseGameRelease(r), func(model.AIOCandidateFilter) ([]model.AIOWork, bool, error) {
			return []model.AIOWork{work}, false, nil
		})
		if err != nil || result.Status == "matched" {
			t.Fatalf("distinct or incompatible release %q became the base game: %s, %v", suffix, result.Status, err)
		}
	}
	for _, title := range []string{"Example.Odyssey-Notag", "Example.Win.x64", "Example.Odyssey-Mephisto"} {
		if parsed := ParseGameRelease(model.AIORecord{Title: title}); parsed.Game {
			t.Fatal("a weak label became independent game evidence", title)
		}
	}
}
