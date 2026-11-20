package aiosource

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/unicode/norm"
)

type streamLanguage struct {
	code, flag string
}

type languageSpec struct {
	code, flag string
	isoCodes   []string // Safe 3-letter ISO / regional codes usable in both structured fields and scoped titles.
	structOnly []string // Structured-only ISO/compact aliases (e.g. collision-prone 3-letter codes like mal, cat, est).
	titleNames []string // Collision-free full names/endonyms usable in scoped titles and structured fields.
}

// Keep all 33 original v1 flags unchanged; extend coverage internationally for
// additional cinema and torrent languages without inventing ambiguous flags.
var streamLanguageCatalog = []languageSpec{
	{"fr", "🇫🇷", []string{"fra", "fre"}, []string{"frfr", "frca", "frbe", "frch"}, []string{"french", "francais", "quebecois"}},
	{"en", "🇬🇧", []string{"eng"}, []string{"enus", "engb", "enau", "enca"}, []string{"english", "anglais", "ingles", "englisch", "inglese", "angielski", "английский", "英語", "영어", "英语"}},
	{"es", "🇪🇸", []string{"spa", "esp"}, []string{"eses", "esla", "esmx", "es419", "lat"}, []string{"spanish", "espanol", "espagnol", "castellano", "latino", "spanisch", "spagnolo", "hiszpanski", "испанский"}},
	{"de", "🇩🇪", []string{"ger", "deu"}, []string{"dede", "deat", "dech"}, []string{"german", "deutsch", "allemand", "aleman", "tedesco", "niemiecki", "немецкий"}},
	{"it", "🇮🇹", []string{"ita"}, []string{"itit"}, []string{"italian", "italiano", "italien", "italienisch", "wloski", "итальянский"}},
	{"pt", "🇵🇹", []string{"por", "pob", "ptbr"}, []string{"ptpt", "pt-br", "pt-pt"}, []string{"portuguese", "portugues", "portugais", "portugiesisch", "portoghese", "brazilian", "brasileiro", "португальский"}},
	{"nl", "🇳🇱", []string{"dut", "nld"}, []string{"nlnl", "nlbe"}, []string{"dutch", "nederlands", "neerlandais", "vlaams", "flemish", "niederlandisch", "holandes"}},
	{"ru", "🇷🇺", []string{"rus"}, []string{"ruru"}, []string{"russian", "russe", "russisch", "rosyjski", "русский"}},
	{"ja", "🇯🇵", []string{"jpn", "jap"}, []string{"jajp", "jp"}, []string{"japanese", "japonais", "japones", "japanisch", "giapponese", "japonski", "японский", "日本語"}},
	{"ko", "🇰🇷", []string{"kor"}, []string{"kokr", "kr"}, []string{"korean", "coreen", "coreano", "koreanisch", "koreanski", "корейский", "한국어"}},
	{"zh", "🇨🇳", []string{"zho", "chi", "cmn", "yue"}, []string{"zhhans", "zhhant", "zhcn", "zhtw", "zhhk", "chs", "cht"}, []string{"chinese", "chinois", "chinesisch", "cinese", "chinski", "mandarin", "cantonese", "китайский", "中文"}},
	{"hi", "🇮🇳", []string{"hin"}, []string{"hiin"}, []string{"hindi", "हिन्दी", "हिंदी"}},
	{"ar", "🇸🇦", []string{"ara"}, []string{"arsa", "areg"}, []string{"arabic", "arabe", "arabisch", "arabski", "арабский", "العربية"}},
	{"pl", "🇵🇱", []string{"pol"}, []string{"plpl"}, []string{"polish", "polonais", "polski", "polnisch", "польский"}},
	{"tr", "🇹🇷", []string{"tur"}, []string{"trtr"}, []string{"turkish", "turkce", "turkisch", "турецкий"}},
	{"sv", "🇸🇪", []string{"swe"}, []string{"svse"}, []string{"swedish", "svenska", "suedois", "schwedisch", "шведский"}},
	{"no", "🇳🇴", []string{"nor"}, []string{"nono"}, []string{"norwegian", "norsk", "norvegien", "norwegisch", "норвежский"}},
	{"da", "🇩🇰", []string{"dan"}, []string{"dadk", "dk"}, []string{"danish", "dansk", "danois", "danisch", "датский"}},
	{"fi", "🇫🇮", nil, []string{"fin", "fifi"}, []string{"finnish", "suomi", "finnois", "finnisch", "финский"}},
	{"cs", "🇨🇿", []string{"ces", "cze"}, []string{"cscz", "cz"}, []string{"czech", "cestina", "tcheque", "tschechisch", "чешский"}},
	{"sk", "🇸🇰", []string{"slk", "slo"}, []string{"sksk"}, []string{"slovak", "slovencina", "slovaque", "slowakisch", "словацкий"}},
	{"hu", "🇭🇺", []string{"hun"}, []string{"huhu"}, []string{"hungarian", "magyar", "hongrois", "ungarisch", "венгерский"}},
	{"ro", "🇷🇴", []string{"rum"}, []string{"ron", "roro"}, []string{"romanian", "romana", "roumain", "rumanisch", "румынский"}},
	{"el", "🇬🇷", []string{"ell", "gre"}, []string{"elgr", "gr"}, []string{"greek", "ellinika", "griechisch", "ελληνικα", "ελληνικά", "греческий"}},
	{"he", "🇮🇱", []string{"heb"}, []string{"heil", "iw"}, []string{"hebrew", "ivrit", "hebreu", "hebraisch", "עברית", "иврит"}},
	{"th", "🇹🇭", []string{"tha"}, []string{"thth"}, []string{"thai", "thailandais", "ไทย", "ภาษาไทย", "тайский"}},
	{"vi", "🇻🇳", nil, []string{"vie", "vivn"}, []string{"vietnamese", "vietnamien", "vietnamesisch", "вьетнамский"}},
	{"id", "🇮🇩", nil, []string{"ind", "idid"}, []string{"indonesian", "indonesien", "indonesisch", "индонезийский"}},
	{"uk", "🇺🇦", []string{"ukr"}, []string{"ukua", "ua"}, []string{"ukrainian", "ukrainien", "ukrainisch", "українська", "украинский"}},
	{"bg", "🇧🇬", []string{"bul"}, []string{"bgbg"}, []string{"bulgarian", "bulgare", "bulgarisch", "български", "болгарский"}},
	{"hr", "🇭🇷", []string{"hrv"}, []string{"hrhr"}, []string{"croatian", "hrvatski", "croate", "kroatisch", "хорватский"}},
	{"sr", "🇷🇸", []string{"srp"}, []string{"scc", "srrs", "srlatn", "srcyrl"}, []string{"serbian", "srpski", "serbe", "serbisch", "српски", "сербский"}},
	{"sl", "🇸🇮", nil, []string{"slv", "slsi"}, []string{"slovenian", "slovene", "slovenscina", "slowenisch", "словенский"}},
	// Additional major international languages.
	{"fa", "🇮🇷", nil, []string{"fas", "per", "fair"}, []string{"persian", "farsi", "persan", "persisch", "فارسی"}},
	{"ur", "🇵🇰", []string{"urd"}, []string{"urpk"}, []string{"urdu", "ourdou", "اردو"}},
	{"bn", "🇧🇩", nil, []string{"ben", "bnbd", "bnin"}, []string{"bengali", "bangla", "বাংলা"}},
	{"ta", "🇮🇳", []string{"tam"}, []string{"tain"}, []string{"tamil", "tamoul", "தமிழ்"}},
	{"te", "🇮🇳", []string{"tel"}, []string{"tein"}, []string{"telugu", "తెలుగు"}},
	{"ml", "🇮🇳", nil, []string{"mal", "mlin"}, []string{"malayalam", "മലയാളം"}},
	{"kn", "🇮🇳", nil, []string{"kan", "knin"}, []string{"kannada", "ಕನ್ನಡ"}},
	{"mr", "🇮🇳", nil, []string{"mar", "mrin"}, []string{"marathi", "मराठी"}},
	{"gu", "🇮🇳", []string{"guj"}, []string{"guin"}, []string{"gujarati", "ગુજરાતી"}},
	{"pa", "🇮🇳", nil, []string{"pan", "pain"}, []string{"punjabi", "panjabi", "ਪੰਜਾਬੀ"}},
	{"ms", "🇲🇾", nil, []string{"msa", "may", "msmy"}, []string{"malay", "malais", "melayu"}},
	{"tl", "🇵🇭", nil, []string{"tgl", "fil", "tlph", "filph"}, []string{"tagalog", "filipino"}},
	{"ca", "", nil, []string{"cat", "caes"}, []string{"catalan", "catala"}},
	{"eu", "", nil, []string{"eus", "baq", "eues"}, []string{"basque", "euskera", "euskara"}},
	{"gl", "", nil, []string{"glg", "gles"}, []string{"galician", "galego"}},
	{"is", "🇮🇸", nil, []string{"isl", "ice", "isis"}, []string{"icelandic", "islenska", "islandais", "islandisch"}},
	{"et", "🇪🇪", nil, []string{"est", "etee"}, []string{"estonian", "eesti", "estonien", "estnisch"}},
	{"lv", "🇱🇻", nil, []string{"lav", "lvlv"}, []string{"latvian", "latviesu", "letton", "lettisch"}},
	{"lt", "🇱🇹", nil, []string{"lit", "ltlt"}, []string{"lithuanian", "lietuviu", "lituanien", "litauisch"}},
	{"ka", "🇬🇪", nil, []string{"kat", "geo", "kage"}, []string{"georgian", "georgien", "georgisch", "ქართული"}},
	{"hy", "🇦🇲", nil, []string{"hye", "arm", "hyam"}, []string{"armenian", "armenien", "armenisch", "հայերեն"}},
	{"az", "🇦🇿", nil, []string{"aze", "azaz"}, []string{"azerbaijani", "azeri", "azerbaidjanais"}},
	{"kk", "🇰🇿", nil, []string{"kaz", "kkkz"}, []string{"kazakh", "қазақ"}},
	{"sq", "🇦🇱", nil, []string{"sqi", "alb", "sqal"}, []string{"albanian", "shqip", "albanais", "albanisch"}},
	{"mk", "🇲🇰", nil, []string{"mkd", "mac", "mkmk"}, []string{"macedonian", "macedonien", "mazedonisch", "македонски"}},
	{"bs", "🇧🇦", nil, []string{"bos", "bsba"}, []string{"bosnian", "bosanski", "bosniaque", "bosnisch"}},
	{"ga", "🇮🇪", nil, []string{"gle", "gaie", "irish"}, []string{"gaeilge", "irlandais", "irisch"}},
	{"af", "🇿🇦", nil, []string{"afr", "afza"}, []string{"afrikaans"}},
	{"sw", "🇰🇪", nil, []string{"swa", "swke"}, []string{"swahili", "kiswahili"}},
	{"be", "🇧🇾", nil, []string{"bel", "beby"}, []string{"belarusian", "bielorusse", "беларуская"}},
	{"cy", "", nil, []string{"cym", "wel", "cygb"}, []string{"welsh", "cymraeg", "gallois"}},
	{"mn", "🇲🇳", nil, []string{"mon", "mnmn"}, []string{"mongolian", "монгол"}},
	{"ne", "🇳🇵", nil, []string{"nep", "nenp"}, []string{"nepali", "नेपाली"}},
	{"si", "🇱🇰", nil, []string{"sin", "silk"}, []string{"sinhala", "sinhalese", "සිංහල"}},
	{"km", "🇰🇭", nil, []string{"khm", "kmkh"}, []string{"khmer", "cambodian", "ភាសាខ្មែរ"}},
	{"lo", "🇱🇦", nil, []string{"lao", "lola"}, []string{"laotian", "ລາວ"}},
	{"my", "🇲🇲", nil, []string{"mya", "bur", "mymm"}, []string{"burmese", "myanmar", "မြန်မာ"}},
	{"uz", "🇺🇿", nil, []string{"uzb", "uzuz"}, []string{"uzbek", "ouzbek"}},
}

// Pure scene/P2P audio tags that never occur as ordinary movie-title words.
var titleUnambiguousAudio = map[string]string{
	// French scene/P2P tags.
	"truefrench": "fr", "vff": "fr", "vfq": "fr", "vfi": "fr", "vof": "fr",
	"vf2": "fr", "vfb": "fr", "voq": "fr", "vfstfr": "fr", "frenchdub": "fr", "frenchdubbed": "fr", "frdub": "fr", "multifrench": "fr",
	// International *dub compounds and unambiguous dubbing terms.
	"engdub": "en", "engdubbed": "en", "englishdub": "en", "englishdubbed": "en",
	"gerdub": "de", "germandub": "de", "germandubbed": "de", "dedub": "de",
	"espdub": "es", "latdub": "es", "spanishdub": "es", "spanishdubbed": "es", "castellanodub": "es",
	"itadub": "it", "italiandub": "it", "italiandubbed": "it",
	"dublado": "pt", "ptdub": "pt", "brdub": "pt", "ptbrdub": "pt",
	"pldub": "pl", "polishdub": "pl", "polishdubbed": "pl", "lektor": "pl", "lektorpl": "pl", "pllektor": "pl", "dubbingpl": "pl",
	"jpndub": "ja", "japdub": "ja",
	"rusdub": "ru", "rudub": "ru", "russiandub": "ru", "russiandubbed": "ru",
	"kordub": "ko", "koreandub": "ko",
	"chinesedub": "zh", "国语": "zh", "國語": "zh", "粤语": "zh", "粵語": "zh", "中配": "zh",
	"hindidub": "hi", "hindidubbed": "hi", "hindub": "hi",
	"tamildub": "ta", "tamildubbed": "ta", "telugudub": "te", "telugudubbed": "te",
	"malayalamdub": "ml", "kannadadub": "kn", "bengalidub": "bn", "punjabidub": "pa", "urdudub": "ur",
	"trdub": "tr", "turkishdub": "tr", "dublaj": "tr", "trdublaj": "tr", "turkcedublaj": "tr",
	"nldub": "nl", "dutchdub": "nl",
	"swedub": "sv", "nordub": "no", "dandub": "da", "findub": "fi",
	"czdub": "cs", "csdub": "cs", "czdabing": "cs", "skdub": "sk", "skdabing": "sk",
	"hudub": "hu", "rodub": "ro", "grdub": "el", "ardub": "ar", "hebdub": "he",
	"ukrdub": "uk", "hrdub": "hr", "srdub": "sr", "bgdub": "bg",
	"thaidub": "th", "thdub": "th", "vidub": "vi", "iddub": "id",
	"fadub": "fa", "persiandub": "fa", "farsidub": "fa",
}

// International compound subtitle tokens (never imply spoken audio, except vfstfr above).
var subtitleCompoundTokens = map[string]string{
	"vostfr": "fr", "subfrench": "fr", "stfr": "fr", "vfstfr": "fr",
	"frsub": "fr", "frsubs": "fr", "fresub": "fr", "fresubs": "fr", "subfr": "fr", "subfre": "fr",
	"vose": "es", "vostes": "es", "subesp": "es", "subes": "es", "espsub": "es", "espsubs": "es",
	"spanishsub": "es", "spanishsubs": "es", "subspanish": "es", "sublatino": "es", "latsub": "es",
	"latinosub": "es", "sublat": "es", "subcastellano": "es", "castellanosub": "es", "subtitulado": "es", "subtitulada": "es",
	"vosta": "en", "vosten": "en", "omeu": "en", "engsub": "en", "engsubs": "en", "subeng": "en", "ensub": "en", "ensubs": "en",
	"esub": "en", "esubs": "en", "englishsub": "en", "englishsubs": "en", "suben": "en",
	"vostit": "it", "subita": "it", "itasub": "it", "itasubs": "it", "italiansub": "it", "subit": "it",
	"vostde": "de", "gersub": "de", "gersubs": "de", "subger": "de", "desub": "de", "desubs": "de", "subde": "de", "germansub": "de",
	"vostpt": "pt", "ptsub": "pt", "ptsubs": "pt", "subpt": "pt", "ptbrsub": "pt", "ptbrsubs": "pt", "brsub": "pt", "brsubs": "pt", "braziliansub": "pt", "legendado": "pt",
	"vostpl": "pl", "plsub": "pl", "plsubs": "pl", "subpl": "pl", "napisy": "pl", "napisypl": "pl", "plnapisy": "pl",
	"vostnl": "nl", "nlsub": "nl", "nlsubs": "nl", "subnl": "nl", "dutchsub": "nl",
	"rusub": "ru", "rusubs": "ru", "subru": "ru", "russub": "ru",
	"trsub": "tr", "trsubs": "tr", "subtr": "tr", "altyazi": "tr", "altyazili": "tr", "traltyazi": "tr", "turkcealtyazi": "tr",
	"arsub": "ar", "arsubs": "ar", "subar": "ar", "arabicsub": "ar", "مترجم": "ar",
	"swesub": "sv", "swesubs": "sv", "subswe": "sv",
	"norsub": "no", "norsubs": "no", "subnor": "no",
	"dansub": "da", "dansubs": "da", "subdan": "da",
	"finsub": "fi", "finsubs": "fi", "subfin": "fi",
	"czsub": "cs", "cssub": "cs", "subcz": "cs",
	"sksub": "sk", "subsk": "sk",
	"husub": "hu", "subhu": "hu",
	"rosub": "ro", "subro": "ro",
	"grsub": "el", "elsub": "el", "subgr": "el",
	"hebsub": "he", "subheb": "he",
	"ukrsub": "uk", "subukr": "uk",
	"bgsub": "bg", "hrsub": "hr", "srsub": "sr",
	"korsub": "ko", "subkor": "ko",
	"jpnsub": "ja", "japsub": "ja", "subjpn": "ja",
	"chisub": "zh", "chssub": "zh", "chtsub": "zh",
	"中字": "zh", "中文字幕": "zh", "双语字幕": "zh", "中英双字": "zh", "中英雙字": "zh",
	"简中": "zh", "繁中": "zh", "简繁": "zh", "简繁内封": "zh", "簡中": "zh", "簡繁": "zh", "簡繁內封": "zh",
	"thsub": "th", "thaisub": "th", "thaisubs": "th", "subthai": "th",
	"visub": "vi", "vietsub": "vi", "vietsubs": "vi", "subviet": "vi",
	"idsub": "id", "indosub": "id", "indosubs": "id", "subindo": "id",
	"malaysub": "ms", "submalay": "ms", "tagalogsub": "tl",
	"hinsub": "hi", "fasub": "fa", "persub": "fa", "farsub": "fa", "persiansub": "fa",
	"multisub": "multi", "multisubs": "multi", "msub": "multi", "msubs": "multi",
}

// 2-letter uppercase tokens safe in scoped titles (never lowercase/TitleCase words like "de", "en", "it", "no", "ru",
// nor collision-prone Roman numerals / media acronyms like "VI", "HR", "SL").
var titleUppercaseTwoLetter = map[string]string{
	"fr": "fr", "vf": "fr", "en": "en", "es": "es", "de": "de", "it": "it",
	"pt": "pt", "nl": "nl", "ru": "ru", "ja": "ja", "jp": "ja", "ko": "ko", "kr": "ko",
	"zh": "zh", "hi": "hi", "ar": "ar", "pl": "pl", "tr": "tr", "sv": "sv", "no": "no",
	"da": "da", "dk": "da", "fi": "fi", "cs": "cs", "cz": "cs", "sk": "sk", "hu": "hu",
	"ro": "ro", "el": "el", "gr": "el", "he": "he", "th": "th", "id": "id",
	"uk": "uk", "ua": "uk", "bg": "bg", "sr": "sr",
}

// Uppercase 2-letter tokens in multi-word structured descriptions that are common non-language words.
var structuredPhraseIgnoredTwoLetter = map[string]bool{
	"no": true, "or": true, "it": true, "is": true, "in": true, "at": true, "on": true,
	"of": true, "to": true, "by": true, "as": true, "an": true, "be": true, "do": true,
	"so": true, "he": true, "my": true, "id": true, "hi": true, "vo": true, "ad": true,
	"hd": true, "sd": true, "tv": true, "dv": true, "ts": true, "av": true, "us": true, "uk": true,
}

var (
	streamLanguageByCode         map[string]streamLanguage
	structuredLanguageExact      map[string]streamLanguage // Atomic codes, ISO, compact BCP-47, release tags, CLDR single-word names.
	structuredLanguagePhraseWord map[string]streamLanguage // Safe single words inside multi-word structured phrases (CLDR >= 4 Latin / non-Latin + release tags).
	structuredLanguageMultiWord  map[string]streamLanguage // Multi-word CLDR names (e.g. "brazilian portuguese", "simplified chinese").
	titleScopedAudio             map[string]streamLanguage // Collision-free full names & 3-letter codes for scoped titles.
)

func init() {
	streamLanguageByCode = make(map[string]streamLanguage, len(streamLanguageCatalog))
	structuredLanguageExact = make(map[string]streamLanguage, 2400)
	structuredLanguagePhraseWord = make(map[string]streamLanguage, 2200)
	structuredLanguageMultiWord = make(map[string]streamLanguage, 256)
	titleScopedAudio = make(map[string]streamLanguage, 256)

	for _, spec := range streamLanguageCatalog {
		entry := streamLanguage{code: spec.code, flag: spec.flag}
		streamLanguageByCode[spec.code] = entry
		structuredLanguageExact[spec.code] = entry
		for _, code := range spec.isoCodes {
			structuredLanguageExact[code] = entry
			titleScopedAudio[code] = entry
		}
		for _, code := range spec.structOnly {
			structuredLanguageExact[code] = entry
		}
		for _, name := range spec.titleNames {
			folded := foldLanguageToken(name)
			structuredLanguageExact[folded] = entry
			structuredLanguagePhraseWord[folded] = entry
			titleScopedAudio[folded] = entry
		}
	}
	// Regional uppercase aliases used in structured descriptions (e.g. "FR + QC").
	structuredLanguageExact["qc"] = streamLanguageByCode["fr"]
	structuredLanguageExact["vf"] = streamLanguageByCode["fr"]

	// Unambiguous release audio tags also apply in structured fields (e.g. VFF/VFQ/VOF/VFI, Dublado, Lektor).
	for token, code := range titleUnambiguousAudio {
		if entry, ok := streamLanguageByCode[code]; ok {
			structuredLanguageExact[token] = entry
			structuredLanguagePhraseWord[token] = entry
		}
	}
	// Regional 2-letter aliases in structured exact values.
	for alias, code := range map[string]string{"cz": "cs", "jp": "ja", "gr": "el", "ua": "uk", "dk": "da", "kr": "ko"} {
		structuredLanguageExact[alias] = streamLanguageByCode[code]
	}

	// Populate localized language names across major CLDR display locales so
	// structured source fields in any language work out of the box.
	namers := []display.Namer{
		display.English.Languages(), display.French.Languages(), display.Spanish.Languages(),
		display.German.Languages(), display.Italian.Languages(), display.Portuguese.Languages(),
		display.Dutch.Languages(), display.Polish.Languages(), display.Russian.Languages(),
		display.Turkish.Languages(), display.Swedish.Languages(), display.Norwegian.Languages(),
		display.Danish.Languages(), display.Finnish.Languages(), display.Czech.Languages(),
		display.Slovak.Languages(), display.Hungarian.Languages(), display.Romanian.Languages(),
		display.Greek.Languages(), display.Hebrew.Languages(), display.Arabic.Languages(),
		display.Japanese.Languages(), display.Korean.Languages(), display.Chinese.Languages(),
		display.TraditionalChinese.Languages(), display.Hindi.Languages(), display.Ukrainian.Languages(),
		display.Bulgarian.Languages(), display.Croatian.Languages(), display.Serbian.Languages(),
		display.Slovenian.Languages(), display.Catalan.Languages(), display.Indonesian.Languages(),
		display.Vietnamese.Languages(), display.Thai.Languages(), display.Persian.Languages(),
		display.Self,
	}
	cldrSingle := make(map[string]string, 2200)
	cldrMulti := make(map[string]string, 256)
	ambiguousSingle := map[string]bool{}
	ambiguousMulti := map[string]bool{}
	for _, spec := range streamLanguageCatalog {
		tag := language.Make(spec.code)
		for _, namer := range namers {
			raw := namer.Name(tag)
			if raw == "" {
				continue
			}
			folded := foldLanguageToken(raw)
			if folded == "" {
				continue
			}
			if strings.ContainsAny(folded, " -()/,.·'") {
				if prev, exists := cldrMulti[folded]; exists && prev != spec.code {
					ambiguousMulti[folded] = true
				} else {
					cldrMulti[folded] = spec.code
				}
				continue
			}
			if isLatinText(folded) && utf8.RuneCountInString(folded) < 4 {
				continue
			}
			if prev, exists := cldrSingle[folded]; exists && prev != spec.code {
				ambiguousSingle[folded] = true
			} else {
				cldrSingle[folded] = spec.code
			}
		}
	}
	for key, code := range cldrSingle {
		if ambiguousSingle[key] {
			continue
		}
		entry := streamLanguageByCode[code]
		if _, exists := structuredLanguageExact[key]; !exists {
			structuredLanguageExact[key] = entry
		}
		if _, exists := structuredLanguagePhraseWord[key]; !exists {
			structuredLanguagePhraseWord[key] = entry
		}
	}
	for key, code := range cldrMulti {
		if ambiguousMulti[key] {
			continue
		}
		structuredLanguageMultiWord[key] = streamLanguageByCode[code]
	}
}

// foldLanguageToken lowercases and strips diacritics from Latin letters only,
// preserving non-Latin scripts such as Cyrillic й and Korean Hangul syllables.
func foldLanguageToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFC.String(s) {
		if unicode.Is(unicode.Latin, r) {
			for _, d := range norm.NFD.String(string(r)) {
				if !unicode.Is(unicode.Mn, d) {
					b.WriteRune(d)
				}
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isLatinText(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return false
		}
	}
	return true
}

func isAllCapsToken(s string) bool {
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return hasLetter
}

func streamLanguageForCode(code string) streamLanguage {
	if code == "multi" {
		return streamLanguage{code: "multi"}
	}
	if entry, ok := streamLanguageByCode[code]; ok {
		return entry
	}
	return streamLanguage{code: code}
}

type languageSet struct {
	items []streamLanguage
	seen  map[string]bool
}

func (s *languageSet) add(lang streamLanguage) {
	if lang.code == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if !s.seen[lang.code] {
		s.items = append(s.items, lang)
		s.seen[lang.code] = true
	}
}

func (s *languageSet) addCode(code string) {
	s.add(streamLanguageForCode(code))
}

func normalizeWorkOriginalLanguage(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if code == "" {
		return ""
	}
	if base, _, ok := strings.Cut(code, "-"); ok {
		code = base
	}
	if base, _, ok := strings.Cut(code, "_"); ok {
		code = base
	}
	switch code {
	case "xx", "und", "zxx", "mul", "mis":
		return ""
	case "cn":
		// TMDB uses "cn" for Cantonese.
		return "zh"
	case "sh":
		return "sr"
	case "mo":
		return "ro"
	case "iw":
		return "he"
	case "in":
		return "id"
	case "ji":
		return "yi"
	}
	normalized := NormalizeLanguage(code)
	if normalized == "" || normalized == "xx" || normalized == "und" || normalized == "zxx" || normalized == "mul" || normalized == "mis" {
		return ""
	}
	if len(normalized) == 3 && normalized >= "qaa" && normalized <= "qtz" {
		return ""
	}
	return normalized
}

func hasConcreteLanguage(items []streamLanguage) bool {
	for _, item := range items {
		if item.code != "" && item.code != "multi" {
			return true
		}
	}
	return false
}

// ReleaseLanguageCodes exposes the same bounded, normalized audio evidence used
// by Stremio presentation. Arbitrary native strings are never exported as tags.
func ReleaseLanguageCodes(record model.AIORecord) []string {
	audio, _ := streamLanguageEvidence(record)
	codes := make([]string, 0, len(audio))
	for _, item := range audio {
		codes = append(codes, item.code)
	}
	return codes
}

func streamLanguages(r model.AIORecord) []streamLanguage {
	audio, _ := streamLanguageEvidence(r)
	return audio
}

var (
	inlineTagsRE               = regexp.MustCompile(`(?i)\{Tags:([^}]+)\}`)
	trailingAltTitleRE         = regexp.MustCompile(`\s+\|\s+[^\[\{]+$`)
	trailingReleaseGroupRE     = regexp.MustCompile(`(?i)\b(?:x26[45]|h[ .]?26[45]|hevc|avc|av1|xvid|divx|vc[- .]?1|mpeg[ .-]?[24]|web[- .]?dl|webrip|blu[- .]?ray|bdrip|brrip|dvdrip|hdrip|hdtv|remux|aac(?:[ .]?[1257]\.[01])?|e?ac3(?:[ .]?[1257]\.[01])?|ddp?(?:[ .]?[1257]\.[01])?|dts(?:[- .]?hd)?(?:[- .]?ma)?(?:[ .]?[1257]\.[01])?|dtshdma|truehd|atmos|flac(?:[ .]?[1257]\.[01])?|opus(?:[ .]?[1257]\.[01])?|10bits?|2160p|1080[pi]|720p|576p|480p|[1257]\.1|[12]\.0)(?:\s+-\s+(\[[A-Za-z0-9][A-Za-z0-9._-]{0,28}\])|-(\[[A-Za-z0-9][A-Za-z0-9._-]{0,28}\]|[A-Za-z0-9][A-Za-z0-9_]{0,24}(?:-[A-Za-z0-9_]{1,16}){0,2}(?:\.[A-Za-z0-9_]{1,12}){0,2}))(?:\.(?:mkv|mp4|avi|mov|m4v|ts|m2ts|webm|wmv|flv|mpg|mpeg|iso|vob))?(?:\s+\([^)]{2,120}\))?$`)
	leadingBracketPrefixRE     = regexp.MustCompile(`^\s*\[[^\]]+\]\s*`)
	streamLanguageTokenRE      = regexp.MustCompile(`[\p{L}\p{N}]+`)
	hyphenatedBCP47TitleRE     = regexp.MustCompile(`\b(PT|ES|FR|ZH|EN|DE|IT|NL)-(BR|PT|LA|ES|MX|419|FR|CA|BE|CH|HANS|HANT|CN|TW|HK|US|GB|AT)\b`)
	technicalTwoLetterSkipRE   = regexp.MustCompile(`(?i)\b(?:HE[- .]AAC|DTS[- .]ES\b|DTS[- .]HD[- .]HR\b|HI[- .]Res\b|HI10P?\b|NO[- .](?:TAG|VF|VFF|VFQ|FR|FRENCH|DUB)\b)`)
	presentationCodecBoundary  = regexp.MustCompile(`(?i)\b(?:HEVC|AV1|AVC|[HX][ .]?26[45]|VC[- .]?1|MPEG[ .-]?[24])\b`)
	languageAudioCodecBoundary = regexp.MustCompile(`(?i)\b(?:MP3|AAC(?:[1257]\.[01])?|E?AC3(?:[1257]\.[01])?|DDP(?:[1257]\.[01])?|DTS|FLAC|TrueHD|900p)\b`)
	presentationSourceBoundary = regexp.MustCompile(`(?i)\b(?:WEB[- .]?DL|WEBRip|Blu[- .]?Ray|BDRip|BRRip|DVDRip|HDTV|PDTV|SDTV|XviD|DivX|REMUX|INTEGRALE|INTÉGRALE)\b`)
	languageSourceBoundary     = regexp.MustCompile(`(?i)\b(?:WEB[- .]?DL|WEBRip|Blu[- .]?Ray|BDRip|BRRip|DVDRip|HDRip|DVDScr|TVRip|SATRip|VHSRip|CAMRip|HDTV|PDTV|SDTV|XviD|DivX|REMUX|BDMV|4KLight|HDLight)\b`)
	presentationSeasonBoundary = regexp.MustCompile(`(?i)\b(?:season|saison|staffel|stagione|temporada)[ ._-]*\d{1,3}\b`)
	episodeMarkerRE            = regexp.MustCompile(`(?i)\b(?:S\d{1,3}E\d{1,3}|\d{1,2}x\d{1,3}|OAV|OVA|EP\d{1,3})\b`)
	tokenYearRE                = regexp.MustCompile(`^(?:19\d{2}|20\d{2})$`)
	tokenSeasonRE              = regexp.MustCompile(`(?i)^(?:S\d{1,3}(?:E\d{1,3})?|\d{1,2}x\d{1,3})$`)
)

var subtitleContextWords = map[string]bool{
	"sub": true, "subs": true, "subbed": true, "subtitle": true, "subtitles": true,
	"hardsub": true, "hardsubs": true, "softsub": true, "softsubs": true,
	"napisy": true, "soustitres": true, "untertitel": true,
	"sottotitoli": true, "subtitulos": true, "altyazi": true, "altyazili": true,
}

func isTechnicalMetadataMarker(marker *regexp.Regexp) bool {
	return marker != yearRE && marker != seasonRE && marker != crossRE && marker != presentationSeasonBoundary
}

func presentationMetadataStart(title string) int {
	offset := 0
	if lead := leadingBracketPrefixRE.FindStringIndex(title); lead != nil && lead[1] < len(title) {
		// Skip leading release-group or episode-prefix brackets (e.g. "[S5.E1] The IT Crowd")
		// so the title itself is never mistaken for post-metadata tags.
		offset = lead[1]
	}
	rest := title[offset:]
	start := len(rest)
	fromTechnical := false
	// Language names and words such as CAM can be part of the actual title.
	// Only unambiguous numbering, resolution, codecs or release sources delimit presentation tags.
	markers := []*regexp.Regexp{
		yearRE, seasonRE, crossRE, presentationSeasonBoundary,
		compiledPresentationTags[0], presentationCodecBoundary, presentationSourceBoundary,
	}
	for _, marker := range markers {
		if at := marker.FindStringIndex(rest); at != nil && at[0] < start {
			start = at[0]
			fromTechnical = isTechnicalMetadataMarker(marker)
		}
	}
	if fromTechnical && start > 0 {
		// When no year or season precedes the technical tag (e.g. "Angels.In.America.FRENCH.720p"),
		// walk backward across immediately adjacent ALL-CAPS release language/source tags.
		// Never walk backward before a year so titles like "In.French.2024.1080p" stay untouched.
		tokens := streamLanguageTokenRE.FindAllStringIndex(rest[:start], -1)
		for i := len(tokens) - 1; i >= 0; i-- {
			at := tokens[i]
			// Only walk across simple separators (spaces, dots, underscores, hyphens, brackets).
			between := rest[at[1]:start]
			if strings.Trim(between, " ._-[]()") != "" {
				break
			}
			raw := rest[at[0]:at[1]]
			folded := foldLanguageToken(raw)
			isMixedMulti := folded == "multi" && (strings.ContainsFunc(raw, unicode.IsUpper) && raw != strings.ToUpper(raw[:1])+strings.ToLower(raw[1:]))
			if isAllCapsToken(raw) || isMixedMulti {
				if isBackwardWalkableReleaseToken(folded) {
					start = at[0]
					continue
				}
			}
			break
		}
	}
	return offset + start
}

// languageMetadataStart locates where release language tags begin in a title.
// Unlike presentationMetadataStart, it anchors on technical tags (resolution, codec, source)
// and walks backward across contiguous release/language tokens so episode titles placed
// between S01E02 and 1080p (e.g. "Show S01E02 Welsh Nut Custard 1080p") are never scanned.
func languageMetadataStart(title string) int {
	offset := 0
	if lead := leadingBracketPrefixRE.FindStringIndex(title); lead != nil && lead[1] < len(title) {
		offset = lead[1]
	}
	rest := title[offset:]
	techStart := len(rest)
	techMarkers := []*regexp.Regexp{
		compiledPresentationTags[0], presentationCodecBoundary, languageAudioCodecBoundary, languageSourceBoundary,
	}
	for _, marker := range techMarkers {
		if at := marker.FindStringIndex(rest); at != nil && at[0] < techStart {
			techStart = at[0]
		}
	}
	if techStart == len(rest) {
		// No technical markers present (e.g. "Movie (2024) FRENCH"): fall back to year/season boundary.
		fallback := len(rest)
		for _, marker := range []*regexp.Regexp{yearRE, seasonRE, crossRE, presentationSeasonBoundary} {
			if at := marker.FindStringIndex(rest); at != nil && at[0] < fallback {
				fallback = at[0]
			}
		}
		return offset + fallback
	}
	if techStart == 0 {
		return offset
	}

	// If a movie release year precedes techStart with no episode marker between the year
	// and techStart, everything from the last year onward is release metadata (never an episode title).
	// Requiring yr[0] > 0 avoids treating titles starting with a year (e.g. "2001 A Space Odyssey") as boundaries.
	if years := yearRE.FindAllStringIndex(rest[:techStart], -1); len(years) > 0 {
		lastYear := years[len(years)-1]
		if lastYear[0] > 0 && !episodeMarkerRE.MatchString(rest[lastYear[1]:techStart]) {
			return offset + lastYear[0]
		}
	}

	start := techStart
	tokens := streamLanguageTokenRE.FindAllStringIndex(rest[:techStart], -1)
	i := len(tokens) - 1
	// Phase 1: Walk backward across unambiguous or ALL-CAPS release/language tags
	// and explicit paired "<Lang/Multi/Dual> <Dub|Sub|Audio>" phrases.
	for i >= 0 {
		at := tokens[i]
		if strings.Trim(rest[at[1]:start], " ._-[]()") != "" {
			break
		}
		raw := rest[at[0]:at[1]]
		folded := foldLanguageToken(raw)
		if isUnconditionalBackwardToken(raw, folded) {
			start = at[0]
			i--
			continue
		}
		if (folded == "dub" || folded == "dubs" || folded == "sub" || folded == "subs" || folded == "audio") && i > 0 {
			prev := tokens[i-1]
			if strings.Trim(rest[prev[1]:at[0]], " ._-/") == "" {
				prevRaw := rest[prev[0]:prev[1]]
				prevFolded := foldLanguageToken(prevRaw)
				if isTitleLanguageOrMultiToken(prevRaw, prevFolded) {
					start = prev[0]
					i -= 2
					continue
				}
			}
		}
		break
	}
	// Phase 2: Tentatively walk across TitleCase/lowercase language or release tags
	// (e.g. "Movie (2026) Tamil HQ HDRip", "RRR.2022.VOSTFR.Telugu-Hindi.2160p"),
	// committing ONLY if the run is immediately preceded by a year or season marker.
	if i >= 0 {
		tentStart := start
		j := i
		for j >= 0 {
			at := tokens[j]
			if strings.Trim(rest[at[1]:tentStart], " ._-[]()") != "" {
				break
			}
			raw := rest[at[0]:at[1]]
			folded := foldLanguageToken(raw)
			if tokenYearRE.MatchString(raw) || tokenSeasonRE.MatchString(raw) {
				if tentStart < start {
					start = tentStart
				}
				break
			}
			if isBackwardWalkableReleaseToken(folded) && (isAllCapsToken(raw) || !isOnlyTwoLetterCode(folded)) {
				tentStart = at[0]
				j--
				continue
			}
			if folded == "dub" || folded == "dubs" || folded == "sub" || folded == "subs" || folded == "audio" {
				tentStart = at[0]
				j--
				continue
			}
			break
		}
	}
	return offset + start
}

func isOnlyTwoLetterCode(folded string) bool {
	if _, ok := titleUppercaseTwoLetter[folded]; !ok {
		return false
	}
	if _, ok := titleScopedAudio[folded]; ok {
		return false
	}
	if _, ok := titleUnambiguousAudio[folded]; ok {
		return false
	}
	if _, ok := subtitleCompoundTokens[folded]; ok {
		return false
	}
	return true
}

func isUnconditionalBackwardToken(raw, folded string) bool {
	if _, ok := titleUnambiguousAudio[folded]; ok {
		return true
	}
	if _, ok := subtitleCompoundTokens[folded]; ok {
		return true
	}
	switch folded {
	case "multi", "multilingual", "multilang", "multiaudio", "dualaudio", "multilingue",
		"vost", "omu", "omeu", "fansub", "fastsub", "subbed":
		return true
	}
	if isAllCapsToken(raw) && isBackwardWalkableReleaseToken(folded) {
		return true
	}
	return false
}

func isTitleLanguageOrMultiToken(raw, folded string) bool {
	switch folded {
	case "multi", "multilingual", "multilang", "multiaudio", "dual", "dualaudio", "multilingue":
		return true
	}
	if _, ok := titleScopedAudio[folded]; ok {
		return true
	}
	if isAllCapsToken(raw) {
		if _, ok := titleUppercaseTwoLetter[folded]; ok {
			return true
		}
	}
	return false
}

func isBackwardWalkableReleaseToken(folded string) bool {
	switch folded {
	case "multi", "multilingual", "multilang", "multiaudio", "dual", "dualaudio", "multilingue",
		"sd", "hd", "hq", "uhd", "ad", "ld", "cp", "vo", "vos", "ov", "vost", "omu", "omeu", "subbed", "custom", "complete", "integrale", "hdlight", "mhd", "light",
		"repack", "proper", "fastsub", "fansub", "subforced", "forced", "uncensored", "extended", "unrated",
		"remastered", "final", "limited", "internal", "readnfo", "hybrid", "dc", "theatrical", "doc", "oav", "ova",
		"web", "dvd", "dvdr", "dvd5", "dvd9", "bd", "iso", "raw", "opus",
		"imax", "3d", "sbs", "hsbs", "ou", "hou", "tob", "hdr", "hdr10", "hdr10plus", "dv", "sdr", "10bit", "8bit", "10bits", "8bits",
		"cr", "amzn", "nf", "dsnp", "atvp", "hmax", "hulu", "pmtp", "pcok", "adn", "wetv", "tver",
		"zee5", "hidive", "funi", "bili", "viki", "mubi", "crav", "itvx", "bbc", "cbc", "arte",
		"ocs", "tf1", "tf1p", "m6", "m6p", "ftv", "ftvp", "frtv", "cnlp", "stsm", "roku", "stan", "sky", "now", "canal", "mycanal", "cplus", "salto", "batch":
		return true
	}
	if _, ok := titleUnambiguousAudio[folded]; ok {
		return true
	}
	if _, ok := titleScopedAudio[folded]; ok {
		return true
	}
	if _, ok := titleUppercaseTwoLetter[folded]; ok {
		return true
	}
	if _, ok := subtitleCompoundTokens[folded]; ok {
		return true
	}
	return false
}

// nativeLanguageEvidence parses only the record's own title and native fields,
// excluding cross-source propagated languages and TMDB original_language inference
// so correspondence computation never feeds back into itself or conflicts with explicit peer audio.
func nativeLanguageEvidence(r model.AIORecord, extraTitles ...string) ([]streamLanguage, []streamLanguage) {
	var audio, subs languageSet
	parseTitleLanguages(r.Title, &audio, &subs)
	for _, extra := range extraTitles {
		if extra != "" && extra != r.Title {
			parseTitleLanguages(extra, &audio, &subs)
		}
	}
	for _, value := range r.Languages {
		parseStructuredLanguageValue(value, false, &audio, &subs, nil)
	}
	for _, value := range r.SubtitleLanguages {
		parseStructuredLanguageValue(value, true, &audio, &subs, nil)
	}
	return audio.items, subs.items
}

func streamLanguageEvidence(r model.AIORecord, extraTitles ...string) ([]streamLanguage, []streamLanguage) {
	var audio, subs languageSet
	originalVersion := parseTitleLanguages(r.Title, &audio, &subs)
	for _, extra := range extraTitles {
		if extra != "" && extra != r.Title {
			if parseTitleLanguages(extra, &audio, &subs) {
				originalVersion = true
			}
		}
	}
	for _, value := range r.Languages {
		parseStructuredLanguageValue(value, false, &audio, &subs, &originalVersion)
	}
	for _, value := range r.SubtitleLanguages {
		parseStructuredLanguageValue(value, true, &audio, &subs, nil)
	}
	for _, code := range r.PropagatedLanguages {
		if code == "multi" {
			audio.addCode("multi")
		} else if normalized := NormalizeLanguage(code); normalized != "" {
			audio.addCode(normalized)
		}
	}
	for _, code := range r.PropagatedSubtitleLanguages {
		if code == "multi" {
			subs.addCode("multi")
		} else if normalized := NormalizeLanguage(code); normalized != "" {
			subs.addCode(normalized)
		}
	}
	// When no explicit audio language (native, MULTI/DUAL or cross-source peer) is known,
	// combine explicit Original Version or concrete subtitle-only tags (e.g. VOSTFR,
	// SUBFRENCH, VOSE, OmU, OV, VO, ENGSUB) with TMDB's original_language.
	if len(audio.items) == 0 {
		if orig := normalizeWorkOriginalLanguage(r.OriginalLanguage); orig != "" {
			if originalVersion || hasConcreteLanguage(subs.items) {
				audio.addCode(orig)
			}
		}
	}
	return audio.items, subs.items
}

type consumedBCP47 struct {
	start, end int
	code       string
}

func parseTitleLanguages(title string, audio, subs *languageSet) bool {
	if title == "" {
		return false
	}
	originalVersion := false
	// 1. Parse explicit inline {Tags:...;A=...;S=...;F=...;} blocks (used by some anime indexers)
	// and remove them so subtitle codes never leak into audio evidence.
	cleaned := title
	if strings.IndexByte(cleaned, '{') >= 0 {
		cleaned = inlineTagsRE.ReplaceAllStringFunc(cleaned, func(match string) string {
			inner := inlineTagsRE.FindStringSubmatch(match)
			if len(inner) == 2 {
				parseInlineTagBlock(inner[1], audio, subs)
			}
			return " "
		})
	}
	// 2. Strip trailing alternative titles separated by " | " (common on anime indexers).
	if strings.Contains(cleaned, " | ") {
		cleaned = trailingAltTitleRE.ReplaceAllString(cleaned, "")
	}
	// 3. Mask known technical compounds containing ISO collisions (HE-AAC, DTS-ES, DTS-HD-HR, HI-Res, NO-VF).
	cleaned = technicalTwoLetterSkipRE.ReplaceAllStringFunc(cleaned, func(m string) string {
		return strings.Repeat(" ", len(m))
	})

	metadataAt := languageMetadataStart(cleaned)

	// Identify trailing "-GROUP" (plus optional appended "(Localized Title)") right after a
	// technical tag so release group names (e.g. -iND, -AZE, -RoN, -RO, -DIY-GR, -Dutch_67)
	// and localized titles appended after the group are not mistaken for audio languages.
	groupStart := len(cleaned)
	if strings.IndexByte(cleaned, '-') >= 0 {
		if m := trailingReleaseGroupRE.FindStringSubmatchIndex(cleaned); m != nil {
			gStart, gEnd := m[2], m[3]
			if gStart < 0 {
				gStart, gEnd = m[4], m[5]
			}
			if gStart >= metadataAt && isTrailingReleaseGroup(cleaned[gStart:gEnd]) {
				groupStart = gStart
			}
		}
	}

	// 4. Identify explicit hyphenated uppercase BCP-47 tags after metadataAt (e.g. [PT-BR], [ES-LA], [ZH-HANS]).
	var bcpTags []consumedBCP47
	if strings.IndexByte(cleaned, '-') >= 0 {
		for _, at := range hyphenatedBCP47TitleRE.FindAllStringIndex(cleaned, -1) {
			if at[0] < metadataAt || at[0] >= groupStart {
				continue
			}
			tag := strings.ToLower(cleaned[at[0]:at[1]])
			code := NormalizeLanguage(tag)
			if code == "" {
				if base, _, ok := strings.Cut(tag, "-"); ok {
					code = titleUppercaseTwoLetter[base]
				}
			}
			if code == "" {
				continue
			}
			bcpTags = append(bcpTags, consumedBCP47{start: at[0], end: at[1], code: code})
		}
	}

	// 5. Tokenize and compute prefix/suffix subtitle scopes so lists like
	// "SUB ITA ENG GER SPA FRE" and "(English, Russian/Русский, German/Deutsch Subs)"
	// route all subtitle languages cleanly without stealing preceding audio tracks.
	indices := streamLanguageTokenRE.FindAllStringIndex(cleaned, -1)
	foldedTokens := make([]string, len(indices))
	rawTokens := make([]string, len(indices))
	bcpOfToken := make([]int, len(indices))
	for i, at := range indices {
		rawTokens[i] = cleaned[at[0]:at[1]]
		foldedTokens[i] = foldLanguageToken(rawTokens[i])
		bcpOfToken[i] = -1
		for bi, tag := range bcpTags {
			if at[0] >= tag.start && at[1] <= tag.end {
				bcpOfToken[i] = bi
				break
			}
		}
	}

	subMask := computeSubtitleTokenMask(cleaned, indices, rawTokens, foldedTokens, bcpOfToken, metadataAt, groupStart)

	bcpEmitted := make([]bool, len(bcpTags))
	for i, at := range indices {
		if bi := bcpOfToken[i]; bi >= 0 {
			if !bcpEmitted[bi] {
				bcpEmitted[bi] = true
				if subMask[i] {
					subs.addCode(bcpTags[bi].code)
				} else {
					audio.addCode(bcpTags[bi].code)
				}
			}
			continue
		}
		raw := rawTokens[i]
		tok := foldedTokens[i]

		// Unambiguous release audio tokens are safe anywhere in the title.
		if code, ok := titleUnambiguousAudio[tok]; ok {
			if tok == "multifrench" {
				audio.addCode("multi")
			}
			audio.addCode(code)
			if subCode, alsoSub := subtitleCompoundTokens[tok]; alsoSub {
				subs.addCode(subCode)
			}
			continue
		}
		// Unambiguous compound subtitle tokens (VOSTFR, VOSTA, SUBFRENCH, VOSE, SUBITA, ENGSUB, NAPISY, LEGENDADO, etc.).
		if subCode, ok := subtitleCompoundTokens[tok]; ok {
			if tok != "vose" || at[0] >= metadataAt || isAllCapsToken(raw) {
				subs.addCode(subCode)
				if strings.HasPrefix(tok, "vost") || tok == "vose" || tok == "omeu" {
					originalVersion = true
				}
			}
			continue
		}

		if at[0] < metadataAt {
			continue
		}

		// Short hyphenated subtitle markers like "M-SUB" / "M-SUBS" / "E-SUB" / "E-SUBS".
		if (tok == "m" || tok == "e") && i+1 < len(indices) &&
			(foldedTokens[i+1] == "sub" || foldedTokens[i+1] == "subs") &&
			cleaned[at[1]:indices[i+1][0]] == "-" {
			if tok == "m" {
				subs.addCode("multi")
			} else {
				subs.addCode("en")
			}
			continue
		}

		if at[0] >= groupStart {
			continue
		}

		// Explicit Original Version markers inside release metadata (VO, OV, VOS, VOST, OmU, Subbed, Fansub).
		switch tok {
		case "vost", "omu", "omeu", "fansub", "fastsub", "subbed":
			originalVersion = true
			continue
		case "vo", "ov", "vos":
			if isAllCapsToken(raw) && !isFollowedByHyphenDigits(cleaned, at[1]) {
				originalVersion = true
			}
			continue
		}

		subContext := subMask[i]

		// Multi / Dual audio markers (never inside trailing -GROUP or appended localized title).
		if tok == "multi" || tok == "multilingual" || tok == "multilang" || tok == "multiaudio" || tok == "dualaudio" || tok == "multilingue" {
			if subContext {
				subs.addCode("multi")
			} else {
				audio.addCode("multi")
			}
			continue
		}
		if tok == "dual" {
			// Guard against "Dual Layer" / "Dual Disc" media descriptors.
			if i+1 < len(indices) && (foldedTokens[i+1] == "layer" || foldedTokens[i+1] == "disc") {
				continue
			}
			if subContext {
				subs.addCode("multi")
			} else {
				audio.addCode("multi")
			}
			continue
		}

		// Collision-free full language names & unambiguous 3-letter codes.
		if lang, ok := titleScopedAudio[tok]; ok {
			if subContext {
				subs.add(lang)
			} else {
				audio.add(lang)
			}
			continue
		}

		// 2-letter codes in titles MUST be ALL-CAPS, not followed by "-<digits>" (e.g. SR-71, AR-15).
		if isAllCapsToken(raw) && !isFollowedByHyphenDigits(cleaned, at[1]) {
			if code, ok := titleUppercaseTwoLetter[tok]; ok {
				if subContext {
					subs.addCode(code)
				} else {
					audio.addCode(code)
				}
			}
		}
	}
	return originalVersion
}

func isFollowedByHyphenDigits(s string, end int) bool {
	if end+1 >= len(s) || s[end] != '-' {
		return false
	}
	return s[end+1] >= '0' && s[end+1] <= '9'
}

func computeSubtitleTokenMask(cleaned string, indices [][]int, rawTokens, foldedTokens []string, bcpOfToken []int, metadataAt, groupStart int) []bool {
	subMask := make([]bool, len(indices))
	isLangAt := func(idx int) bool {
		if idx < 0 || idx >= len(indices) || indices[idx][0] < metadataAt || indices[idx][0] >= groupStart {
			return false
		}
		if bcpOfToken[idx] >= 0 {
			return true
		}
		tok := foldedTokens[idx]
		if tok == "dual" && idx+1 < len(indices) && (foldedTokens[idx+1] == "layer" || foldedTokens[idx+1] == "disc") {
			return false
		}
		if isAllCapsToken(rawTokens[idx]) && isFollowedByHyphenDigits(cleaned, indices[idx][1]) {
			if _, onlyTwo := titleUppercaseTwoLetter[tok]; onlyTwo {
				if _, scoped := titleScopedAudio[tok]; !scoped {
					return false
				}
			}
		}
		return isTitleLanguageOrMultiToken(rawTokens[idx], tok)
	}
	markBCP := func(idx int) {
		subMask[idx] = true
		if bi := bcpOfToken[idx]; bi >= 0 {
			for j := idx - 1; j >= 0 && bcpOfToken[j] == bi; j-- {
				subMask[j] = true
			}
			for j := idx + 1; j < len(indices) && bcpOfToken[j] == bi; j++ {
				subMask[j] = true
			}
		}
	}

	for k := range indices {
		if indices[k][0] < metadataAt || indices[k][0] >= groupStart {
			continue
		}
		if !subtitleContextWords[foldedTokens[k]] {
			continue
		}

		// Check if tightly bound as a suffix to k-1 via '-' or '_' (e.g. "Eng-Sub", "JPN_SUB", "Multi-Subs").
		tightlyBoundToPrev := false
		if k > 0 && isLangAt(k-1) {
			sepPrev := cleaned[indices[k-1][1]:indices[k][0]]
			if sepPrev == "-" || sepPrev == "_" {
				tightlyBoundToPrev = true
			}
		}

		// Case 1: Prefix keyword ("SUB ITA ENG GER SPA FRE", "Sub. Español", "HARD SUB (ZH-HANS)", "Napisy PL").
		isPrefix := false
		if !tightlyBoundToPrev && k+1 < len(indices) && isLangAt(k+1) {
			sepNext := cleaned[indices[k][1]:indices[k+1][0]]
			if !strings.ContainsAny(sepNext, "])|;") && strings.Trim(sepNext, " ._-([:/+&") == "" {
				isPrefix = true
			}
		}
		if isPrefix {
			for j := k + 1; j < len(indices) && isLangAt(j); j++ {
				if j > k+1 {
					sep := cleaned[indices[j-1][1]:indices[j][0]]
					sameBCP := bcpOfToken[j] >= 0 && bcpOfToken[j] == bcpOfToken[j-1]
					if !sameBCP {
						if strings.ContainsAny(sep, "])|;") || strings.Trim(sep, " ._,/+&") != "" {
							break
						}
					}
				}
				markBCP(j)
			}
			continue
		}

		// Case 2: Suffix keyword ("Eng-Subs", "JPN_SUB", "NL SUBS", "(English, Russian/Русский, German/Deutsch Subs)").
		if k > 0 && isLangAt(k-1) {
			sepPrev := cleaned[indices[k-1][1]:indices[k][0]]
			if strings.Trim(sepPrev, " ._-/:") == "" {
				markBCP(k - 1)
				for j := k - 2; j >= 0 && isLangAt(j); j-- {
					sameBCP := bcpOfToken[j] >= 0 && bcpOfToken[j] == bcpOfToken[j+1]
					if !sameBCP {
						sep := cleaned[indices[j][1]:indices[j+1][0]]
						if !strings.ContainsAny(sep, ",/+&") || strings.Trim(sep, " ,/+&") != "" {
							break
						}
					}
					markBCP(j)
				}
			}
		}
	}
	return subMask
}

func isTrailingReleaseGroup(groupRaw string) bool {
	trimmed := strings.Trim(groupRaw, "[]")
	folded := foldLanguageToken(trimmed)
	// Keep explicit trailing language/subtitle markers such as "-VOSTFR", "-Multisub", "-NLSubs", "-EngDub", "-OmU".
	if strings.Contains(folded, "sub") || strings.Contains(folded, "vost") || strings.Contains(folded, "dub") || folded == "omu" || folded == "omeu" {
		return false
	}
	// Never treat an explicit single-token full language name (>=4 runes) or multi/unambiguous tag (e.g. "-FRENCH", "-ITALIAN", "-MULTI") as a release group.
	// Short 3-letter release group tags like "-SRP" remain treated as release groups.
	if utf8.RuneCountInString(folded) >= 4 {
		if _, ok := titleScopedAudio[folded]; ok {
			return false
		}
	}
	if _, ok := titleUnambiguousAudio[folded]; ok {
		return false
	}
	if _, ok := subtitleCompoundTokens[folded]; ok {
		return false
	}
	if folded == "multi" || folded == "dual" {
		return false
	}
	if strings.ContainsAny(trimmed, ".-") {
		parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '.' || r == '-' })
		for _, part := range parts {
			p := foldLanguageToken(part)
			if p == "" {
				continue
			}
			if _, ok := titleScopedAudio[p]; ok {
				return false
			}
			if _, ok := titleUnambiguousAudio[p]; ok {
				return false
			}
			if _, ok := subtitleCompoundTokens[p]; ok {
				return false
			}
			switch p {
			case "multi", "dual", "x264", "x265", "h264", "h265", "hevc", "avc", "av1",
				"aac", "ac3", "eac3", "ddp", "dts", "atmos", "truehd", "flac", "opus",
				"1080p", "720p", "2160p", "480p", "576p", "web", "dl", "webdl", "webrip", "bluray", "remux":
				return false
			}
		}
	}
	return true
}

func parseInlineTagBlock(block string, audio, subs *languageSet) {
	for _, entry := range strings.Split(block, ";") {
		entry = strings.TrimSpace(entry)
		key, values, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		var target *languageSet
		switch key {
		case "A":
			target = audio
		case "S", "F":
			target = subs
		default:
			continue
		}
		for _, item := range strings.Split(values, ",") {
			if lang, ok := lookupStructuredExact(item); ok {
				target.add(lang)
			}
		}
	}
}

func parseStructuredLanguageValue(value string, subtitleOnly bool, audio, subs *languageSet, originalVersion *bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if !subtitleOnly {
		folded := foldLanguageToken(value)
		for _, prefix := range []string{"subtitles:", "subtitle:", "sous-titres:", "sous-titre:", "subs:", "sub:", "untertitel:", "subtitulos:", "sottotitoli:", "napisy:"} {
			if strings.HasPrefix(folded, prefix) {
				subtitleOnly = true
				break
			}
		}
	}
	// Split explicit lists separated by commas, semicolons or pipes.
	// Keep parentheses intact inside a segment first so phrases like
	// "Multi (Français inclus)" or "Français (VFF/TrueFrench)" parse cleanly.
	segments := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})
	for _, seg := range segments {
		parseStructuredSegment(seg, subtitleOnly, audio, subs, originalVersion)
	}
}

func parseStructuredSegment(seg string, subtitleOnly bool, audio, subs *languageSet, originalVersion *bool) {
	seg = strings.TrimSpace(seg)
	if seg == "" {
		return
	}
	// First try exact atomic lookup on the whole segment (e.g. "fr", "fr-FR", "eng", "JAPONAIS", "pt_BR", "VOSTFR").
	if handleStructuredExact(seg, subtitleOnly, audio, subs, originalVersion) {
		return
	}
	// If the segment contains slash/plus lists without prose (e.g. "fr/en", "FR + EN", "VFF/TrueFrench"),
	// split on / and + and try exact lookup on each part.
	if strings.ContainsAny(seg, "/+&") && !strings.ContainsAny(seg, "()") {
		parts := strings.FieldsFunc(seg, func(r rune) bool { return r == '/' || r == '+' || r == '&' })
		matchedAny := false
		for _, part := range parts {
			if handleStructuredExact(part, subtitleOnly, audio, subs, originalVersion) {
				matchedAny = true
			}
		}
		if matchedAny {
			return
		}
	}
	// Check exact multi-word CLDR language name (e.g. "Brazilian Portuguese", "Simplified Chinese", "Chinois simplifié").
	foldedWhole := foldLanguageToken(seg)
	if lang, ok := structuredLanguageMultiWord[foldedWhole]; ok {
		if subtitleOnly {
			subs.add(lang)
		} else {
			audio.add(lang)
		}
		return
	}

	// Descriptive structured label (e.g. "Multi (Français inclus)", "Français (VFF/TrueFrench)",
	// "Multi VF2 (FR + QC inclus)", "Québecois (VFQ/French)", "Español (Latino)", "Subtitles: English").
	segSubtitleOnly := subtitleOnly
	if !segSubtitleOnly {
		if originalVersion != nil {
			for _, phrase := range []string{"version originale", "original version", "version original", "versione originale", "originalversion"} {
				if strings.Contains(foldedWhole, phrase) {
					*originalVersion = true
					break
				}
			}
		}
		if strings.Contains(foldedWhole, "sous-titr") || strings.Contains(foldedWhole, "subtitl") || strings.Contains(foldedWhole, "subbed") {
			segSubtitleOnly = true
		}
	}
	tokens := streamLanguageTokenRE.FindAllString(seg, -1)
	for _, raw := range tokens {
		tok := foldLanguageToken(raw)
		if tok == "" || tok == "und" || tok == "unknown" || tok == "none" || tok == "muet" || tok == "silent" {
			continue
		}
		if tok == "vost" || tok == "omu" || tok == "fansub" || tok == "fastsub" || tok == "subbed" ||
			(isAllCapsToken(raw) && (tok == "vo" || tok == "ov" || tok == "vos")) {
			if !subtitleOnly && originalVersion != nil {
				*originalVersion = true
			}
			continue
		}
		if tok == "vo" {
			continue
		}
		if tok == "multi" || tok == "multilingual" || tok == "multilang" || tok == "multiaudio" || tok == "dual" || tok == "dualaudio" || tok == "multilingue" {
			if segSubtitleOnly {
				subs.addCode("multi")
			} else {
				audio.addCode("multi")
			}
			continue
		}
		if subCode, ok := subtitleCompoundTokens[tok]; ok {
			subs.addCode(subCode)
			if !subtitleOnly && originalVersion != nil && (strings.HasPrefix(tok, "vost") || tok == "vose" || tok == "omeu") {
				*originalVersion = true
			}
			if audioCode, alsoAudio := titleUnambiguousAudio[tok]; alsoAudio && !segSubtitleOnly {
				audio.addCode(audioCode)
			}
			continue
		}
		if lang, ok := structuredLanguagePhraseWord[tok]; ok {
			if segSubtitleOnly {
				subs.add(lang)
			} else {
				audio.add(lang)
			}
			continue
		}
		// Inside a multi-word description, only accept 2-letter codes when explicitly ALL-CAPS
		// and not common short words (e.g. "(FR + QC inclus)" matches FR/QC, while "not a language" / "Audio en français" ignores "en").
		if isAllCapsToken(raw) && !structuredPhraseIgnoredTwoLetter[tok] {
			if lang, ok := structuredLanguageExact[tok]; ok {
				if segSubtitleOnly {
					subs.add(lang)
				} else {
					audio.add(lang)
				}
			}
		}
	}
}

func handleStructuredExact(raw string, subtitleOnly bool, audio, subs *languageSet, originalVersion *bool) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	folded := foldLanguageToken(raw)
	switch folded {
	case "", "und", "unknown", "zxx", "none", "muet", "silent", "na":
		return true
	case "vo", "vost", "vos", "ov", "omu", "original", "original version", "version originale", "version original", "versione originale", "originalversion", "subbed", "fansub", "fastsub":
		if !subtitleOnly && originalVersion != nil {
			*originalVersion = true
		}
		return true
	case "multi", "multilingual", "multilang", "multiaudio", "dual", "dualaudio", "multilingue":
		if subtitleOnly {
			subs.addCode("multi")
		} else {
			audio.addCode("multi")
		}
		return true
	}
	if subCode, ok := subtitleCompoundTokens[folded]; ok {
		subs.addCode(subCode)
		if !subtitleOnly && originalVersion != nil && (strings.HasPrefix(folded, "vost") || folded == "vose" || folded == "omeu") {
			*originalVersion = true
		}
		if audioCode, alsoAudio := titleUnambiguousAudio[folded]; alsoAudio && !subtitleOnly {
			audio.addCode(audioCode)
		}
		return true
	}
	if lang, ok := lookupStructuredExact(raw); ok {
		if subtitleOnly {
			subs.add(lang)
		} else {
			audio.add(lang)
		}
		return true
	}
	return false
}

func lookupStructuredExact(raw string) (streamLanguage, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return streamLanguage{}, false
	}
	folded := foldLanguageToken(raw)
	if folded == "" || folded == "und" || folded == "unknown" || folded == "vo" || folded == "zxx" {
		return streamLanguage{}, false
	}
	if lang, ok := structuredLanguageExact[folded]; ok {
		return lang, true
	}
	if strings.ContainsAny(raw, "-_") {
		normalized := NormalizeLanguage(strings.ReplaceAll(raw, "_", "-"))
		if normalized != "" {
			return streamLanguageForCode(normalized), true
		}
	}
	if normalized := NormalizeLanguage(folded); normalized != "" {
		return streamLanguageForCode(normalized), true
	}
	return streamLanguage{}, false
}

func streamLanguageNames(languages []streamLanguage) string {
	labels := make([]string, 0, len(languages))
	for _, item := range languages {
		label := "MULTI"
		if item.code != "multi" {
			// Short codes such as EN and SR are also service aliases in
			// aggregators. Keep audio names unambiguous without changing the
			// normalized language evidence used for filtering.
			label = display.English.Languages().Name(language.Make(item.code))
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, " / ")
}

func streamLanguageLabel(languages []streamLanguage) string {
	flags := []string{}
	for _, item := range languages {
		if item.flag != "" {
			flags = append(flags, item.flag)
		}
	}
	return strings.TrimSpace(streamLanguageNames(languages) + " " + strings.Join(flags, " "))
}
