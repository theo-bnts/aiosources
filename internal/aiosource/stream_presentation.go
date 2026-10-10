package aiosource

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type streamLanguage struct {
	code, flag string
}

// Source APIs use ISO 639-1, ISO 639-2 and language names interchangeably.
// Keep this normalization at the presentation boundary: native values and
// correspondence evidence remain unchanged.
var streamLanguageAliases = func() map[string]streamLanguage {
	groups := []struct{ aliases, flag string }{
		{"fr fra fre french", "🇫🇷"}, {"en eng english", "🇬🇧"},
		{"es spa spanish", "🇪🇸"}, {"de ger deu german deutsch", "🇩🇪"},
		{"it ita italian", "🇮🇹"}, {"pt por portuguese", "🇵🇹"},
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
		if value == "" {
			return
		}
		language, known := streamLanguageAliases[value]
		if !known {
			language.code = string([]rune(value)[:min(8, len([]rune(value)))])
		}
		if !seen[language.code] {
			out = append(out, language)
			seen[language.code] = true
		}
	}
	metadataAt := len(r.Title)
	for _, marker := range []*regexp.Regexp{yearRE, seasonRE, crossRE, technicalRE} {
		if at := marker.FindStringIndex(r.Title); at != nil && at[0] < metadataAt {
			metadataAt = at[0]
		}
	}
	for _, at := range streamLanguageTokenRE.FindAllStringIndex(r.Title, -1) {
		token := strings.ToLower(r.Title[at[0]:at[1]])
		switch token {
		case "truefrench", "vff", "vfq", "vostfr":
			add("fr")
		case "vf":
			if at[0] >= metadataAt {
				add("fr")
			}
		case "multi":
			// Multiple tracks do not identify any particular language.
			add("multi")
		default:
			if _, known := streamLanguageAliases[token]; known && (len(token) > 2 || at[0] >= metadataAt) {
				add(token)
			}
		}
	}
	for _, value := range r.Languages {
		add(value)
	}
	return out
}

func streamLanguageLabel(languages []streamLanguage) string {
	labels, flags := []string{}, []string{}
	for _, language := range languages {
		labels = append(labels, strings.ToUpper(language.code))
		if language.flag != "" {
			flags = append(flags, language.flag)
		}
	}
	return strings.TrimSpace(strings.Join(labels, "/") + " " + strings.Join(flags, " "))
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
