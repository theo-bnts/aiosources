package music

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Release captures music-specific identification evidence parsed from an
// imported torrent record.
type Release struct {
	Kind          string // "", "music", "lossless", "mp3"
	Title         string
	Normalized    string
	Artist        string
	Label         string
	CatalogNumber string
	Barcode       string
	MBID          string
	ReleaseMBID   string
	DiscogsID     string
	WikidataID    string
	Year          int
	StrippedYear  bool
	Collection    bool
	InvalidID     bool
	Variants      []string
	Alternatives  []string
}

var (
	musicLosslessRE = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:FLAC|ALAC|WAV|AIFF|DSD(?:64|128|256)?|SACD|LOSSLESS|24[ ._-]?bit|16[ ._-]?bit|24-?96|24-?192|24-?48|24-?44(?:\.1)?|WEB[ ._-]?FLAC|CD[ ._-]?FLAC|VINYL[ ._-]?FLAC|APE|WV)(?:\b|[\]\)\._-])`)
	musicMP3RE      = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:MP3|320\s?kbps|CBR\s?320|V0|V2|AAC\s?256|M4A|OGG|OPUS)(?:\b|[\]\)\._-])`)
	musicTagRE      = regexp.MustCompile(`(?i)(?:\b|[\[\(\._-])(?:FLAC|ALAC|WAV|AIFF|DSD(?:64|128|256)?|SACD|LOSSLESS|MP3|320\s?kbps|CBR\s?320|VBR|V0|V2|WEB[ ._-]?FLAC|CD[ ._-]?RIP|CDRIP|VINYL[ ._-]?RIP|24[ ._-]?bit|16[ ._-]?bit|24-?96|24-?192|24-?48|24-?44(?:\.1)?|44\.1kHz|48kHz|96kHz|192kHz|Hi-?Res|Qobuz|Tidal|Bandcamp|CDDA|EAC|XLD|CUE\+LOG|LOG\+CUE)(?:\b|[\]\)\._-])`)
	musicVideoRE    = regexp.MustCompile(`(?i)\b(?:2160p|1080p|720p|480p|x264|x265|h\.?264|h\.?265|HEVC|XviD|DivX|AV1|BluRay|BDRip|WEBRip|WEB-DL|HDTV|DVDRip|REMUX|HDR|MKV|MP4|AVI|S\d{1,2}E\d{1,3}|S\d{1,2}|Saison|Season|INTEGRALE|VOSTFR|VOST|MULTI|MULTISUB|VFF|VFQ|TRUEFRENCH|SUBFRENCH|OVA|OAV)\b`)
	musicBookRE     = regexp.MustCompile(`(?i)\b(?:audiobooks?|livres?\s+audio|epub|pdf|mobi|azw3?|cb[rz]|m4b|aaxc?|narrated\s+by|unabridged)\b`)
	musicCollRE     = regexp.MustCompile(`(?i)\b(?:discograph(?:y|ie)|complete\s+(?:discography|works|albums|studio\s+albums)|studio\s+albums\s+collection|all\s+albums|box\s*set|mega\s*pack)\b`)
	musicYearRE     = regexp.MustCompile(`(?:\(((?:19|20|21)[0-9]{2})\)|\[((?:19|20|21)[0-9]{2})\]|\b((?:19|20|21)[0-9]{2})\b)`)
	musicBracketRE  = regexp.MustCompile(`[\[\(\{][^\]\)\}]*[\]\)\}]`)
	musicEditionRE  = regexp.MustCompile(`(?i)\b(?:deluxe|expanded|special|collector'?s?|anniversary|remaster(?:ed)?|reissue|bonus\s+tracks?|japan(?:ese)?|explicit|clean|limited|edition|version|ep|lp|ost|soundtrack|web|cd|vinyl|digital)\b`)
	musicSplitRE    = regexp.MustCompile(`\s+[-–—]\s+`)
)

// ParseRecord classifies whether a torrent record is a music release and
// extracts artist, album, year, format and external identifiers.
func ParseRecord(r model.AIORecord) Release {
	h := r.MusicHints
	out := Release{
		Title:    strings.TrimSpace(r.Title),
		Variants: []string{},
	}
	if musicBookRE.MatchString(r.Title) {
		return Release{}
	}
	for _, c := range h.Categories {
		if c == 3030 || (c >= 7000 && c < 8000) || (c >= 1000 && c < 2000) || c == 4050 {
			return Release{}
		}
	}
	for _, name := range h.CategoryNames {
		low := strings.ToLower(name)
		if strings.Contains(low, "audiobook") || strings.Contains(low, "livre audio") || strings.Contains(low, "ebook") || strings.Contains(low, "book") {
			return Release{}
		}
	}

	// Parse explicit identifiers from hints.
	if h.MBID != "" {
		if id := MBID(h.MBID); id != "" {
			out.MBID = id
		} else {
			out.InvalidID = true
		}
	}
	if r.MusicBrainz != "" && out.MBID == "" {
		if id := MBID(r.MusicBrainz); id != "" {
			out.MBID = id
		} else {
			out.InvalidID = true
		}
	}
	if h.ReleaseMBID != "" {
		if id := MBID(h.ReleaseMBID); id != "" {
			out.ReleaseMBID = id
		} else {
			out.InvalidID = true
		}
	}
	if h.DiscogsID != "" {
		if id := DiscogsID(h.DiscogsID); id != "" {
			out.DiscogsID = id
		} else {
			out.InvalidID = true
		}
	}
	if h.WikidataID != "" {
		if id := WikidataID(h.WikidataID); id != "" {
			out.WikidataID = id
		} else {
			out.InvalidID = true
		}
	}
	if h.Barcode != "" {
		if bc := Barcode(h.Barcode); bc != "" {
			out.Barcode = bc
		} else {
			out.InvalidID = true
		}
	}
	out.CatalogNumber = CatalogNumber(h.CatalogNumber)
	out.Label = strings.TrimSpace(h.Label)
	out.Artist = strings.TrimSpace(h.Artist)

	// Determine whether independent music evidence exists.
	kindHint := strings.ToLower(strings.TrimSpace(h.Kind))
	isMusic := kindHint == "music" || kindHint == "audio" || kindHint == "album" || kindHint == "mp3" || kindHint == "flac" || kindHint == "lossless"
	isMusic = isMusic || out.MBID != "" || out.ReleaseMBID != "" || out.DiscogsID != "" || out.Barcode != "" || out.CatalogNumber != "" || h.Artist != "" || h.Album != ""
	for _, c := range h.Categories {
		if c == 3000 || c == 3010 || c == 3040 || c == 3050 || c == 3060 {
			isMusic = true
		}
	}
	for _, name := range h.CategoryNames {
		n := Normalize(name)
		if slices.Contains([]string{"music", "musique", "audio", "flac", "mp3", "lossless", "album", "albums", "singles", "vinyle", "vinyl"}, n) ||
			strings.Contains(n, "musique") || strings.Contains(n, "music") {
			isMusic = true
		}
	}
	if !isMusic && musicTagRE.MatchString(r.Title) && !musicVideoRE.MatchString(r.Title) && r.VideoType == "" && r.Season == nil && r.Episode == nil && r.IMDb == "" && r.TMDB == "" && r.TVDB == "" && r.IGDB == "" {
		isMusic = true
	}
	if !isMusic {
		return Release{}
	}

	// Classify Torznab music subtype (lossless vs mp3 vs generic music).
	formatEvidence := r.Title + " " + h.Format + " " + strings.Join(h.CategoryNames, " ")
	switch {
	case slices.Contains(h.Categories, 3040) || musicLosslessRE.MatchString(formatEvidence):
		out.Kind = "lossless"
	case slices.Contains(h.Categories, 3010) || musicMP3RE.MatchString(formatEvidence):
		out.Kind = "mp3"
	default:
		out.Kind = "music"
	}

	out.Collection = musicCollRE.MatchString(r.Title)

	// Extract year from bracketed/parenthesized or trailing year tokens.
	cleaned := strings.TrimSpace(r.Title)
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	if matches := musicYearRE.FindAllStringSubmatch(cleaned, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		for _, g := range last[1:] {
			if y := parseYear(g); y > 0 {
				out.Year = y
				out.StrippedYear = true
				break
			}
		}
	}

	// Strip technical brackets and music format/codec tags.
	body := stripMusicNoise(cleaned)

	// Extract Artist - Album if not already explicitly provided.
	albumTitle := strings.TrimSpace(h.Album)
	if albumTitle != "" {
		out.Title = albumTitle
		if out.Artist == "" {
			if parts := musicSplitRE.Split(body, 2); len(parts) == 2 {
				out.Artist = strings.TrimSpace(parts[0])
			}
		}
	} else if parts := musicSplitRE.Split(body, 2); len(parts) == 2 {
		left, right := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if out.Artist == "" && left != "" && right != "" {
			out.Artist = left
			out.Title = right
			out.Alternatives = append(out.Alternatives, left)
		} else if right != "" {
			out.Title = right
		}
	} else {
		out.Title = body
	}

	if out.Title == "" {
		out.Title = body
	}
	out.Title = cleanAlbumTitle(out.Title)
	out.Artist = cleanArtistName(out.Artist)
	out.Normalized = Normalize(out.Title)

	addVariant := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if n := Normalize(v); n != "" && n != out.Normalized && !slices.Contains(out.Variants, v) {
			out.Variants = append(out.Variants, v)
		}
	}
	// Keep edition-stripped album title as a matching variant.
	stripped := strings.TrimSpace(musicEditionRE.ReplaceAllString(out.Title, " "))
	stripped = strings.Join(strings.Fields(stripped), " ")
	addVariant(stripped)
	if body != "" && body != out.Title {
		addVariant(body)
	}
	return out
}

func stripMusicNoise(s string) string {
	// Remove trailing bracketed groups containing format/year/source tags.
	s = musicBracketRE.ReplaceAllStringFunc(s, func(group string) string {
		inner := group[1 : len(group)-1]
		if musicTagRE.MatchString(inner) || musicYearRE.MatchString(inner) || musicEditionRE.MatchString(inner) {
			return " "
		}
		return group
	})
	s = musicTagRE.ReplaceAllString(s, " ")
	// Strip standalone 4-digit release years at end or surrounded by separators.
	if loc := musicYearRE.FindAllStringIndex(s, -1); len(loc) > 0 {
		last := loc[len(loc)-1]
		// Only strip trailing year (not a title like "1989").
		if last[0] > 0 && len(strings.TrimSpace(s[:last[0]])) >= 2 {
			s = s[:last[0]] + " " + s[last[1]:]
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func cleanAlbumTitle(s string) string {
	s = strings.Trim(s, " -–—.|/")
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

func cleanArtistName(s string) string {
	s = strings.Trim(s, " -–—.|/")
	low := strings.ToLower(strings.TrimSpace(s))
	if low == "va" || low == "v.a." || low == "v/a" {
		return "Various Artists"
	}
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// ImportSelection configures which MusicBrainz release groups are retained
// during dump import and live replication.
type ImportSelection struct {
	OfficialOnly  bool
	ExcludeSpoken bool
	IncludeCovers bool
	Types         []string
	Languages     []string
}

// Key builds a stable fingerprint of the active catalog filter configuration.
func (s ImportSelection) Key() string {
	types, ok := ImportTypes(s.Types)
	if !ok {
		types = DefaultPrimaryTypes
	}
	langs, _ := ImportLanguages(s.Languages)
	return "v1:official=" + strconv.FormatBool(s.OfficialOnly) +
		";spoken=" + strconv.FormatBool(s.ExcludeSpoken) +
		";covers=" + strconv.FormatBool(s.IncludeCovers) +
		";types=" + strings.Join(types, ",") +
		";langs=" + strings.Join(langs, ",")
}
