package aiosource

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

type streamLanguage struct {
	code, flag string
}

// ReleaseLanguageCodes exposes the same bounded, normalized audio evidence used
// by Stremio presentation. Arbitrary native strings are never exported as tags.
func ReleaseLanguageCodes(record model.AIORecord) []string {
	codes := []string{}
	for _, item := range streamLanguages(record) {
		codes = append(codes, item.code)
	}
	return codes
}

// Source APIs use ISO 639-1, ISO 639-2 and language names interchangeably.
// Keep this normalization at the presentation boundary: native values and
// correspondence evidence remain unchanged.
var streamLanguageAliases = func() map[string]streamLanguage {
	groups := []struct{ aliases, flag string }{
		{"fr fra fre french", "🇫🇷"}, {"en eng english", "🇬🇧"},
		{"es spa esp spanish", "🇪🇸"}, {"de ger deu german deutsch", "🇩🇪"},
		{"it ita italian", "🇮🇹"}, {"pt por pob ptbr brazilian portuguese", "🇵🇹"},
		{"nl dut nld dutch", "🇳🇱"}, {"ru rus russian", "🇷🇺"},
		{"ja jpn japanese", "🇯🇵"}, {"ko kor korean", "🇰🇷"},
		{"zh zho chi chinese", "🇨🇳"}, {"hi hin hindi", "🇮🇳"},
		{"ar ara arabic", "🇸🇦"}, {"pl pol polish", "🇵🇱"},
		{"tr tur turkish", "🇹🇷"}, {"sv swe swedish", "🇸🇪"},
		{"no nor norwegian", "🇳🇴"}, {"da dan danish", "🇩🇰"},
		{"fi fin finnish", "🇫🇮"}, {"cs ces cze czech", "🇨🇿"},
		{"sk slk slo slovak", "🇸🇰"}, {"hu hun hungarian", "🇭🇺"},
		{"ro ron rum romanian", "🇷🇴"}, {"el ell gre greek", "🇬🇷"},
		{"he heb hebrew", "🇮🇱"}, {"th tha thai", "🇹🇭"},
		{"vi vie vietnamese", "🇻🇳"}, {"id ind indonesian", "🇮🇩"},
		{"uk ukr ukrainian", "🇺🇦"}, {"bg bul bulgarian", "🇧🇬"},
		{"hr hrv croatian", "🇭🇷"}, {"sr srp scc serbian", "🇷🇸"},
		{"sl slv slovenian", "🇸🇮"},
	}
	out := map[string]streamLanguage{}
	for _, group := range groups {
		aliases := strings.Fields(group.aliases)
		for _, alias := range aliases {
			out[alias] = streamLanguage{code: aliases[0], flag: group.flag}
		}
	}
	return out
}()

var streamLanguageTokenRE = regexp.MustCompile(`[\p{L}\p{N}]+`)

func streamLanguages(r model.AIORecord) []streamLanguage {
	out := []streamLanguage{}
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "und" || value == "unknown" || value == "vo" {
			return
		}
		if strings.ContainsAny(value, "-_") {
			if normalized := NormalizeLanguage(strings.ReplaceAll(value, "_", "-")); normalized != "" {
				value = normalized
			}
		}
		language, known := streamLanguageAliases[value]
		if !known {
			if value == "multi" || value == "multilingual" {
				language.code = "multi"
			} else {
				language.code = NormalizeLanguage(value)
				if language.code == "" {
					return
				}
			}
		}
		if !seen[language.code] {
			out = append(out, language)
			seen[language.code] = true
		}
	}
	metadataAt := presentationMetadataStart(r.Title)
	for _, at := range streamLanguageTokenRE.FindAllStringIndex(r.Title, -1) {
		token := strings.ToLower(r.Title[at[0]:at[1]])
		switch token {
		case "truefrench", "vff", "vfq", "vf2":
			add("fr")
		case "vf":
			if at[0] >= metadataAt {
				add("fr")
			}
		case "multi":
			// An ordinary word in the work title is not an audio-track label.
			if at[0] >= metadataAt {
				add("multi")
			}
		default:
			if _, known := streamLanguageAliases[token]; known && at[0] >= metadataAt {
				add(token)
			}
		}
	}
	for _, value := range r.Languages {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '/' || r == '|' }) {
			add(part)
		}
	}
	return out
}

func streamLanguageLabel(languages []streamLanguage) string {
	labels, flags := []string{}, []string{}
	for _, item := range languages {
		label := "MULTI"
		if item.code != "multi" {
			// Short codes such as EN and SR are also service aliases in
			// aggregators. Keep audio names unambiguous without changing the
			// normalized language evidence used for filtering.
			label = display.English.Languages().Name(language.Make(item.code))
		}
		labels = append(labels, label)
		if item.flag != "" {
			flags = append(flags, item.flag)
		}
	}
	return strings.TrimSpace(strings.Join(labels, " / ") + " " + strings.Join(flags, " "))
}

func streamSize(bytes int64) string {
	if bytes <= 0 {
		return "Unknown size"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size, unit := float64(bytes), 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	precision := 2
	if size >= 100 {
		precision = 0
	} else if size >= 10 {
		precision = 1
	}
	return fmt.Sprintf("%.*f %s", precision, size, units[unit])
}

// Stremio selects URL playback before infoHash. Its ordinary fields and the
// structured hints read by AIOStreams preserve metadata without wrapping a
// debrid URL into another torrent playback request.
func playbackStream(stream Stream, rec model.AIORecord, service string, cached *bool, file *model.AIOFile, playbackURL string) Stream {
	stream.URL = playbackURL
	stream.Hints.Service = service
	stream.Hints.Cached = cached
	stream.Hints.BingeGroup += "|" + service
	if ParseRelease(rec.Title).Pack {
		stream.Hints.Duration = 0
	}
	if file != nil {
		stream.Hints.Filename = file.Path
		stream.Hints.Size = max(0, file.Size)
		stream.Hints.FolderName = rec.Title
		stream.Hints.FolderSize = max(0, rec.Size)
		// Web-ready requires a known MP4 file and an HTTPS origin. A pack's
		// aggregate duration is not the selected episode's duration.
		stream.Hints.NotWebReady = !strings.HasPrefix(playbackURL, "https://") || !strings.HasSuffix(strings.ToLower(file.Path), ".mp4")
	}
	return formatStream(stream, rec)
}

func streamBingeGroup(rec model.AIORecord, r Resource, quality string, observedLanguages []streamLanguage) string {
	// A group describes the work/source/quality/language, never the episode
	// or the spelling of an IMDb/TMDB/TVDB request alias.
	r.Season, r.Episode = nil, nil
	languages := []string{}
	for _, language := range observedLanguages {
		languages = append(languages, language.code)
	}
	slices.Sort(languages)
	return "aiosource|" + fingerprint(rec.Provider+"|"+streamResourceKey(r, []model.AIORecord{rec})+"|"+quality+"|"+strings.Join(languages, ","))
}

var presentationCodecBoundary = regexp.MustCompile(`(?i)\b(?:HEVC|AV1|[HX][ .]?26[45])\b`)

func presentationMetadataStart(title string) int {
	start := len(title)
	// Language names and words such as CAM can be part of the actual title.
	// Only unambiguous numbering, resolution or codecs delimit presentation tags.
	for _, marker := range []*regexp.Regexp{yearRE, seasonRE, crossRE, compiledPresentationTags[0], presentationCodecBoundary} {
		if at := marker.FindStringIndex(title); at != nil && at[0] < start {
			start = at[0]
		}
	}
	return start
}
