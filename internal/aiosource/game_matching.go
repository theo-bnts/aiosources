package aiosource

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

type GameRelease struct {
	Title, Normalized string
	Platforms         []string
	Year              *int
	Game, Conflict    bool
}

var gameGroupRE = regexp.MustCompile(`(?i)(?:-[\[(]?|[\[(])(?:CODEX|RUNE|TENOKE|FLT|SKIDROW|RELOADED|GOG|DARKSiDERS|TiNYiSO|DINOByTES|PLAZA|EMPRESS|ElAmigos|FitGirl|DODI)(?:\b.*)?$`)

// These labels are removed only after independent game evidence is present.
// An arbitrary hyphenated suffix must never turn a different title into a match.
var gameExtraGroupRE = regexp.MustCompile(`(?i)(?:-[\[(]?|[\[(])(?:Mephisto|voices38|KaOs|InsaneRamZes|SeleZen|HardwareMining|Notag)(?:\b.*)?$`)
var gameVersionRE = regexp.MustCompile(`(?i)\b(?:v[ ._-]?\d+(?:[ ._-]\d+)+|v\d{6,12}|build[ ._-]?\d+|repack|portable|multi\d*|french|english|german|spanish|italian|russian|japanese|digital[ ._-]?download|DRM[ ._-]?FREE)\b`)
var gameArchitectureRE = regexp.MustCompile(`(?i)\b(?:x86[ ._-]64|x64|x86|preactivated|preactived|preinstalled)\b`)
var gamePlatformRE = regexp.MustCompile(`(?i)\b(?:Windows|Win32|Win64|Win[ ._-]+(?:x86[ ._-]64|x64|x86|32|64)|Linux|macOS|OSX|PC|PS[1-5]|PSP|PS[ ._-]?Vita|PlayStation[ ._-]?(?:[1-5]|Portable|Vita)|Nintendo[ ._-]?(?:Switch(?:[ ._-]?2)?|3DS|DS)|Switch(?:[ ._-]?2)?|N3DS|NDS|3DS|WiiU?|XBOX[ ._-]?(?:360|ONE|SERIES[ ._-]?[XS])|NSW)\b`)
var gamePackageRE = regexp.MustCompile(`(?i)\b(?:NSP|NSZ|XCI|XCZ|ISO|ROM)\b`)
var gameYearRE = regexp.MustCompile(`(?:\((?:19|20|21)[0-9]{2}\)|\[(?:19|20|21)[0-9]{2}\])`)
var gameCollectionRE = regexp.MustCompile(`(?i)\b(?:collection|anthology|mega[ ._-]?pack|games[ ._-]?pack)\b`)

func gamePlatform(value string) string {
	v := Normalize(value)
	switch v {
	case "pc", "windows", "win32", "win64", "win x64", "win x86", "win x86 64", "win 32", "win 64", "pc microsoft windows":
		return "windows"
	case "linux", "steamos":
		return "linux"
	case "mac", "macos", "osx", "mac os x":
		return "mac"
	case "ps1", "playstation", "playstation 1":
		return "ps"
	case "ps2", "playstation 2":
		return "ps2"
	case "ps3", "playstation 3":
		return "ps3"
	case "ps4", "playstation 4":
		return "ps4"
	case "ps5", "playstation 5":
		return "ps5"
	case "psp", "playstation portable":
		return "psp"
	case "ps vita", "psvita", "playstation vita":
		return "vita"
	case "nds", "nintendo ds":
		return "nds"
	case "n3ds", "3ds", "nintendo 3ds":
		return "3ds"
	case "switch", "nintendo switch", "nsw":
		return "switch"
	case "switch2", "switch 2", "nintendo switch2", "nintendo switch 2":
		return "switch-2"
	case "xbox 360", "xbox360":
		return "xbox360"
	case "xbox one", "xboxone":
		return "xboxone"
	case "xbox series x", "xbox series s", "xbox series x s":
		return "series-x-s"
	case "wii":
		return "wii"
	case "wiiu", "wii u":
		return "wiiu"
	}
	return strings.ReplaceAll(v, " ", "-")
}

func ParseGameRelease(r model.AIORecord) GameRelease {
	h := r.GameHints
	p := GameRelease{Title: r.Title}
	kind := Normalize(h.Kind)
	p.Game = kind == "game" || kind == "games" || kind == "videogame" || kind == "video game"
	p.Game = p.Game || h.IGDB != ""
	for _, c := range h.Categories {
		if c >= 1000 && c < 2000 || c == 4050 {
			p.Game = true
		}
	}
	for _, name := range h.CategoryNames {
		n := Normalize(name)
		if slices.Contains([]string{"game", "games", "video game", "video games", "pc games", "console", "consoles", "console games", "computer games", "jeux", "jeu", "jeux pc", "jeux consoles"}, n) || strings.Contains(n, "jeu video") || strings.Contains(n, "jeux video") {
			p.Game = true
		}
	}
	for _, platform := range h.Platforms {
		if v := gamePlatform(platform); v != "" && !slices.Contains(p.Platforms, v) {
			p.Platforms = append(p.Platforms, v)
		}
	}
	if !p.Game && (r.VideoType != "" || r.IMDb != "" || r.TMDB != "" || r.TVDB != "") {
		return p
	}
	s := strings.ReplaceAll(strings.ReplaceAll(r.Title, ".", " "), "_", " ")
	markers := gamePlatformRE.FindAllString(s, -1)
	strongPlatform := false
	for _, marker := range markers {
		v := gamePlatform(marker)
		label := Normalize(marker)
		genericSwitch := label == "switch" || label == "switch 2" || label == "switch2"
		strongPlatform = strongPlatform || v != "windows" && v != "mac" && v != "linux" && !genericSwitch
	}
	if (strongPlatform || gameGroupRE.MatchString(s)) && r.VideoType == "" && r.IMDb == "" && r.TMDB == "" && r.TVDB == "" {
		p.Game = true
	}
	if !p.Game {
		return p
	}
	for _, c := range h.Categories {
		if c >= 3000 && c < 4000 || c > 4000 && c < 5000 && c != 4050 {
			p.Conflict = true
		}
	}
	if kind != "" && kind != "game" && kind != "games" && kind != "videogame" && kind != "video game" {
		p.Conflict = true
	}
	p.Conflict = p.Conflict || r.VideoType != "" || r.IMDb != "" || r.TMDB != "" || r.TVDB != "" || r.BookHints.Kind == "book" || r.BookHints.Kind == "ebook" || r.BookHints.Kind == "audiobook"
	for _, platform := range markers {
		v := gamePlatform(platform)
		if !slices.Contains(p.Platforms, v) {
			p.Platforms = append(p.Platforms, v)
		}
	}
	s = gameGroupRE.ReplaceAllString(s, "")
	s = gameExtraGroupRE.ReplaceAllString(s, "")
	s = gameVersionRE.ReplaceAllString(s, "")
	s = gamePlatformRE.ReplaceAllString(s, "")
	s = gameArchitectureRE.ReplaceAllString(s, "")
	s = gamePackageRE.ReplaceAllString(s, "")
	if at := gameYearRE.FindStringIndex(s); at != nil && len(strings.TrimSpace(s[:at[0]])) > 1 {
		year, _ := strconv.Atoi(strings.Trim(s[at[0]:at[1]], "()[]"))
		p.Year = &year
		s = s[:at[0]] + s[at[1]:]
	}
	p.Title = strings.Trim(s, " []()-_")
	p.Normalized = Normalize(p.Title)
	return p
}

func decideGame(r model.AIORecord, p GameRelease, find store.AIOCandidateSearch) (model.AIOMatchResult, error) {
	r.Normalized = p.Normalized
	r.IdentificationType = "game"
	result := model.AIOMatchResult{Record: r, Status: "unmatched", Reason: "insufficient game evidence"}
	if p.Conflict {
		result.Reason = "media type conflict"
		return result, nil
	}
	if gameCollectionRE.MatchString(p.Title) {
		result.Status = "ambiguous"
		result.Reason = "game collection requires review"
		return result, nil
	}
	if len([]rune(p.Normalized)) < 2 {
		return result, nil
	}
	works, overflow, err := find(model.AIOCandidateFilter{Type: "game", Title: p.Normalized, IGDB: r.IGDB, Year: p.Year, ExactOnly: true})
	if err != nil {
		return result, err
	}
	winners := []model.AIOWork{}
	for _, w := range works {
		if w.Type != "game" || r.IGDB != "" && r.IGDB != w.IGDB || p.Year != nil && !slices.Contains(w.Years, *p.Year) {
			continue
		}
		exact := false
		for _, t := range w.Titles {
			exact = exact || Normalize(t) == p.Normalized
		}
		if !exact {
			continue
		}
		platformCompatible := len(p.Platforms) == 0
		for _, hint := range p.Platforms {
			for _, platform := range w.Game.Platforms {
				platformCompatible = platformCompatible || hint == gamePlatform(platform.Name) || hint == gamePlatform(platform.Abbreviation) || hint == platform.Slug
			}
		}
		if !platformCompatible {
			continue
		}
		// Exact aliases still need a game-specific declaration. No fuzzy title
		// or unverified numeral may select a sequel, remake, DLC or homonym.
		if len([]rune(p.Normalized)) < 5 && r.IGDB == "" && p.Year == nil {
			continue
		}
		winners = append(winners, w)
	}
	if overflow || len(winners) > 1 {
		result.Status = "ambiguous"
		result.Reason = "multiple plausible games"
		return result, nil
	}
	if len(winners) == 1 {
		result.Status = "matched"
		result.Reason = "exact game alias with compatible evidence"
		result.Work = &winners[0]
	}
	return result, nil
}
