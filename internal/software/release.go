package software

import (
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Release captures software-specific identification evidence parsed from an
// imported torrent record.
type Release struct {
	Kind         string // "", "pc", "0day", "iso", "mac", "mobile", "ios", "android"
	Title        string
	Normalized   string
	Vendor       string
	Version      string
	Platforms    []string
	Architecture string
	Tags         []string
	SoftwareKey  string
	WinGetID     string
	HomebrewCask string
	NVDCPE       string
	Collection   bool
	InvalidID    bool
	Variants     []string
}

var (
	// Strict anti-collision patterns for non-software media.
	swVideoRE = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:2160p|1080p|720p|480p|576p|4k|uhd|x264|x265|h\.?264|h\.?265|hevc|xvid|divx|av1|bluray|bdrip|brrip|webrip|web-dl|hdtv|dvdrip|remux|hdr10\+?|dolby\.?vision|s\d{1,2}e\d{1,3}|s\d{1,2}|saison|season|vostfr|truefrench|subfrench|french\.?sub|multi\.?sub|ova|oav)(?:\b|[\]\)\._-])|\.(?:mkv|mp4|avi|webm|m4v|ts)$`)
	swGameRE  = regexp.MustCompile(`(?i)(?:-[\[(]?|[\[\(\._-])(?:CODEX|RUNE|TENOKE|FLT|SKIDROW|RELOADED|GOG|DARKSiDERS|TiNYiSO|DINOByTES|PLAZA|EMPRESS|ElAmigos|FitGirl|DODI|KaOs|InsaneRamZes|RAZOR1911|CPY|STEAMPUNKS|HOODLUM|GOLDERG|GOLDBERG)(?:\b|[\]\)\._-])`)
	swConsole = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:PS[1-5]|PSP|PS[ ._-]?Vita|PlayStation[ ._-]?(?:[1-5]|Portable|Vita)|Nintendo[ ._-]?(?:Switch(?:[ ._-]?2)?|3DS|DS)|N3DS|NDS|3DS|WiiU?|XBOX[ ._-]?(?:360|ONE|SERIES[ ._-]?[XS])|NSW|NSP|NSZ|XCI|XCZ)(?:\b|[\]\)\._-])`)
	swBookRE  = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|audiobooks?|e[ ._-]?books?|livres?[ ._-]?audio|narrated\s+by|unabridged)(?:\b|[\]\)\._-])`)
	swMusicRE = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:FLAC|ALAC|AIFF|DSD(?:64|128|256)?|SACD|LOSSLESS|MP3|320\s?kbps|CBR\s?320|V0|V2|24[ ._-]?bit|16[ ._-]?bit|24-?96|24-?192|Qobuz|Tidal|Bandcamp|CDRIP|CD[ ._-]?RIP|VINYL[ ._-]?RIP|CUE\+LOG|LOG\+CUE)(?:\b|[\]\)\._-])`)

	// Software-specific technical signal patterns.
	swStrongRE   = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:Win64|Win32|Windows|macOS|Mac[ ._-]?OS[ ._-]?X?|OSX|AppImage|DEB|RPM|DMG|PKG|MSI|APK|IPA|VST3?|AAX|Pre-?Activated|PreActivated|Incl\.?(?:Crack|Keygen|Patch)|Keygen|x86[ ._-]?64|amd64|Win[ ._-]?x(?:64|86)|Installer|Setup\.exe)(?:\b|[\]\)\._-])`)
	swVersionRE  = regexp.MustCompile(`(?i)(?:^|[\s._(\[-])(?:v(?:er(?:sion)?)?\.?\s*(\d+(?:\.\d+){1,4}(?:-(?:alpha|beta|rc|pre|patch|rev|sp|u)\d*|[a-z])?)|(\d+\.\d+\.\d+(?:\.\d+)?)|build[ ._-]?(\d{2,7})|r(\d{4,6}))(?:\b|[\s._)\]-]|$)`)
	swShortVerRE = regexp.MustCompile(`\s+(\d{1,4}\.\d{1,4})(?:\s|$)`)
	swCCYearRE   = regexp.MustCompile(`(?i)\b(CC[ ._-]?(?:19|20)\d{2})\b`)
	swYearRE     = regexp.MustCompile(`(?:\b|[\[\(\._-])((?:199|20[0-3])\d)(?:\b|[\]\)\._-])`)
	swArchRE     = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(x86[ ._-]?64|amd64|x64|win64|64[ ._-]?bit|x86|win32|32[ ._-]?bit|arm64|aarch64|apple[ ._-]?silicon|universal2?|m[1-4])(?:\b|[\]\)\._-])`)
	swOSRE       = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(Windows|Win64|Win32|Win1[01]|macOS|Mac[ ._-]?OS[ ._-]?X?|OSX|Linux|Ubuntu|Debian|Fedora|AppImage|DEB|RPM|Android|APK|iOS|iPadOS|IPA)(?:\b|[\]\)\._-])`)
	swModTagRE   = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(Portable|Repack|Multilingual|Multi\d*|Silent|Unattended|Pre-?Activated|PreActivated|Cracked|Patched|Activated|Keygen|Retail|ISO|DMG|PKG|MSI|EXE|AppImage|DEB|RPM|APK|IPA|VST3?|AAX|LTS|ARM64|x64|x86|Universal)(?:\b|[\]\)\._-])`)
	swReleaseTag = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(Portable|Repack|Multilingual|Pro|Enterprise|Studio|VST3|VST|AAX|LTS|Desktop|Server|AppImage|DEB|RPM|DMG|PKG|MSI|APK|IPA|ISO|Pre-?Activated|Silent)(?:\b|[\]\)\._-])`)
	swAudioUnit  = regexp.MustCompile(`\bAU\b`)
	swStripTagRE = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:Win64|Win32|macOS|Mac[ ._-]?OS[ ._-]?X?|OSX|x86[ ._-]?64|amd64|x64|x86|64[ ._-]?bit|32[ ._-]?bit|arm64|aarch64|apple[ ._-]?silicon|universal2?|Portable|Repack|Multilingual|Multi\d*|Silent|Unattended|Pre-?Activated|PreActivated|Cracked|Patched|Activated|Incl\.?(?:Crack|Keygen|Patch)|Keygen|Crack|Retail|Installer|AppImage|DEB|RPM|DMG|PKG|MSI|EXE|APK|IPA|ISO|VST3?|AAX|LTS|Standalone|Hotfix|x86x64|x64x86)(?:\b|[\]\)\._-])`)
	swTailOSRE   = regexp.MustCompile(`(?i)\s+(?:Windows|Linux|Android|iOS|iPadOS|AU)\s*$`)
	swBracketRE  = regexp.MustCompile(`[\[\(\{][^\]\)\}]*[\]\)\}]`)
	swGroupTail  = regexp.MustCompile(`(?i)-[A-Za-z0-9_]{2,20}$`)
	swEditionRE  = regexp.MustCompile(`(?i)\s+(?:Enterprise|Ultimate|Professional|ProPlus|Pro\s+Plus|Standard|Community|Home|Business|Desktop|Server|Workstation|Edition|Suite|CC)\s*$`)
	swCollRE     = regexp.MustCompile(`(?i)\b(?:master[ ._-]?collection|software[ ._-]?pack|apps?[ ._-]?pack|mega[ ._-]?pack|all[ ._-]?in[ ._-]?one[ ._-]?pack|aio[ ._-]?pack|pack[ ._-]?(?:logiciels?|applications?)|collection[ ._-]?(?:logiciels?|software)|wpi[ ._-]?pack|complete[ ._-]?bundle)\b`)
	swAudioPlug  = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:VST3?|AU|CLAP|LV2|RTAS|Win64|Win32|macOS|OSX|x64|x86|Universal|Keygen|R2R)(?:\b|[\]\)\._-])`)
	swAudioAAX   = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])AAX(?:\b|[\]\)\._-])`)
)

// ParseRecord classifies whether an imported torrent record is a Software
// release and extracts canonical title, optional vendor, version, OS platforms,
// architecture, release tags, Torznab subtype and external identifiers.
func ParseRecord(r model.AIORecord) Release {
	h := r.SoftwareHints
	rawTitle := strings.TrimSpace(r.Title)
	if rawTitle == "" {
		return Release{}
	}

	// Hard anti-collision rejections against other media catalogs.
	if r.VideoType != "" || r.IMDb != "" || r.TMDB != "" || r.TVDB != "" || r.IGDB != "" || r.MusicBrainz != "" || r.Season != nil || r.Episode != nil {
		return Release{}
	}
	if r.BookHints.Kind == "book" || r.BookHints.Kind == "ebook" || r.BookHints.Kind == "audiobook" || r.BookHints.OpenLibrary != "" || r.BookHints.ISBN != "" {
		return Release{}
	}
	if r.GameHints.IGDB != "" || strings.EqualFold(r.GameHints.Kind, "game") || strings.EqualFold(r.GameHints.Kind, "games") {
		return Release{}
	}
	if r.MusicHints.MBID != "" || r.MusicHints.ReleaseMBID != "" || r.MusicHints.DiscogsID != "" || r.MusicHints.Barcode != "" {
		return Release{}
	}

	hasSoftwareCat := false
	for _, c := range h.Categories {
		switch {
		case c == 4050:
			// PC-Games belongs strictly to Games.
			return Release{}
		case (c >= 1000 && c < 2000) || (c >= 2000 && c < 3000) || (c >= 3000 && c < 4000) || (c >= 5000 && c < 6000) || (c >= 7000 && c < 8000):
			return Release{}
		case c == 4000 || c == 4010 || c == 4020 || c == 4030 || c == 4040 || c == 4060 || c == 4070:
			hasSoftwareCat = true
		}
	}
	for _, name := range h.CategoryNames {
		low := strings.ToLower(strings.TrimSpace(name))
		if isExcludedCategoryName(low) {
			return Release{}
		}
	}
	if swVideoRE.MatchString(rawTitle) || swGameRE.MatchString(rawTitle) || swConsole.MatchString(rawTitle) || swBookRE.MatchString(rawTitle) || swMusicRE.MatchString(rawTitle) {
		return Release{}
	}
	// Bare "AAX" without software/audio-plugin context belongs to Audible audiobooks.
	if swAudioAAX.MatchString(rawTitle) && !swAudioPlug.MatchString(rawTitle) && !hasSoftwareCat && h.Kind == "" {
		return Release{}
	}

	out := Release{
		Title:    rawTitle,
		Vendor:   strings.TrimSpace(h.Vendor),
		Variants: []string{},
	}

	// Parse explicit software identifiers from hints.
	if h.SoftwareKey != "" {
		if k := CanonicalSoftwareKey(h.SoftwareKey); k != "" {
			out.SoftwareKey = k
		} else {
			out.InvalidID = true
		}
	}
	if r.SoftwareKey != "" && out.SoftwareKey == "" {
		if k := CanonicalSoftwareKey(r.SoftwareKey); k != "" {
			out.SoftwareKey = k
		} else {
			out.InvalidID = true
		}
	}
	if h.WinGetID != "" {
		if id := WinGetID(h.WinGetID); id != "" {
			out.WinGetID = id
		} else {
			out.InvalidID = true
		}
	}
	if h.HomebrewCask != "" {
		if id := HomebrewCask(h.HomebrewCask); id != "" {
			out.HomebrewCask = id
		} else {
			out.InvalidID = true
		}
	}
	if h.NVDCPE != "" {
		if id := NVDCPE(h.NVDCPE); id != "" {
			out.NVDCPE = id
		} else {
			out.InvalidID = true
		}
	}

	// Check independent software evidence.
	hasSoftwareCatName := false
	for _, name := range h.CategoryNames {
		if isSoftwareCategoryName(name) {
			hasSoftwareCatName = true
		}
	}
	hasExplicitHint := h.Kind != "" || out.SoftwareKey != "" || out.WinGetID != "" || out.HomebrewCask != "" || out.NVDCPE != "" || out.Vendor != ""
	hasStrongTitleToken := swStrongRE.MatchString(rawTitle)
	modSignals := 0
	if swVersionRE.MatchString(rawTitle) || swCCYearRE.MatchString(rawTitle) {
		modSignals++
	}
	if swArchRE.MatchString(rawTitle) {
		modSignals++
	}
	if swOSRE.MatchString(rawTitle) {
		modSignals++
	}
	if swModTagRE.MatchString(rawTitle) {
		modSignals++
	}
	isSoftware := hasSoftwareCat || hasSoftwareCatName || hasExplicitHint || hasStrongTitleToken || modSignals >= 2
	if !isSoftware {
		return Release{}
	}

	// Extract OS platforms from hints, categories and title.
	addPlatform := func(raw string) {
		if p := NormalizePlatform(raw); p != "" && !slices.Contains(out.Platforms, p) {
			out.Platforms = append(out.Platforms, p)
		}
	}
	for _, p := range h.Platforms {
		addPlatform(p)
	}
	for _, c := range h.Categories {
		switch c {
		case 4030:
			addPlatform("macos")
		case 4060:
			addPlatform("ios")
		case 4070:
			addPlatform("android")
		}
	}
	for _, m := range swOSRE.FindAllStringSubmatch(rawTitle, -1) {
		if len(m) > 1 {
			addPlatform(m[1])
		}
	}
	if len(out.Platforms) == 0 && (slices.Contains(h.Categories, 4000) || slices.Contains(h.Categories, 4010) || hasWindowsHint(rawTitle)) {
		addPlatform("windows")
	}
	slices.Sort(out.Platforms)

	// Extract architecture.
	if h.Architecture != "" {
		out.Architecture = normalizeArch(h.Architecture)
	}
	if out.Architecture == "" {
		if m := swArchRE.FindStringSubmatch(rawTitle); len(m) > 1 {
			out.Architecture = normalizeArch(m[1])
		}
	}

	// Extract release tags.
	for _, m := range swReleaseTag.FindAllStringSubmatch(rawTitle, -1) {
		if len(m) > 1 {
			if tag := canonicalReleaseTag(m[1]); tag != "" && !slices.Contains(out.Tags, tag) {
				out.Tags = append(out.Tags, tag)
			}
		}
	}
	if swAudioUnit.MatchString(rawTitle) && (slices.Contains(out.Tags, "VST") || slices.Contains(out.Tags, "VST3") || slices.Contains(out.Tags, "AAX") || slices.Contains(out.Platforms, "macos")) {
		if !slices.Contains(out.Tags, "AU") {
			out.Tags = append(out.Tags, "AU")
		}
	}

	// Extract version (explicit hint > semantic/build version > CC year > 2-part version > standalone year).
	out.Version = strings.TrimSpace(h.Version)
	var ccYear string
	if m := swCCYearRE.FindStringSubmatch(rawTitle); len(m) > 1 {
		ccYear = strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(m[1])), " ")
	}
	if out.Version == "" {
		if m := swVersionRE.FindStringSubmatch(rawTitle); len(m) > 4 {
			switch {
			case m[1] != "":
				out.Version = m[1]
			case m[2] != "":
				out.Version = m[2]
			case m[3] != "":
				out.Version = "Build " + m[3]
			case m[4] != "":
				out.Version = "r" + m[4]
			}
		}
	}
	cleanedForShortVer := replaceSeparatorDots(strings.ReplaceAll(rawTitle, "_", " "))
	var shortVer string
	if out.Version == "" {
		if m := swShortVerRE.FindStringSubmatch(cleanedForShortVer); len(m) > 1 {
			shortVer = m[1]
			out.Version = shortVer
		}
	}
	var releaseYear string
	if matches := swYearRE.FindAllStringSubmatch(rawTitle, -1); len(matches) > 0 {
		releaseYear = matches[len(matches)-1][1]
	}
	if out.Version == "" {
		if ccYear != "" {
			out.Version = ccYear
		} else if releaseYear != "" {
			out.Version = releaseYear
		}
	}

	// Determine Torznab Software subtype ("android", "ios", "mobile", "mac", "iso", "0day", "pc").
	out.Kind = classifySoftwareKind(h, out.Platforms, out.Tags, rawTitle)
	out.Collection = swCollRE.MatchString(rawTitle)

	// Strip version/OS/arch/tag noise to recover clean canonical software title and variants.
	out.Title, out.Variants = extractSoftwareTitle(rawTitle, shortVer, ccYear, releaseYear)
	out.Normalized = Normalize(out.Title)
	return out
}

func isExcludedCategoryName(low string) bool {
	excluded := []string{
		"pc-games", "pc games", "pc/games", "games", "game", "jeux", "console", "playstation", "xbox", "nintendo",
		"movies", "movie", "films", "film", "tv", "series", "anime",
		"music", "musique", "audio/mp3", "audio/lossless", "flac", "mp3",
		"audiobook", "livre audio", "ebook", "e-book", "books", "book", "comics", "manga",
	}
	for _, token := range excluded {
		if low == token || strings.Contains(low, token) {
			return true
		}
	}
	return false
}

func isSoftwareCategoryName(name string) bool {
	n := Normalize(name)
	if n == "" {
		return false
	}
	exact := []string{
		"software", "softwares", "logiciel", "logiciels", "app", "apps", "application", "applications",
		"0day", "pc 0day", "pc iso", "pc mac", "pc", "mac", "macos", "mobile", "mobile ios", "mobile android",
		"mobile other", "android", "ios", "apk", "ipa", "utilitaire", "utilitaires", "os", "operating system",
		"operating systems", "systeme", "systemes", "plugins", "vst",
	}
	if slices.Contains(exact, n) {
		return true
	}
	return strings.Contains(n, "software") || strings.Contains(n, "logiciel") || strings.Contains(n, "application")
}

func hasWindowsHint(raw string) bool {
	low := strings.ToLower(raw)
	return strings.Contains(low, "x64") || strings.Contains(low, "x86") || strings.Contains(low, ".msi") || strings.Contains(low, ".exe") || strings.Contains(low, "portable") || strings.Contains(low, "repack")
}

func normalizeArch(raw string) string {
	low := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.TrimSpace(raw)))
	switch low {
	case "x8664", "amd64", "x64", "win64", "64bit":
		return "x64"
	case "x86", "win32", "32bit", "i386", "i686":
		return "x86"
	case "arm64", "aarch64", "applesilicon", "m1", "m2", "m3", "m4":
		return "arm64"
	case "universal", "universal2":
		return "universal"
	default:
		return ""
	}
}

func canonicalReleaseTag(raw string) string {
	low := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(raw)))
	switch low {
	case "portable":
		return "Portable"
	case "repack":
		return "Repack"
	case "multilingual":
		return "Multilingual"
	case "pro":
		return "Pro"
	case "enterprise":
		return "Enterprise"
	case "studio":
		return "Studio"
	case "vst3":
		return "VST3"
	case "vst":
		return "VST"
	case "au":
		return "AU"
	case "aax":
		return "AAX"
	case "lts":
		return "LTS"
	case "desktop":
		return "Desktop"
	case "server":
		return "Server"
	case "appimage":
		return "AppImage"
	case "deb":
		return "DEB"
	case "rpm":
		return "RPM"
	case "dmg":
		return "DMG"
	case "pkg":
		return "PKG"
	case "msi":
		return "MSI"
	case "apk":
		return "APK"
	case "ipa":
		return "IPA"
	case "iso":
		return "ISO"
	case "preactivated":
		return "PreActivated"
	case "silent":
		return "Silent"
	default:
		return ""
	}
}

func classifySoftwareKind(h model.SoftwareHints, platforms, tags []string, rawTitle string) string {
	if h.Kind != "" && h.Kind != "pc" {
		return h.Kind
	}
	if slices.Contains(h.Categories, 4070) || slices.Contains(platforms, "android") || slices.Contains(tags, "APK") {
		return "android"
	}
	if slices.Contains(h.Categories, 4060) || slices.Contains(platforms, "ios") || slices.Contains(tags, "IPA") {
		return "ios"
	}
	if slices.Contains(h.Categories, 4040) {
		return "mobile"
	}
	if slices.Contains(h.Categories, 4030) || (len(platforms) == 1 && platforms[0] == "macos") || slices.Contains(tags, "DMG") || slices.Contains(tags, "PKG") {
		return "mac"
	}
	if slices.Contains(h.Categories, 4020) || slices.Contains(tags, "ISO") {
		return "iso"
	}
	if slices.Contains(h.Categories, 4010) {
		return "0day"
	}
	if h.Kind == "pc" || slices.Contains(h.Categories, 4000) {
		return "pc"
	}
	low := strings.ToLower(rawTitle)
	if strings.Contains(low, "0day") {
		return "0day"
	}
	return "pc"
}

func extractSoftwareTitle(rawTitle, shortVer, ccYear, releaseYear string) (string, []string) {
	s := strings.TrimSpace(rawTitle)
	// Strip scene group suffix like "-R2R", "-TNT", "-CRD" before dot normalization
	// only when preceded by technical metadata (avoid stripping "Pro-Q" or "7-Zip").
	if loc := swGroupTail.FindStringIndex(s); loc != nil && loc[0] > 4 {
		prefix := s[:loc[0]]
		if swStripTagRE.MatchString(prefix) || swVersionRE.MatchString(prefix) {
			s = prefix
		}
	}

	// Remove bracketed/parenthesized technical groups.
	s = swBracketRE.ReplaceAllStringFunc(s, func(group string) string {
		inner := group[1 : len(group)-1]
		if swStripTagRE.MatchString(inner) || swVersionRE.MatchString(inner) || swYearRE.MatchString(inner) {
			return " "
		}
		return " " + inner + " "
	})

	// Replace underscores and non-numeric dots with spaces while preserving
	// version numbers so we can find the exact cut point cleanly.
	cleaned := replaceSeparatorDots(strings.ReplaceAll(s, "_", " "))

	// Truncate at the earliest technical marker (version, CC year, OS/arch/tag)
	// after at least 2 characters of product name.
	cutAt := len(cleaned)
	findCut := func(re *regexp.Regexp) {
		if loc := re.FindStringIndex(cleaned); loc != nil && loc[0] >= 2 && loc[0] < cutAt {
			cutAt = loc[0]
		}
	}
	findCut(swVersionRE)
	findCut(swCCYearRE)
	findCut(swStripTagRE)
	if shortVer != "" {
		if loc := swShortVerRE.FindStringIndex(cleaned); loc != nil && loc[0] >= 2 && loc[0] < cutAt {
			cutAt = loc[0]
		}
	}

	// Keep a copy before stripping a trailing standalone year so "Adobe Photoshop 2025"
	// becomes a matching variant alongside "Adobe Photoshop".
	head := strings.Trim(strings.Join(strings.Fields(cleaned[:cutAt]), " "), " -–—.|/")
	if head == "" {
		head = strings.Trim(strings.Join(strings.Fields(swStripTagRE.ReplaceAllString(cleaned, " ")), " "), " -–—.|/")
	}
	// Strip trailing standalone OS decoration (e.g. "Firefox Linux" -> "Firefox")
	// without breaking real OS titles like "Microsoft Windows", "Arch Linux", "Kali Linux".
	if next := strings.TrimSpace(swTailOSRE.ReplaceAllString(head, "")); next != "" && next != head {
		lowHead := strings.ToLower(head)
		if !strings.HasSuffix(lowHead, "microsoft windows") && !strings.HasSuffix(lowHead, "arch linux") && !strings.HasSuffix(lowHead, "kali linux") && !strings.HasSuffix(lowHead, "alpine linux") {
			head = next
		}
	}

	variants := []string{}
	addVariant := func(v string) {
		v = strings.Trim(strings.Join(strings.Fields(v), " "), " -–—.|/")
		if len([]rune(v)) < 2 {
			return
		}
		if Normalize(v) == "" {
			return
		}
		if !slices.Contains(variants, v) {
			variants = append(variants, v)
		}
	}

	primary := head
	// Strip trailing year from primary title (preserving year-bearing form as a variant).
	if releaseYear != "" {
		if stripped, changed := stripTrailingToken(primary, releaseYear); changed && len([]rune(stripped)) >= 2 {
			addVariant(primary)
			primary = stripped
		}
	}
	if ccYear != "" {
		addVariant(primary + " CC")
	}

	// Preserve edition-stripped variant (e.g. "JetBrains IntelliJ IDEA Ultimate" -> "JetBrains IntelliJ IDEA",
	// and "Microsoft Office 2024 ProPlus" -> "Microsoft Office 2024" and "Microsoft Office").
	if trimmed := strings.TrimSpace(swEditionRE.ReplaceAllString(primary, "")); trimmed != "" && trimmed != primary && len([]rune(trimmed)) >= 2 {
		addVariant(trimmed)
		if releaseYear != "" {
			if stripped, changed := stripTrailingToken(trimmed, releaseYear); changed && len([]rune(stripped)) >= 2 {
				addVariant(stripped)
			}
		}
	}

	// Filter out duplicates matching primary's normalized form.
	primaryNorm := Normalize(primary)
	filtered := make([]string, 0, len(variants))
	for _, v := range variants {
		if Normalize(v) != primaryNorm {
			filtered = append(filtered, v)
		}
	}
	return primary, filtered
}

func replaceSeparatorDots(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] != '.' {
			continue
		}
		prevDigit := i > 0 && b[i-1] >= '0' && b[i-1] <= '9'
		nextDigit := i+1 < len(b) && b[i+1] >= '0' && b[i+1] <= '9'
		if prevDigit && nextDigit {
			continue
		}
		b[i] = ' '
	}
	return string(b)
}

func stripTrailingToken(s, token string) (string, bool) {
	if token == "" {
		return s, false
	}
	if strings.HasSuffix(s, " "+token) {
		return strings.TrimSpace(strings.TrimSuffix(s, " "+token)), true
	}
	return s, false
}
