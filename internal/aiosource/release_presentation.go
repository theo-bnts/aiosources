package aiosource

import (
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

var presentationGameVersion = regexp.MustCompile(`(?i)\b(?:v[ ._-]?\d{1,12}(?:[ ._-]\d{1,12}){1,4}|v\d{6,12}|build[ ._-]?\d{1,12})\b`)
var presentationSoftwareVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,47}$`)
var presentationAuthor = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} .,'’&()_-]{0,199}$`)

var presentationPlatforms = map[string]string{
	"windows": "Windows", "linux": "Linux", "mac": "macOS", "pc": "PC",
	"ps": "PlayStation", "ps2": "PlayStation 2", "ps3": "PlayStation 3",
	"ps4": "PlayStation 4", "ps5": "PlayStation 5", "psp": "PSP", "vita": "PS Vita",
	"nds": "Nintendo DS", "3ds": "Nintendo 3DS", "switch": "Nintendo Switch", "switch-2": "Nintendo Switch 2",
	"wii": "Wii", "wiiu": "Wii U", "xbox360": "Xbox 360", "xboxone": "Xbox One",
	"series-x-s": "Xbox Series X|S",
	"android":    "Android", "ios": "iOS", "xbox": "Xbox",
}

// Only release evidence is presented. A game's catalog-wide platform list
// describes all its editions, not the compatibility of this particular file.
func releasePlatforms(record model.AIORecord) []string {
	values := slices.Clone(record.GameHints.Platforms)
	values = append(values, gamePlatformRE.FindAllString(record.Title, -1)...)
	platforms := []string{}
	for _, value := range values {
		key := gamePlatform(value)
		if Normalize(value) == "pc" {
			key = "pc"
		}
		if label := presentationPlatforms[key]; label != "" && !slices.Contains(platforms, label) {
			platforms = append(platforms, label)
		}
	}
	// PC is a general desktop label, not evidence for Windows specifically.
	if slices.ContainsFunc(platforms, func(value string) bool {
		return value == "Windows" || value == "Linux" || value == "macOS"
	}) {
		platforms = slices.DeleteFunc(platforms, func(value string) bool { return value == "PC" })
	}
	return platforms
}

// Games and books reuse the same formatter as videos without exposing raw
// source objects or changing identifiers, matching decisions or playback hints.
func releaseContentDetails(record model.AIORecord) []string {
	if game := ParseGameRelease(record); record.IdentificationType == "game" || game.Game && !game.Conflict {
		lines := []string{}
		if platforms := releasePlatforms(record); len(platforms) > 0 {
			lines = append(lines, "🎮 "+strings.Join(platforms, " • "))
		}
		versions := []string{}
		for _, value := range presentationGameVersion.FindAllString(record.Title, 3) {
			if !slices.Contains(versions, value) {
				versions = append(versions, value)
			}
		}
		if len(versions) > 0 {
			lines = append(lines, "🏷️ "+strings.Join(versions, " • "))
		}
		return lines
	}
	if record.IdentificationType == "software" || (record.IdentificationType == "" && record.VideoType == "" && record.BookHints.Kind == "" && ParseSoftwareRelease(record).Kind != "") {
		parsed := ParseSoftwareRelease(record)
		if parsed.Kind != "" || record.IdentificationType == "software" {
			lines := []string{}
			platLabels := make([]string, 0, len(parsed.Platforms))
			for _, p := range parsed.Platforms {
				if lbl := map[string]string{"windows": "Windows", "macos": "macOS", "linux": "Linux", "android": "Android", "ios": "iOS"}[p]; lbl != "" {
					platLabels = append(platLabels, lbl)
				}
			}
			if parsed.Architecture != "" {
				platLabels = append(platLabels, strings.ToUpper(parsed.Architecture))
			}
			if len(platLabels) > 0 {
				lines = append(lines, "💻 "+strings.Join(platLabels, " • "))
			} else {
				lines = append(lines, "💻 Software")
			}
			meta := []string{}
			if parsed.Version != "" && presentationSoftwareVersion.MatchString(parsed.Version) {
				meta = append(meta, parsed.Version)
			}
			for _, tag := range parsed.Tags {
				if len(meta) >= 5 {
					break
				}
				meta = append(meta, tag)
			}
			if len(meta) > 0 {
				lines = append(lines, "🏷️ "+strings.Join(meta, " • "))
			}
			return lines
		}
	}
	if record.IdentificationType == "music" || (record.IdentificationType == "" && record.VideoType == "" && record.BookHints.Kind == "" && ParseMusicRelease(record).Kind != "") {
		parsed := ParseMusicRelease(record)
		if parsed.Kind == "" && record.IdentificationType == "music" {
			parsed.Kind = "music"
		}
		if parsed.Kind != "" {
			label := map[string]string{"music": "Music", "lossless": "Lossless", "mp3": "MP3"}[parsed.Kind]
			if label == "" {
				label = "Music"
			}
			lines := []string{"🎵 " + label}
			if artist := strings.TrimSpace(parsed.Artist); presentationAuthor.MatchString(artist) {
				lines = append(lines, "🎤 "+artist)
			}
			if catno := strings.TrimSpace(parsed.CatalogNumber); catno != "" && presentationAuthor.MatchString(catno) {
				lines = append(lines, "🏷️ "+strings.ToUpper(catno))
			}
			return lines
		}
	}
	if record.IdentificationType == "book" && record.BookHints.Kind == "" {
		record.BookHints.Kind = "book"
	}
	book := ParseBookRelease(record)
	label := map[string]string{"book": "Book", "ebook": "Ebook", "audiobook": "Audiobook"}[book.Kind]
	if label == "" {
		return nil
	}
	// Unknown native fields never become public labels or URLs.
	if slices.Contains([]string{
		"epub", "pdf", "mobi", "azw", "azw3", "fb2", "djvu", "cbr", "cbz", "cb7", "cba", "cbt",
		"m4b", "aax", "aaxc", "mp3", "flac", "aac", "m4a", "ogg", "opus", "wav",
	}, book.Format) {
		label += " • " + strings.ToUpper(book.Format)
	}
	lines := []string{"📖 " + label}
	if author := strings.TrimSpace(book.Author); presentationAuthor.MatchString(author) {
		lines = append(lines, "✍️ "+author)
	}
	if book.ISBN != "" {
		lines = append(lines, "🔖 ISBN "+book.ISBN)
	}
	return lines
}
