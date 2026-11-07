package aiosource

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Raw filenames and structured hints remain untouched for AIOStreams. These
// labels are presentation only and never contribute matching evidence.
var presentationTags = []struct{ icon, pattern string }{
	{"🖼️", `(?i)\b(?:4320p|2160p|1440p|1080[pi]|720p|576p|480p|360p|4K|8K)\b`},
	{"🎞️", `(?i)\b(?:WEB[- .]?DL|WEBRip|WEB|Blu[- .]?Ray|BDRip|BRRip|REMUX|HDTV|DVDRip|DVD|HDCAM|CAM|TELESYNC|AV1|HEVC|[HX][ .]?26[45]|MPEG[ .-]?[24]|XVID|REPACK|PROPER)\b`},
	{"🎨", `(?i)\b(?:HDR10\+?|HDR|SDR|DOLBY[ .-]?VISION|DV|HLG|10[ .-]?BIT|12[ .-]?BIT|3D|H[- .]?SBS|H[- .]?OU)\b`},
	{"🔊", `(?i)\b(?:DTS[- .]?HD(?:[ .-]?MA)?|DTS[ .:]?X|DTS|TRUEHD|ATMOS|DDP|EAC3|E-AC-3|AC3|AC-3|AAC|FLAC|OPUS|MP3|PCM|[257]\.1(?:\.\d)?|2\.0|MONO|STEREO)\b`},
	{"✨", `(?i)\b(?:DIRECTOR.?S[ .-]?CUT|EXTENDED|UNRATED|UNCUT|REMASTERED|IMAX|THEATRICAL|CRITERION)\b`},
}
var compiledPresentationTags = func() []*regexp.Regexp {
	out := []*regexp.Regexp{}
	for _, tag := range presentationTags {
		out = append(out, regexp.MustCompile(tag.pattern))
	}
	return out
}()
var presentationGroup = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$`)
var subtitleMarker = regexp.MustCompile(`(?i)\b(?:VOSTFR|SUBFRENCH)\b`)
var presentationEncode = regexp.MustCompile(`(?i)^(?:AV1|HEVC|[HX][ .]?26[45]|MPEG[ .-]?[24]|XVID)$`)
var presentationChannels = regexp.MustCompile(`^(?:[257]\.1(?:\.\d)?|2\.0|MONO|STEREO)$`)
var presentationStereo = regexp.MustCompile(`^(?:3D|H[- .]?(?:SBS|OU))$`)

func formatStream(stream Stream, rec model.AIORecord) Stream {
	name := []string{}
	if stream.Hints.Service != "" {
		marker := ""
		for _, provider := range model.DebridProviders() {
			if provider.ID == stream.Hints.Service {
				marker = provider.Marker
			}
		}
		status := "❓ Cache unknown"
		prefix := ""
		if stream.Hints.Cached != nil {
			if *stream.Hints.Cached {
				status = "⚡ Cached"
				prefix = "[" + marker + "+] "
			} else {
				status = "⬇️ Download"
				prefix = "[" + marker + " download] "
			}
		}
		name = append(name, prefix+status)
	}
	presentation := formatReleasePresentation(rec, stream.Hints)
	name = append(name, presentation.Highlights...)
	if presentation.Language != "" {
		name = append(name, presentation.Language)
	}
	if len(name) == 0 {
		name = append(name, "AIOSources")
	}
	stream.Name = strings.Join(name, "\n")
	stream.Title = strings.Join(presentation.Details, "\n")
	return stream
}

// Search and Stremio share these labels. The caller chooses which source name
// may be shown; this formatter never falls back to a private source identity.
func formatReleasePresentation(rec model.AIORecord, hints Behavior) model.ReleasePresentation {
	presentation := model.ReleasePresentation{Details: []string{}}
	// Prefer the selected file for episode-specific tags; the folder supplies
	// release metadata absent from otherwise bare episode filenames.
	technical := rec.Title[presentationMetadataStart(rec.Title):]
	if hints.Filename != "" && hints.Filename != rec.Title {
		technical += " " + hints.Filename[presentationMetadataStart(hints.Filename):]
	}
	groups := []string{"resolution", "video", "visual", "audio", "edition"}
	tags := map[string][]string{}
	for i, re := range compiledPresentationTags {
		native := strings.Join(rec.Presentation[groups[i]], " ")
		native = strings.ReplaceAll(native, "_", "-")
		if i == 0 {
			for _, height := range []string{"4320", "2160", "1440", "1080", "720", "576", "480", "360"} {
				if slices.Contains(rec.Presentation[groups[i]], height) {
					native += " " + height + "p"
				}
			}
		}
		seen := map[string]bool{}
		values := []string{}
		for _, value := range re.FindAllString(technical+" "+native, -1) {
			value = strings.ToUpper(value)
			if value == "HDR10" && strings.Contains(strings.ToUpper(technical+" "+native), "HDR10+") {
				value = "HDR10+"
			}
			if !seen[value] {
				seen[value] = true
				values = append(values, value)
			}
		}
		tags[groups[i]] = values
	}
	if slices.ContainsFunc(tags["visual"], presentationStereo.MatchString) {
		presentation.Highlights = append(presentation.Highlights, "🕶️ 3D")
	}
	duration := presentationDuration(hints.Duration)
	if len(tags["edition"]) > 0 {
		edition := "✨ " + strings.Join(tags["edition"], " • ")
		if duration != "" {
			edition += " (" + duration + ")"
		}
		presentation.Highlights = append(presentation.Highlights, edition)
	} else if duration != "" {
		presentation.Highlights = append(presentation.Highlights, "⏱️ "+duration)
	}
	if label := streamLanguageLabel(streamLanguages(rec)); label != "" {
		presentation.Language = "🌍 " + label
	}
	subtitles := streamLanguages(model.AIORecord{Languages: rec.SubtitleLanguages})
	if subtitleMarker.MatchString(rec.Title) && !slices.ContainsFunc(subtitles, func(value streamLanguage) bool { return value.code == "fr" }) {
		subtitles = append(subtitles, streamLanguage{code: "fr"})
	}
	if len(subtitles) > 0 {
		if presentation.Language != "" {
			presentation.Language += " • "
		}
		// Subtitle flags in the name would be mistaken for audio languages by
		// generic addon parsers. Full language names also avoid service aliases.
		presentation.Language += "💬 " + streamLanguageNames(subtitles) + " subtitles"
	}
	add := func(icon string, values []string) {
		if len(values) > 0 {
			presentation.Details = append(presentation.Details, icon+" "+strings.Join(values, " • "))
		}
	}
	add("🖼️", tags["resolution"])
	qualities, encodes, repairs := []string{}, []string{}, []string{}
	for _, value := range tags["video"] {
		switch {
		case presentationEncode.MatchString(value):
			encodes = append(encodes, value)
		case value == "REPACK" || value == "PROPER":
			repairs = append(repairs, value)
		default:
			qualities = append(qualities, value)
		}
	}
	add("🎞️", append(append(qualities, encodes...), repairs...))
	add("🎨", slices.DeleteFunc(tags["visual"], func(value string) bool { return value == "3D" }))
	audio, channels := []string{}, []string{}
	for _, value := range tags["audio"] {
		if presentationChannels.MatchString(value) {
			channels = append(channels, value)
		} else {
			audio = append(audio, value)
		}
	}
	add("🔊", append(audio, channels...))
	size := hints.Size
	pack := false
	if size > 0 || rec.Bitrate > 0 {
		pack = ParseRelease(rec.Title).Pack
	}
	if size > 0 {
		label := "📦 " + streamSize(size)
		if pack && hints.FolderName == "" {
			label += " · Season pack"
		}
		if hints.Duration > 0 {
			label += fmt.Sprintf(" • %.1f Mbps", float64(size)*8/hints.Duration/1e6)
		} else if rec.Bitrate > 0 && !pack {
			label += fmt.Sprintf(" • %.1f Mbps", rec.Bitrate/1e6)
		}
		presentation.Details = append(presentation.Details, label)
	}
	if size <= 0 && rec.Bitrate > 0 && !pack {
		presentation.Details = append(presentation.Details, fmt.Sprintf("📦 %.1f Mbps", rec.Bitrate/1e6))
	}
	extra := []string{}
	for _, group := range rec.Presentation["group"] {
		if presentationGroup.MatchString(group) {
			extra = append(extra, "🧩 "+group)
			break
		}
	}
	if hints.PublishDate != "" {
		extra = append(extra, "📅 "+strings.Split(hints.PublishDate, "T")[0])
	}
	if len(extra) > 0 {
		presentation.Details = append(presentation.Details, strings.Join(extra, " • "))
	}
	footer := []string{"🔍 AIOSources"}
	if hints.Indexer != "" {
		footer = append(footer, "🧭 "+hints.Indexer)
	}
	if hints.Service != "" {
		footer = append(footer, "🌐 "+model.ServiceName(hints.Service))
	}
	presentation.Details = append(presentation.Details, strings.Join(footer, " • "))
	return presentation
}

func presentationDuration(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	minutes := int64(seconds) / 60
	if minutes == 0 {
		return fmt.Sprintf("%ds", int64(seconds))
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}
