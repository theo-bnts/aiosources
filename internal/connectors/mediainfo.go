package connectors

import (
	"encoding/base64"
	"encoding/json"
	"html"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"golang.org/x/text/unicode/norm"
)

const (
	maxMediaInfoInputBytes = 256 * 1024
	maxMediaInfoLanguages  = 16
)

// MediaInfoSummary holds verified technical and language facts extracted from
// an already-received MediaInfo, BDInfo, or structured NFO payload.
type MediaInfoSummary struct {
	AudioLanguages    []string
	SubtitleLanguages []string
	DurationSeconds   int64
	BitrateBPS        int64
	Resolution        string
	VideoCodec        string
	AudioCodec        string
	AudioChannels     string
	HDR               string
}

func (s MediaInfoSummary) empty() bool {
	return len(s.AudioLanguages) == 0 && len(s.SubtitleLanguages) == 0 &&
		s.DurationSeconds == 0 && s.BitrateBPS == 0 &&
		s.Resolution == "" && s.VideoCodec == "" &&
		s.AudioCodec == "" && s.AudioChannels == "" && s.HDR == ""
}

// supplementMediaInfo opportunistically extracts structured audio/subtitle
// languages and technical facts from MediaInfo/BDInfo/NFO payloads already
// present on mapped fields. It never mutates nested source containers and
// never overwrites non-empty fields already mapped from the source.
func supplementMediaInfo(fields map[string]any) {
	if len(fields) == 0 {
		return
	}
	candidates := collectMediaInfoCandidates(fields)
	if len(candidates) == 0 {
		return
	}
	var merged MediaInfoSummary
	for _, cand := range candidates {
		parsed := parseMediaInfoCandidate(cand)
		mergeMediaInfoSummary(&merged, parsed)
	}
	if merged.empty() {
		return
	}
	if isFieldMissingOrEmpty(fields["audiolangs"]) && len(merged.AudioLanguages) > 0 {
		fields["audiolangs"] = merged.AudioLanguages
	}
	if isFieldMissingOrEmpty(fields["sublangs"]) && len(merged.SubtitleLanguages) > 0 {
		fields["sublangs"] = merged.SubtitleLanguages
	}
	if isFieldMissingOrEmpty(fields["duration"]) && merged.DurationSeconds > 0 {
		fields["duration"] = merged.DurationSeconds
	}
	if isFieldMissingOrEmpty(fields["bitrate"]) && merged.BitrateBPS > 0 {
		fields["bitrate"] = merged.BitrateBPS
	}
	if isFieldMissingOrEmpty(fields["resolution"]) && merged.Resolution != "" {
		fields["resolution"] = merged.Resolution
	}
	if isFieldMissingOrEmpty(fields["video_codec"]) && isFieldMissingOrEmpty(fields["codec"]) && merged.VideoCodec != "" {
		fields["video_codec"] = merged.VideoCodec
	}
	if isFieldMissingOrEmpty(fields["audio_codec"]) && merged.AudioCodec != "" {
		fields["audio_codec"] = merged.AudioCodec
	}
	if isFieldMissingOrEmpty(fields["audio_channels"]) && merged.AudioChannels != "" {
		fields["audio_channels"] = merged.AudioChannels
	}
	if isFieldMissingOrEmpty(fields["hdr"]) && merged.HDR != "" {
		fields["hdr"] = merged.HDR
	}
}

func mergeMediaInfoSummary(dst *MediaInfoSummary, src MediaInfoSummary) {
	for _, code := range src.AudioLanguages {
		if len(dst.AudioLanguages) < maxMediaInfoLanguages && !slices.Contains(dst.AudioLanguages, code) {
			dst.AudioLanguages = append(dst.AudioLanguages, code)
		}
	}
	for _, code := range src.SubtitleLanguages {
		if len(dst.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(dst.SubtitleLanguages, code) {
			dst.SubtitleLanguages = append(dst.SubtitleLanguages, code)
		}
	}
	if dst.DurationSeconds == 0 && src.DurationSeconds > 0 {
		dst.DurationSeconds = src.DurationSeconds
	}
	if dst.BitrateBPS == 0 && src.BitrateBPS > 0 {
		dst.BitrateBPS = src.BitrateBPS
	}
	if dst.Resolution == "" && src.Resolution != "" {
		dst.Resolution = src.Resolution
	}
	if dst.VideoCodec == "" && src.VideoCodec != "" {
		dst.VideoCodec = src.VideoCodec
	}
	if dst.AudioCodec == "" && src.AudioCodec != "" {
		dst.AudioCodec = src.AudioCodec
	}
	if dst.AudioChannels == "" && src.AudioChannels != "" {
		dst.AudioChannels = src.AudioChannels
	}
	if dst.HDR == "" && src.HDR != "" {
		dst.HDR = src.HDR
	}
}

func isFieldMissingOrEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []string:
		return len(v) == 0
	case []any:
		return len(v) == 0
	case json.Number:
		s := strings.TrimSpace(string(v))
		return s == "" || s == "0"
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0
	}
	return false
}

type mediaInfoCandidate struct {
	text       string
	jsonObj    map[string]any
	strictOnly bool // true for description: accept only explicit MediaInfo/BDInfo blocks
}

var mediaInfoFieldKeys = []string{
	"mediainfo", "media_info", "bdinfo", "bd_info",
	"nfoContent", "nfo_content", "nfo",
}

func collectMediaInfoCandidates(fields map[string]any) []mediaInfoCandidate {
	var out []mediaInfoCandidate
	add := func(value any, strictOnly bool) {
		if value == nil {
			return
		}
		text, obj := decodeMediaInfoInput(value)
		if text != "" || obj != nil {
			out = append(out, mediaInfoCandidate{text: text, jsonObj: obj, strictOnly: strictOnly})
		}
	}
	for _, key := range mediaInfoFieldKeys {
		add(fields[key], false)
	}
	if meta, ok := fields["metadata"].(map[string]any); ok {
		for _, key := range mediaInfoFieldKeys {
			add(meta[key], false)
		}
	}
	switch attrs := fields["attributes"].(type) {
	case map[string]any:
		for _, key := range mediaInfoFieldKeys {
			add(attrs[key], false)
		}
		if desc, ok := attrs["description"]; ok {
			add(desc, true)
		}
	case map[string][]string:
		for _, key := range mediaInfoFieldKeys {
			if vals := attrs[key]; len(vals) > 0 {
				add(vals[0], false)
			}
		}
		if vals := attrs["description"]; len(vals) > 0 {
			add(vals[0], true)
		}
	case map[string]string:
		for _, key := range mediaInfoFieldKeys {
			if val := attrs[key]; val != "" {
				add(val, false)
			}
		}
		if val := attrs["description"]; val != "" {
			add(val, true)
		}
	}
	if desc, ok := fields["description"]; ok {
		add(desc, true)
	}
	return out
}

func decodeMediaInfoInput(value any) (string, map[string]any) {
	switch v := value.(type) {
	case string:
		text := sanitizeMediaInfoString(v)
		if text == "" {
			return "", nil
		}
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"track"`) {
			var obj map[string]any
			if err := decodeJSON([]byte(trimmed), &obj); err == nil {
				return text, obj
			}
		}
		return text, nil
	case map[string]any:
		if enc, _ := v["encoding"].(string); enc == "base64" {
			if data, ok := v["data"].(string); ok && len(data) <= maxMediaInfoInputBytes*2 {
				if raw, err := base64.StdEncoding.DecodeString(data); err == nil {
					return sanitizeMediaInfoString(string(raw)), nil
				}
			}
			return "", nil
		}
		if _, hasMedia := v["media"]; hasMedia {
			return "", v
		}
		if _, hasTrack := v["track"]; hasTrack {
			return "", v
		}
		return "", nil
	case []string:
		for _, item := range v {
			if text, obj := decodeMediaInfoInput(item); text != "" || obj != nil {
				return text, obj
			}
		}
	case []any:
		for _, item := range v {
			if text, obj := decodeMediaInfoInput(item); text != "" || obj != nil {
				return text, obj
			}
		}
	}
	return "", nil
}

func sanitizeMediaInfoString(s string) string {
	if len(s) > maxMediaInfoInputBytes {
		s = s[:maxMediaInfoInputBytes]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

func parseMediaInfoCandidate(c mediaInfoCandidate) MediaInfoSummary {
	if c.jsonObj != nil {
		if parsed, ok := parseMediaInfoJSON(c.jsonObj); ok {
			return parsed
		}
	}
	if c.text == "" {
		return MediaInfoSummary{}
	}
	return ParseMediaInfo(c.text, c.strictOnly)
}

// ParseMediaInfo parses a MediaInfo, BDInfo, or (when strictOnly is false)
// structured key-value NFO text block into normalized language and technical
// metadata. Free-form prose is ignored to avoid false positives.
func ParseMediaInfo(raw string, strictOnly bool) MediaInfoSummary {
	cleaned := cleanMediaInfoText(raw)
	if cleaned == "" {
		return MediaInfoSummary{}
	}
	if summary, ok := parseMediaInfoText(cleaned); ok {
		return summary
	}
	if summary, ok := parseBDInfoText(cleaned); ok {
		return summary
	}
	if !strictOnly {
		if summary, ok := parseStructuredNFO(cleaned); ok {
			return summary
		}
	}
	return MediaInfoSummary{}
}

var (
	bbcodeBlockStripRE  = regexp.MustCompile(`(?i)\[/?(?:code|spoiler(?:=[^\]]*)?|pre|quote(?:=[^\]]*)?|center|left|right|hide(?:=[^\]]*)?|mediainfo|nfo)\]`)
	bbcodeInlineStripRE = regexp.MustCompile(`(?i)\[/?(?:b|i|u|s|color(?:=[^\]]*)?|font(?:=[^\]]*)?|size(?:=[^\]]*)?)\]`)
	htmlBreakRE         = regexp.MustCompile(`(?i)<(?:br\s*/?|/(?:p|div|tr|li|h[1-6]))>`)
	htmlTagStripRE      = regexp.MustCompile(`<[^>]{1,120}>`)
	ansiEscapeRE        = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	markdownFenceRE     = regexp.MustCompile("(?m)^[ \t]*```[A-Za-z0-9_-]*[ \t]*$")
	mediaInfoBoxBorder  = "│║┌┐└┘├┤┬┴┼─═█▀▄▌▐░▒▓|"
)

func cleanMediaInfoText(raw string) string {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if strings.IndexByte(s, '\x1b') >= 0 {
		s = ansiEscapeRE.ReplaceAllString(s, "")
	}
	if strings.IndexByte(s, '[') >= 0 {
		s = bbcodeBlockStripRE.ReplaceAllString(s, "\n")
		s = bbcodeInlineStripRE.ReplaceAllString(s, "")
	}
	if strings.IndexByte(s, '<') >= 0 {
		s = htmlBreakRE.ReplaceAllString(s, "\n")
		s = htmlTagStripRE.ReplaceAllString(s, "")
	}
	if strings.Contains(s, "&") {
		s = html.UnescapeString(s)
	}
	if strings.Contains(s, "```") {
		s = markdownFenceRE.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return s
}

func trimNFOLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, mediaInfoBoxBorder)
	line = strings.TrimRight(line, mediaInfoBoxBorder)
	return strings.TrimSpace(line)
}

// --- Parser 1: MediaInfo JSON ---

func parseMediaInfoJSON(root map[string]any) (MediaInfoSummary, bool) {
	var tracksRaw []any
	if media, ok := root["media"].(map[string]any); ok {
		tracksRaw, _ = media["track"].([]any)
	} else {
		tracksRaw, _ = root["track"].([]any)
	}
	if len(tracksRaw) == 0 {
		return MediaInfoSummary{}, false
	}
	var sections []mediaInfoSection
	for _, entry := range tracksRaw {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(FieldString(obj["@type"])))
		kind := ""
		switch typ {
		case "general":
			kind = "general"
		case "video":
			kind = "video"
		case "audio":
			kind = "audio"
		case "text":
			kind = "text"
		default:
			continue
		}
		sec := mediaInfoSection{kind: kind, kv: map[string]string{}}
		for k, v := range obj {
			if k == "@type" || k == "extra" {
				continue
			}
			val := strings.TrimSpace(FieldString(v))
			if val == "" {
				continue
			}
			switch strings.ToLower(k) {
			case "language":
				sec.kv["language"] = val
			case "title":
				sec.kv["title"] = val
			case "forced":
				sec.kv["forced"] = val
			case "servicekind":
				sec.kv["service_kind"] = val
			case "format":
				sec.kv["format"] = val
			case "format_commercial_ifany", "format_commercial":
				sec.kv["commercial_name"] = val
			case "format_additionalfeatures":
				sec.kv["format_additional"] = val
			case "codecid":
				sec.kv["codec_id"] = val
			case "channels":
				sec.kv["channels"] = val
			case "duration":
				sec.kv["duration"] = val
			case "overallbitrate":
				sec.kv["overall_bitrate"] = val
			case "bitrate":
				sec.kv["bitrate"] = val
			case "width":
				sec.kv["width"] = val
			case "height":
				sec.kv["height"] = val
			case "scantype":
				sec.kv["scan_type"] = val
			case "hdr_format":
				sec.kv["hdr_format"] = val
			case "hdr_format_compatibility":
				sec.kv["hdr_compat"] = val
			case "transfer_characteristics":
				sec.kv["transfer"] = val
			case "colour_primaries", "color_primaries":
				sec.kv["primaries"] = val
			}
		}
		sections = append(sections, sec)
	}
	if len(sections) == 0 {
		return MediaInfoSummary{}, false
	}
	summary := buildSummaryFromSections(sections)
	return summary, !summary.empty()
}

// --- Parser 2: Standard MediaInfo Text (multilingual) ---

type mediaInfoSection struct {
	kind string // general, video, audio, text
	kv   map[string]string
}

var sectionHeaderSuffixRE = regexp.MustCompile(`^(.*?)(?:\s*#?\s*\d+)?$`)

func classifyMediaInfoSectionHeader(line string) string {
	if line == "" || utf8.RuneCountInString(line) > 40 || strings.ContainsAny(line, ":;=[]/\\") {
		return ""
	}
	folded := foldMediaInfoToken(line)
	m := sectionHeaderSuffixRE.FindStringSubmatch(folded)
	if len(m) != 2 {
		return ""
	}
	base := strings.TrimSpace(m[1])
	switch base {
	case "general", "allgemein", "generale", "geral", "ogolne", "algemeen", "genel", "общее", "общие":
		return "general"
	case "video", "wideo", "видео":
		return "video"
	case "audio", "dzwiek", "аудио":
		return "audio"
	case "text", "texte", "texto", "testo", "tekst", "текст", "subtitle", "subtitles", "sous-titres", "sous titres", "untertitel", "sottotitoli", "subtitulos", "napisy":
		return "text"
	case "menu", "chapters", "chapitres", "kapitel", "capitulos", "capitoli", "меню", "other", "autres", "andere", "image", "images":
		return "stop"
	}
	return ""
}

func normalizeMediaInfoKey(raw string) string {
	folded := foldMediaInfoToken(raw)
	folded = strings.Join(strings.Fields(folded), " ")
	switch folded {
	case "language", "langue", "sprache", "idioma", "lingua", "jezyk", "taal", "язык":
		return "language"
	case "title", "titre", "titel", "titulo", "titolo", "tytul", "заголовок", "название":
		return "title"
	case "forced", "force", "erzwungen", "forzado", "forzato", "wymuszony", "принудительный":
		return "forced"
	case "service kind", "type de service", "serviceart":
		return "service_kind"
	case "format", "формат":
		return "format"
	case "commercial name", "nom commercial", "kommerzieller name", "nombre comercial", "nome commerciale", "коммерческое название":
		return "commercial_name"
	case "format/info", "format info", "format/url":
		return "format_info"
	case "format settings", "parametres du format":
		return "format_settings"
	case "codec id", "identifiant du codec", "codec-id", "идентификатор кодека":
		return "codec_id"
	case "channel(s)", "channels", "channel", "canaux", "kanal", "kanale", "canales", "canali", "kanaly", "каналы":
		return "channels"
	case "duration", "duree", "dauer", "duracion", "durata", "czas trwania", "продолжительность":
		return "duration"
	case "overall bit rate", "debit global moyen", "debit global", "gesamte bitrate", "tasa de bits general", "bitrate totale", "calkowita przeplywnosc", "общий поток":
		return "overall_bitrate"
	case "bit rate", "debit", "bitrate", "tasa de bits", "битрейт":
		return "bitrate"
	case "width", "largeur", "breite", "ancho", "larghezza", "szerokosc", "ширина":
		return "width"
	case "height", "hauteur", "hohe", "alto", "altezza", "wysokosc", "высота":
		return "height"
	case "scan type", "type de balayage", "scantyp", "tipo de exploracion", "tipo di scansione":
		return "scan_type"
	case "hdr format", "format hdr", "hdr-format":
		return "hdr_format"
	case "transfer characteristics", "caracteristiques de transfert", "transfer-eigenschaften":
		return "transfer"
	case "colour primaries", "color primaries", "coordonnees de chromaticite", "primaries":
		return "primaries"
	case "complete name", "nom complet", "vollstandiger name", "nombre completo", "id", "unique id", "identifiant unique", "format version", "format profile", "file size", "taille du fichier":
		return "marker"
	}
	return ""
}

func parseMediaInfoText(text string) (MediaInfoSummary, bool) {
	lines := strings.Split(text, "\n")
	var sections []mediaInfoSection
	var current *mediaInfoSection
	stopped := false
	knownKVCount := 0
	hasTechSection := false

	flush := func() {
		if current != nil && len(current.kv) > 0 {
			sections = append(sections, *current)
		}
		current = nil
	}

	for _, rawLine := range lines {
		line := trimNFOLine(rawLine)
		if line == "" {
			continue
		}
		if kind := classifyMediaInfoSectionHeader(line); kind != "" {
			flush()
			if kind == "stop" {
				stopped = true
				continue
			}
			stopped = false
			if kind == "video" || kind == "audio" || kind == "text" {
				hasTechSection = true
			}
			current = &mediaInfoSection{kind: kind, kv: map[string]string{}}
			continue
		}
		if stopped || current == nil {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 || utf8.RuneCountInString(line[:colon]) > 48 {
			continue
		}
		key := normalizeMediaInfoKey(line[:colon])
		if key == "" {
			continue
		}
		val := strings.TrimSpace(line[colon+1:])
		if val == "" {
			continue
		}
		knownKVCount++
		if key == "marker" {
			continue
		}
		if _, exists := current.kv[key]; !exists {
			current.kv[key] = val
		}
	}
	flush()

	if len(sections) == 0 || knownKVCount < 2 || (!hasTechSection && knownKVCount < 3) {
		return MediaInfoSummary{}, false
	}
	summary := buildSummaryFromSections(sections)
	return summary, !summary.empty()
}

func buildSummaryFromSections(sections []mediaInfoSection) MediaInfoSummary {
	var summary MediaInfoSummary
	bestAudioRank := -1

	for _, sec := range sections {
		switch sec.kind {
		case "general":
			if summary.DurationSeconds == 0 {
				summary.DurationSeconds = parseMediaInfoDuration(sec.kv["duration"])
			}
			if summary.BitrateBPS == 0 {
				summary.BitrateBPS = parseMediaInfoBitrate(sec.kv["overall_bitrate"])
			}
		case "video":
			if summary.DurationSeconds == 0 {
				summary.DurationSeconds = parseMediaInfoDuration(sec.kv["duration"])
			}
			if summary.Resolution == "" {
				w := parseMediaInfoPixels(sec.kv["width"])
				h := parseMediaInfoPixels(sec.kv["height"])
				interlaced := strings.Contains(strings.ToLower(sec.kv["scan_type"]), "interlac")
				summary.Resolution = resolutionFromDimensions(w, h, interlaced)
			}
			if summary.VideoCodec == "" {
				summary.VideoCodec = normalizeMediaInfoVideoCodec(sec.kv["format"], sec.kv["commercial_name"], sec.kv["codec_id"])
			}
			if hdr := normalizeMediaInfoHDR(sec.kv["hdr_format"], sec.kv["hdr_compat"], sec.kv["transfer"], sec.kv["primaries"]); hdr != "" {
				if summary.HDR == "" {
					summary.HDR = hdr
				} else if !strings.Contains(summary.HDR, hdr) {
					summary.HDR = mergeHDRStrings(summary.HDR, hdr)
				}
			}
		case "audio":
			if isCommentaryTrack(sec.kv["title"], sec.kv["service_kind"]) {
				continue
			}
			langCode := parseMediaInfoTrackLanguage(sec.kv["language"], sec.kv["title"])
			if langCode != "" && len(summary.AudioLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.AudioLanguages, langCode) {
				summary.AudioLanguages = append(summary.AudioLanguages, langCode)
			}
			codec, rank := normalizeMediaInfoAudioCodec(sec.kv["commercial_name"], sec.kv["format"], sec.kv["format_info"], sec.kv["format_additional"], sec.kv["codec_id"])
			channels := normalizeMediaInfoChannels(sec.kv["channels"])
			if codec != "" && rank > bestAudioRank {
				bestAudioRank = rank
				summary.AudioCodec = codec
				if channels != "" {
					summary.AudioChannels = channels
				}
			} else if summary.AudioChannels == "" && channels != "" {
				summary.AudioChannels = channels
			}
		case "text":
			if isForcedSubtitleTrack(sec.kv["forced"], sec.kv["title"]) {
				continue
			}
			langCode := parseMediaInfoTrackLanguage(sec.kv["language"], sec.kv["title"])
			if langCode != "" && len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, langCode) {
				summary.SubtitleLanguages = append(summary.SubtitleLanguages, langCode)
			}
		}
	}
	return summary
}

// --- Parser 3: BDInfo Text ---

var (
	bdInfoHeaderRE = regexp.MustCompile(`(?i)\b(?:DISC INFO|PLAYLIST REPORT|QUICK SUMMARY)\s*:`)
	bdInfoAudioRE  = regexp.MustCompile(`(?i)(?:DTS(?:-HD)?\s*(?:Master|High-?Res|Express)?\s*Audio|DTS:X\s*Master\s*Audio|Dolby\s+(?:TrueHD(?:/Atmos)?|Digital(?:\s+Plus(?:/Atmos)?|\s+EX)?)\s*Audio|LPCM\s*Audio|AC3\s+Embedded|FLAC\s+Audio|MPEG\s+Audio)\s+([A-Za-zÀ-ÿ][A-Za-zÀ-ÿ\s()/,-]{1,36}?)\s+(\d[\d.,]*\s*kbps|\d+\.\d+\b)`)
	bdInfoSubRE    = regexp.MustCompile(`(?i)(?:Presentation\s+Graphics|Text)\s+([A-Za-zÀ-ÿ][A-Za-zÀ-ÿ\s()/,-]{1,36}?)(?:\s+(?:\d[\d.,]*\s*kbps|\d+\.\d+\s*%)|$)`)
	bdInfoResRE    = regexp.MustCompile(`(?i)\b(2160p|1080[pi]|720p|576[pi]|480[pi])\b`)
	bdInfoChanRE   = regexp.MustCompile(`\b([1257]\.[01])\b`)
)

func parseBDInfoText(text string) (MediaInfoSummary, bool) {
	isBDInfo := bdInfoHeaderRE.MatchString(text)
	if !isBDInfo {
		upper := strings.ToUpper(text)
		isBDInfo = strings.Contains(upper, "VIDEO:") && strings.Contains(upper, "AUDIO:") &&
			(strings.Contains(upper, "MPEG-H HEVC") || strings.Contains(upper, "MPEG-4 AVC") || strings.Contains(upper, "DTS-HD") || strings.Contains(upper, "DOLBY TRUEHD") || strings.Contains(upper, "PRESENTATION GRAPHICS"))
	}
	if !isBDInfo {
		return MediaInfoSummary{}, false
	}

	var summary MediaInfoSummary
	section := ""
	bestAudioRank := -1

	handleVideoLine := func(line, upper string) {
		if summary.VideoCodec == "" {
			switch {
			case strings.Contains(upper, "HEVC") || strings.Contains(upper, "H.265"):
				summary.VideoCodec = "HEVC"
			case strings.Contains(upper, "AVC") || strings.Contains(upper, "H.264"):
				summary.VideoCodec = "H264"
			case strings.Contains(upper, "VC-1"):
				summary.VideoCodec = "VC-1"
			case strings.Contains(upper, "MPEG-2"):
				summary.VideoCodec = "MPEG-2"
			}
		}
		if summary.Resolution == "" {
			if m := bdInfoResRE.FindStringSubmatch(line); len(m) == 2 {
				summary.Resolution = strings.ToLower(m[1])
			}
		}
		if hdr := normalizeMediaInfoHDR(line, "", "", ""); hdr != "" {
			if summary.HDR == "" {
				summary.HDR = hdr
			} else if !strings.Contains(summary.HDR, hdr) {
				summary.HDR = mergeHDRStrings(summary.HDR, hdr)
			}
		}
	}

	handleAudioCodec := func(line string) {
		codec, rank := normalizeMediaInfoAudioCodec(line, line, "", "", "")
		ch := ""
		if cm := bdInfoChanRE.FindStringSubmatch(line); len(cm) == 2 {
			ch = cm[1]
		}
		if codec != "" && rank > bestAudioRank {
			bestAudioRank = rank
			summary.AudioCodec = codec
			if ch != "" {
				summary.AudioChannels = ch
			}
		}
	}

	for _, rawLine := range strings.Split(text, "\n") {
		line := trimNFOLine(rawLine)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case upper == "VIDEO:":
			section = "video"
			continue
		case strings.HasPrefix(upper, "VIDEO:"):
			rest := strings.TrimSpace(line[len("VIDEO:"):])
			handleVideoLine(rest, strings.ToUpper(rest))
			continue
		case upper == "AUDIO:" || upper == "AUDIO TRACKS:":
			section = "audio"
			continue
		case strings.HasPrefix(upper, "AUDIO:"):
			rest := strings.TrimSpace(line[len("AUDIO:"):])
			if !isCommentaryTrack(rest, "") {
				parts := strings.Split(rest, "/")
				if len(parts) > 0 {
					if code := lookupMediaInfoLanguage(strings.TrimSpace(parts[0])); code != "" && len(summary.AudioLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.AudioLanguages, code) {
						summary.AudioLanguages = append(summary.AudioLanguages, code)
					}
				}
				handleAudioCodec(rest)
			}
			continue
		case upper == "SUBTITLES:" || upper == "SUBTITLE:":
			section = "subtitles"
			continue
		case strings.HasPrefix(upper, "SUBTITLE:") || strings.HasPrefix(upper, "SUBTITLES:"):
			colon := strings.IndexByte(line, ':')
			rest := strings.TrimSpace(line[colon+1:])
			if !isForcedSubtitleTrack("", rest) {
				parts := strings.Split(rest, "/")
				if len(parts) > 0 {
					if code := lookupMediaInfoLanguage(strings.TrimSpace(parts[0])); code != "" && len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, code) {
						summary.SubtitleLanguages = append(summary.SubtitleLanguages, code)
					}
				}
			}
			continue
		case upper == "FILES:" || upper == "CHAPTERS:" || upper == "STREAM DIAGNOSTICS:":
			section = "stop"
			continue
		}

		if strings.HasPrefix(upper, "LENGTH:") {
			if d := parseMediaInfoDuration(strings.TrimSpace(line[len("LENGTH:"):])); d > 0 && summary.DurationSeconds == 0 {
				summary.DurationSeconds = d
			}
			continue
		}
		if strings.HasPrefix(upper, "TOTAL BITRATE:") {
			if b := parseMediaInfoBitrate(strings.TrimSpace(line[len("TOTAL BITRATE:"):])); b > 0 && summary.BitrateBPS == 0 {
				summary.BitrateBPS = b
			}
			continue
		}

		switch section {
		case "video":
			handleVideoLine(line, upper)
		case "audio":
			if isCommentaryTrack(line, "") {
				continue
			}
			if m := bdInfoAudioRE.FindStringSubmatch(line); len(m) == 3 {
				if code := lookupMediaInfoLanguage(m[1]); code != "" && len(summary.AudioLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.AudioLanguages, code) {
					summary.AudioLanguages = append(summary.AudioLanguages, code)
				}
				handleAudioCodec(line)
			}
		case "subtitles":
			if isForcedSubtitleTrack("", line) {
				continue
			}
			if m := bdInfoSubRE.FindStringSubmatch(line); len(m) == 2 {
				if code := lookupMediaInfoLanguage(m[1]); code != "" && len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, code) {
					summary.SubtitleLanguages = append(summary.SubtitleLanguages, code)
				}
			}
		}
	}
	return summary, !summary.empty()
}

// --- Parser 4: Structured Key-Value NFO Fallback ---

var nfoAudioListSplitRE = regexp.MustCompile(`[,;/+|&]+`)
var nfoTrackNumberSuffixRE = regexp.MustCompile(`\s*#?\s*\d+$`)

func parseStructuredNFO(text string) (MediaInfoSummary, bool) {
	var summary MediaInfoSummary
	matchedFields := 0

	for _, rawLine := range strings.Split(text, "\n") {
		line := trimNFOLine(rawLine)
		if line == "" || utf8.RuneCountInString(line) > 260 {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 || utf8.RuneCountInString(line[:colon]) > 40 {
			continue
		}
		rawKey := foldMediaInfoToken(line[:colon])
		rawKey = strings.TrimSpace(strings.Trim(rawKey, ".-_*•>"))
		rawKey = strings.TrimSpace(nfoTrackNumberSuffixRE.ReplaceAllString(rawKey, ""))
		val := strings.TrimSpace(line[colon+1:])
		if val == "" || utf8.RuneCountInString(val) > 200 {
			continue
		}

		switch rawKey {
		case "audio", "langue", "langues", "langue(s)", "language", "languages",
			"langue audio", "langues audio", "piste audio", "pistes audio",
			"audio language", "audio languages", "ton", "tonspur", "sprache", "sprachen",
			"idioma", "idiomas", "lingua", "lingue":
			if isCommentaryTrack(val, "") {
				continue
			}
			added := false
			for _, part := range nfoAudioListSplitRE.Split(val, -1) {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if subCode, isSubOnly := nfoSubtitleCompoundCode(part); isSubOnly {
					if len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, subCode) {
						summary.SubtitleLanguages = append(summary.SubtitleLanguages, subCode)
						added = true
					}
					continue
				}
				for _, tok := range mediaInfoTokenRE.FindAllString(part, -1) {
					if subCode, isSubOnly := nfoSubtitleCompoundCode(tok); isSubOnly {
						if len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, subCode) {
							summary.SubtitleLanguages = append(summary.SubtitleLanguages, subCode)
							added = true
						}
					}
				}
				for _, code := range lookupAllMediaInfoLanguages(part) {
					if len(summary.AudioLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.AudioLanguages, code) {
						summary.AudioLanguages = append(summary.AudioLanguages, code)
						added = true
					}
				}
			}
			if added {
				matchedFields++
			}
		case "sous-titre", "sous-titres", "sous titre", "sous titres", "sous-titre(s)", "sous titre(s)",
			"subtitle", "subtitles", "sub", "subs", "untertitel", "subtitulos", "sottotitoli", "napisy":
			if isNFONoneValue(val) {
				continue
			}
			added := false
			for _, part := range nfoAudioListSplitRE.Split(val, -1) {
				part = strings.TrimSpace(part)
				if part == "" || isNFONoneValue(part) || isForcedSubtitleTrack("", part) {
					continue
				}
				for _, code := range lookupAllMediaInfoLanguages(part) {
					if len(summary.SubtitleLanguages) < maxMediaInfoLanguages && !slices.Contains(summary.SubtitleLanguages, code) {
						summary.SubtitleLanguages = append(summary.SubtitleLanguages, code)
						added = true
					}
				}
			}
			if added {
				matchedFields++
			}
		case "duree", "duration", "runtime", "laufzeit", "duracion", "durata", "temps":
			if summary.DurationSeconds == 0 {
				if d := parseMediaInfoDuration(val); d > 0 {
					summary.DurationSeconds = d
					matchedFields++
				}
			}
		}
	}
	return summary, matchedFields > 0 && !summary.empty()
}

func isNFONoneValue(val string) bool {
	switch foldMediaInfoToken(val) {
	case "", "none", "no", "non", "aucun", "aucune", "sans", "nein", "ninguno", "nessuno", "brak", "n/a", "na", "-":
		return true
	}
	return false
}

func nfoSubtitleCompoundCode(part string) (string, bool) {
	folded := foldMediaInfoToken(part)
	switch folded {
	case "vostfr", "subfrench", "stfr", "subfr":
		return "fr", true
	case "vose", "vostes", "subesp", "subspanish":
		return "es", true
	case "vosta", "vosten", "engsub", "engsubs", "subeng":
		return "en", true
	case "vostit", "subita":
		return "it", true
	case "vostde", "subger":
		return "de", true
	case "vostpt", "legendado":
		return "pt", true
	}
	return "", false
}

// --- Track Filters (Commentary / Forced Subtitles) ---

func isCommentaryTrack(title, serviceKind string) bool {
	if strings.EqualFold(strings.TrimSpace(serviceKind), "cm") {
		return true
	}
	folded := foldMediaInfoToken(title)
	if folded == "" {
		return false
	}
	for _, marker := range []string{
		"commentary", "commentaire", "kommentar", "comentario", "commento",
		"komentarz", "комментарий", "коментар",
	} {
		if strings.Contains(folded, marker) {
			return true
		}
	}
	return false
}

func isForcedSubtitleTrack(forced, title string) bool {
	switch foldMediaInfoToken(forced) {
	case "yes", "oui", "ja", "si", "sim", "tak", "да", "true", "1":
		return true
	}
	folded := foldMediaInfoToken(title)
	if folded == "" {
		return false
	}
	// If the track explicitly states it also covers full dialogue, keep it.
	if strings.Contains(folded, "full") || strings.Contains(folded, "complet") {
		return false
	}
	for _, marker := range []string{
		"forced", "force", "forzado", "erzwungen", "forzati", "forzato", "wymuszony", "принудител",
		"signs & songs", "signs/songs", "songs & signs", "signs only", "songs only", "panneaux", "chansons",
		"commentary", "commentaire", "kommentar", "comentario", "commento",
	} {
		if strings.Contains(folded, marker) {
			return true
		}
	}
	return false
}

// --- Duration, Bitrate, Video/Audio Technical Normalizers ---

var (
	hmsColonDurationRE = regexp.MustCompile(`\b(\d{1,2}):(\d{2}):(\d{2})(?:\.\d+)?\b`)
	unitDurationRE     = regexp.MustCompile(`(?i)(\d+)\s*(h(?:ours?|eures?|oras?|rs?)?\b|std\b\.?|stunden?\b|godz\b\.?|ч(?:ас(?:а|ов)?)?|min(?:utes?|s?)?\b\.?|мин(?:ут[аы]?)?|mn\b|m\b|s(?:ec(?:onds?|ondes?)?|ek(?:unden?)?)?\b\.?|сек(?:унд[аы]?)?)`)
	bitrateValueRE     = regexp.MustCompile(`(?i)(\d+(?:[\s\x{00a0}]\d{3})*(?:[.,]\d+)?)\s*(mb/s|mbps|mbit/s|мбит/с|kb/s|kbps|kbit/s|кбит/с|b/s|bps|бит/с)`)
	digitsRE           = regexp.MustCompile(`\d+`)
)

func parseMediaInfoDuration(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// MediaInfo JSON emits numeric seconds (e.g. "7215.040").
	if f, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		sec := int64(math.Round(f))
		if sec >= 30 && sec <= 86400*7 {
			return sec
		}
		return 0
	}
	if m := hmsColonDurationRE.FindStringSubmatch(raw); len(m) == 4 {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		s, _ := strconv.Atoi(m[3])
		sec := int64(h*3600 + mi*60 + s)
		if sec >= 30 && sec <= 86400*7 {
			return sec
		}
		return 0
	}
	matches := unitDurationRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0
	}
	var total int64
	for _, m := range matches {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		unit := strings.ToLower(m[2])
		switch {
		case strings.HasPrefix(unit, "h") || strings.HasPrefix(unit, "std") || strings.HasPrefix(unit, "stu") || strings.HasPrefix(unit, "godz") || strings.HasPrefix(unit, "ч"):
			total += n * 3600
		case strings.HasPrefix(unit, "m") || strings.HasPrefix(unit, "мин"):
			total += n * 60
		case strings.HasPrefix(unit, "s") || strings.HasPrefix(unit, "сек"):
			total += n
		}
	}
	if total >= 30 && total <= 86400*7 {
		return total
	}
	return 0
}

func parseMediaInfoBitrate(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// MediaInfo JSON emits integer bps (e.g. "12400000").
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n >= 50_000 && n <= 500_000_000 {
			return n
		}
		return 0
	}
	m := bitrateValueRE.FindStringSubmatch(raw)
	if len(m) != 3 {
		return 0
	}
	numStr := strings.ReplaceAll(strings.ReplaceAll(m[1], " ", ""), "\u00a0", "")
	numStr = strings.ReplaceAll(numStr, ",", ".")
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil || val <= 0 || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0
	}
	unit := strings.ToLower(m[2])
	var bps float64
	switch {
	case strings.HasPrefix(unit, "m") || strings.HasPrefix(unit, "м"):
		bps = val * 1_000_000
	case strings.HasPrefix(unit, "k") || strings.HasPrefix(unit, "к"):
		bps = val * 1_000
	default:
		bps = val
	}
	result := int64(math.Round(bps))
	if result >= 50_000 && result <= 500_000_000 {
		return result
	}
	return 0
}

func parseMediaInfoPixels(raw string) int {
	if raw == "" {
		return 0
	}
	// Strip thousands spaces ("3 840 pixels" -> "3840").
	cleaned := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), "\u00a0", "")
	m := digitsRE.FindString(cleaned)
	if m == "" {
		return 0
	}
	n, _ := strconv.Atoi(m)
	return n
}

func resolutionFromDimensions(width, height int, interlaced bool) string {
	switch {
	case width >= 7600 || height >= 4300:
		return "4320p"
	case width >= 3800 || height >= 2100:
		return "2160p"
	case width >= 2500 || height >= 1400:
		return "1440p"
	case width >= 1900 || height >= 1070:
		if interlaced {
			return "1080i"
		}
		return "1080p"
	case width >= 1260 || height >= 700:
		return "720p"
	case height >= 540:
		return "576p"
	case height >= 440:
		return "480p"
	default:
		return ""
	}
}

func normalizeMediaInfoVideoCodec(format, commercial, codecID string) string {
	combined := strings.ToUpper(format + " " + commercial + " " + codecID)
	switch {
	case strings.Contains(combined, "HEVC") || strings.Contains(combined, "H.265") || strings.Contains(combined, "H265") || strings.Contains(combined, "X265"):
		return "HEVC"
	case strings.Contains(combined, "AV1") || strings.Contains(combined, "AV01"):
		return "AV1"
	case strings.Contains(combined, "AVC") || strings.Contains(combined, "H.264") || strings.Contains(combined, "H264") || strings.Contains(combined, "X264"):
		return "H264"
	case strings.Contains(combined, "VC-1") || strings.Contains(combined, "WVC1"):
		return "VC-1"
	case strings.Contains(combined, "MPEG VIDEO") || strings.Contains(combined, "MPEG-2") || strings.Contains(combined, "V_MPEG2"):
		return "MPEG-2"
	case strings.Contains(combined, "XVID") || strings.Contains(combined, "MPEG-4 VISUAL"):
		return "XVID"
	}
	return ""
}

func normalizeMediaInfoHDR(hdrFormat, hdrCompat, transfer, primaries string) string {
	combined := strings.ToUpper(hdrFormat + " " + hdrCompat)
	var tags []string
	add := func(tag string) {
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if strings.Contains(combined, "DOLBY VISION") {
		add("DV")
	}
	if strings.Contains(combined, "HDR10+") || strings.Contains(combined, "SMPTE ST 2094") {
		add("HDR10+")
	} else if strings.Contains(combined, "HDR10") || strings.Contains(combined, "SMPTE ST 2086") {
		add("HDR10")
	}
	transUpper := strings.ToUpper(transfer)
	primUpper := strings.ToUpper(primaries)
	if strings.Contains(combined, "HLG") || strings.Contains(transUpper, "HLG") {
		add("HLG")
	} else if len(tags) == 0 && strings.Contains(transUpper, "PQ") && strings.Contains(primUpper, "BT.2020") {
		add("HDR10")
	}
	return strings.Join(tags, " ")
}

func mergeHDRStrings(a, b string) string {
	var tags []string
	for _, part := range append(strings.Fields(a), strings.Fields(b)...) {
		if !slices.Contains(tags, part) {
			tags = append(tags, part)
		}
	}
	return strings.Join(tags, " ")
}

func normalizeMediaInfoAudioCodec(commercial, format, formatInfo, additional, codecID string) (string, int) {
	combined := strings.ToUpper(commercial + " " + format + " " + formatInfo + " " + additional + " " + codecID)
	hasAtmos := strings.Contains(combined, "ATMOS") || strings.Contains(combined, "JOC")
	switch {
	case strings.Contains(combined, "TRUEHD") || strings.Contains(combined, "MLP FBA"):
		if hasAtmos {
			return "TRUEHD ATMOS", 100
		}
		return "TRUEHD", 95
	case strings.Contains(combined, "DTS:X") || strings.Contains(combined, "DTS XLL X") || strings.Contains(combined, "XLL X"):
		return "DTS:X", 98
	case strings.Contains(combined, "DTS-HD MASTER") || strings.Contains(combined, "DTS XLL") || strings.Contains(combined, "XLL"):
		return "DTS-HD MA", 90
	case strings.Contains(combined, "DTS-HD HIGH") || strings.Contains(combined, "DTS XBR") || strings.Contains(combined, "XBR"):
		return "DTS-HD", 80
	case strings.Contains(combined, "E-AC-3") || strings.Contains(combined, "EAC3") || strings.Contains(combined, "DOLBY DIGITAL PLUS"):
		if hasAtmos {
			return "DDP ATMOS", 75
		}
		return "DDP", 70
	case strings.Contains(combined, "FLAC"):
		return "FLAC", 85
	case strings.Contains(combined, "PCM") || strings.Contains(combined, "LPCM"):
		return "PCM", 82
	case strings.Contains(combined, "DTS"):
		return "DTS", 65
	case strings.Contains(combined, "AC-3") || strings.Contains(combined, "AC3") || strings.Contains(combined, "DOLBY DIGITAL"):
		return "AC3", 60
	case strings.Contains(combined, "OPUS"):
		return "OPUS", 55
	case strings.Contains(combined, "AAC"):
		return "AAC", 50
	case strings.Contains(combined, "MPEG AUDIO") || strings.Contains(combined, "MP3"):
		return "MP3", 40
	}
	return "", -1
}

func normalizeMediaInfoChannels(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if m := bdInfoChanRE.FindStringSubmatch(raw); len(m) == 2 {
		return m[1]
	}
	m := digitsRE.FindString(raw)
	if m == "" {
		return ""
	}
	switch m {
	case "8":
		return "7.1"
	case "6":
		return "5.1"
	case "2":
		return "2.0"
	case "1":
		return "1.0"
	}
	return ""
}

// --- Multilingual CLDR Language Lookup for MediaInfo/BDInfo/NFO ---

var (
	mediaInfoLangExact     map[string]string
	mediaInfoLangTitleWord map[string]string
	mediaInfoLangMultiWord map[string]string
	mediaInfoTokenRE       = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

var mediaInfoCatalogCodes = []string{
	"fr", "en", "es", "de", "it", "pt", "nl", "ru", "ja", "ko", "zh", "hi", "ar",
	"pl", "tr", "sv", "no", "da", "fi", "cs", "sk", "hu", "ro", "el", "he", "th",
	"vi", "id", "uk", "bg", "hr", "sr", "sl", "fa", "ur", "bn", "ta", "te", "ml",
	"kn", "mr", "gu", "pa", "ms", "tl", "ca", "eu", "gl", "is", "et", "lv", "lt",
	"ka", "hy", "az", "kk", "sq", "mk", "bs", "ga", "af", "sw", "be", "cy", "mn",
	"ne", "si", "km", "lo", "my", "uz",
}

func init() {
	mediaInfoLangExact = make(map[string]string, 2400)
	mediaInfoLangTitleWord = make(map[string]string, 2400)
	mediaInfoLangMultiWord = make(map[string]string, 256)

	for _, code := range mediaInfoCatalogCodes {
		mediaInfoLangExact[code] = code
	}
	// ISO 639-2 / regional aliases & unambiguous dub tags used in MediaInfo Language/Title lines.
	aliases := map[string]string{
		"fra": "fr", "fre": "fr", "french": "fr", "francais": "fr", "quebecois": "fr",
		"vff": "fr", "vfq": "fr", "vfi": "fr", "vof": "fr", "vf2": "fr", "vfb": "fr", "truefrench": "fr",
		"eng": "en", "english": "en", "anglais": "en",
		"spa": "es", "esp": "es", "spanish": "es", "espanol": "es", "castellano": "es", "latino": "es",
		"ger": "de", "deu": "de", "german": "de", "deutsch": "de", "allemand": "de",
		"ita": "it", "italian": "it", "italiano": "it", "italien": "it",
		"por": "pt", "pob": "pt", "ptbr": "pt", "portuguese": "pt", "portugues": "pt", "brazilian": "pt", "brasileiro": "pt", "dublado": "pt",
		"dut": "nl", "nld": "nl", "dutch": "nl", "nederlands": "nl", "vlaams": "nl", "flemish": "nl",
		"rus": "ru", "russian": "ru", "russe": "ru",
		"jpn": "ja", "jap": "ja", "japanese": "ja", "japonais": "ja",
		"kor": "ko", "korean": "ko", "coreen": "ko",
		"zho": "zh", "chi": "zh", "cmn": "zh", "yue": "zh", "chinese": "zh", "chinois": "zh", "mandarin": "zh", "cantonese": "zh", "cantonais": "zh",
		"hin": "hi", "hindi": "hi",
		"ara": "ar", "arabic": "ar", "arabe": "ar",
		"pol": "pl", "polish": "pl", "polonais": "pl", "polski": "pl", "lektor": "pl",
		"tur": "tr", "turkish": "tr", "turkce": "tr", "dublaj": "tr",
		"swe": "sv", "swedish": "sv", "svenska": "sv",
		"nor": "no", "nob": "no", "nno": "no", "norwegian": "no", "norsk": "no",
		"dan": "da", "danish": "da", "dansk": "da",
		"fin": "fi", "finnish": "fi", "suomi": "fi",
		"ces": "cs", "cze": "cs", "czech": "cs", "cestina": "cs",
		"slk": "sk", "slo": "sk", "slovak": "sk",
		"hun": "hu", "hungarian": "hu", "magyar": "hu",
		"ron": "ro", "rum": "ro", "romanian": "ro", "romana": "ro",
		"ell": "el", "gre": "el", "greek": "el",
		"heb": "he", "hebrew": "he", "ivrit": "he",
		"tha": "th", "thai": "th",
		"vie": "vi", "vietnamese": "vi",
		"ind": "id", "indonesian": "id",
		"ukr": "uk", "ukrainian": "uk",
		"bul": "bg", "bulgarian": "bg",
		"hrv": "hr", "croatian": "hr",
		"srp": "sr", "scc": "sr", "serbian": "sr",
		"slv": "sl", "slovenian": "sl", "slovene": "sl",
		"fas": "fa", "per": "fa", "persian": "fa", "farsi": "fa",
		"urd": "ur", "ben": "bn", "tam": "ta", "tel": "te", "mal": "ml", "kan": "kn",
		"mar": "mr", "guj": "gu", "pan": "pa", "msa": "ms", "may": "ms", "tgl": "tl", "fil": "tl",
		"cat": "ca", "eus": "eu", "baq": "eu", "glg": "gl", "isl": "is", "ice": "is",
		"est": "et", "lav": "lv", "lit": "lt", "kat": "ka", "geo": "ka", "hye": "hy", "arm": "hy",
		"aze": "az", "kaz": "kk", "sqi": "sq", "alb": "sq", "mkd": "mk", "mac": "mk",
		"bos": "bs", "gle": "ga", "afr": "af", "swa": "sw", "bel": "be", "cym": "cy", "wel": "cy",
	}
	for k, v := range aliases {
		mediaInfoLangExact[k] = v
		if utf8.RuneCountInString(k) >= 4 || k == "vff" || k == "vfq" || k == "vfi" || k == "vof" || k == "vf2" || k == "vfb" {
			mediaInfoLangTitleWord[k] = v
		}
	}

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
	ambigSingle := map[string]bool{}
	ambigMulti := map[string]bool{}
	for _, code := range mediaInfoCatalogCodes {
		tag := language.Make(code)
		for _, namer := range namers {
			raw := namer.Name(tag)
			if raw == "" {
				continue
			}
			folded := foldMediaInfoToken(raw)
			if folded == "" {
				continue
			}
			if strings.ContainsAny(folded, " -()/,.·'") {
				if prev, exists := cldrMulti[folded]; exists && prev != code {
					ambigMulti[folded] = true
				} else {
					cldrMulti[folded] = code
				}
				continue
			}
			if isMediaInfoLatin(folded) && utf8.RuneCountInString(folded) < 4 {
				continue
			}
			if prev, exists := cldrSingle[folded]; exists && prev != code {
				ambigSingle[folded] = true
			} else {
				cldrSingle[folded] = code
			}
		}
	}
	for k, code := range cldrSingle {
		if ambigSingle[k] {
			continue
		}
		if _, exists := mediaInfoLangExact[k]; !exists {
			mediaInfoLangExact[k] = code
		}
		if _, exists := mediaInfoLangTitleWord[k]; !exists {
			mediaInfoLangTitleWord[k] = code
		}
	}
	for k, code := range cldrMulti {
		if !ambigMulti[k] {
			mediaInfoLangMultiWord[k] = code
		}
	}
}

func foldMediaInfoToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
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

func isMediaInfoLatin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return false
		}
	}
	return true
}

func parseMediaInfoTrackLanguage(langField, titleField string) string {
	if code := lookupMediaInfoLanguage(langField); code != "" {
		return code
	}
	// Fallback to unambiguous language names / dub tags in Track Title ONLY when
	// the track Language header is absent or Undetermined.
	if titleField != "" {
		return lookupMediaInfoLanguageInTitle(titleField)
	}
	return ""
}

func lookupMediaInfoLanguage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	folded := foldMediaInfoToken(raw)
	switch folded {
	case "", "und", "undetermined", "unknown", "inconnu", "inconnue", "unbekannt", "desconocido", "sconosciuto", "zxx", "mul", "multi", "multiple", "multiple languages", "none", "n/a":
		return ""
	}
	if code, ok := mediaInfoLangExact[folded]; ok {
		return code
	}
	if code, ok := mediaInfoLangMultiWord[folded]; ok {
		return code
	}
	// Strip parenthetical regional qualifiers like "French (France)" or "Chinese (Simplified)".
	if idx := strings.IndexByte(raw, '('); idx > 0 {
		if code := lookupMediaInfoLanguage(raw[:idx]); code != "" {
			return code
		}
	}
	// BCP-47 tags like "fr-FR", "pt-BR", "zh-Hans", "en_US".
	if strings.ContainsAny(raw, "-_") && !strings.Contains(raw, " ") {
		if code := model.NormalizeLanguage(strings.ReplaceAll(raw, "_", "-")); code != "" && code != "xx" && code != "zxx" && code != "mul" {
			return code
		}
	}
	// Try safe multi-word / single-word tokens inside a label like "Français (VFF)" or "English 5.1".
	return lookupMediaInfoLanguageInTitle(raw)
}

func lookupMediaInfoLanguageInTitle(raw string) string {
	foldedWhole := foldMediaInfoToken(raw)
	if code, ok := mediaInfoLangMultiWord[foldedWhole]; ok {
		return code
	}
	for _, tok := range mediaInfoTokenRE.FindAllString(raw, -1) {
		f := foldMediaInfoToken(tok)
		if code, ok := mediaInfoLangTitleWord[f]; ok {
			return code
		}
	}
	return ""
}

func lookupAllMediaInfoLanguages(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	folded := foldMediaInfoToken(raw)
	switch folded {
	case "", "und", "undetermined", "unknown", "inconnu", "inconnue", "unbekannt", "desconocido", "sconosciuto", "zxx", "mul", "multi", "multiple", "multiple languages", "none", "n/a":
		return nil
	}
	if code, ok := mediaInfoLangExact[folded]; ok {
		return []string{code}
	}
	if code, ok := mediaInfoLangMultiWord[folded]; ok {
		return []string{code}
	}
	if strings.ContainsAny(raw, "-_") && !strings.Contains(raw, " ") {
		if code := model.NormalizeLanguage(strings.ReplaceAll(raw, "_", "-")); code != "" && code != "xx" && code != "zxx" && code != "mul" {
			return []string{code}
		}
	}
	var out []string
	for _, tok := range mediaInfoTokenRE.FindAllString(raw, -1) {
		f := foldMediaInfoToken(tok)
		if code, ok := mediaInfoLangTitleWord[f]; ok && !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	return out
}
