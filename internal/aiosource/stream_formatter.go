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

func formatStream(stream Stream, rec model.AIORecord) Stream {
	name := "AIOSources"
	if stream.Hints.Service != "" {
		service, marker := model.ServiceName(stream.Hints.Service), ""
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
		name = prefix + name + "\n" + status + " · " + service
	}
	lines := []string{rec.Title}
	// Prefer the selected file for episode-specific tags; the folder supplies
	// release metadata absent from otherwise bare episode filenames.
	technical := rec.Title[presentationMetadataStart(rec.Title):]
	if stream.Hints.Filename != "" && stream.Hints.Filename != rec.Title {
		technical += " " + stream.Hints.Filename[presentationMetadataStart(stream.Hints.Filename):]
	}
	groups := []string{"resolution", "video", "visual", "audio", "edition"}
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
		if len(values) > 0 {
			lines = append(lines, presentationTags[i].icon+" "+strings.Join(values, " • "))
		}
	}
	if label := streamLanguageLabel(streamLanguages(rec)); label != "" {
		name += "\n🌐 " + label
	}
	subtitles := []string{}
	for _, lang := range streamLanguages(model.AIORecord{Languages: rec.SubtitleLanguages}) {
		subtitles = append(subtitles, strings.ToUpper(lang.code))
	}
	if subtitleMarker.MatchString(rec.Title) && !slices.Contains(subtitles, "FR") {
		subtitles = append(subtitles, "FR")
	}
	if len(subtitles) > 0 {
		lines = append(lines, "💬 "+strings.Join(subtitles, "/")+" subtitles")
	}
	size := stream.Hints.Size
	if size > 0 {
		label := "📦 " + streamSize(size)
		if ParseRelease(rec.Title).Pack && stream.Hints.FolderName == "" {
			label += " · Season pack"
		}
		if stream.Hints.Duration > 0 {
			label += fmt.Sprintf(" • %.1f Mbps", float64(size)*8/stream.Hints.Duration/1e6)
		} else if rec.Bitrate > 0 && !ParseRelease(rec.Title).Pack {
			label += fmt.Sprintf(" • %.1f Mbps", rec.Bitrate/1e6)
		}
		lines = append(lines, label)
	}
	if size <= 0 && rec.Bitrate > 0 && !ParseRelease(rec.Title).Pack {
		lines = append(lines, fmt.Sprintf("📶 %.1f Mbps", rec.Bitrate/1e6))
	}
	if stream.Hints.Duration > 0 {
		seconds := int64(stream.Hints.Duration)
		lines = append(lines, fmt.Sprintf("⏱️ %dh %02dm", seconds/3600, seconds%3600/60))
	}
	if stream.Hints.Indexer != "" {
		lines = append(lines, "🧭 "+stream.Hints.Indexer)
	}
	for _, group := range rec.Presentation["group"] {
		if presentationGroup.MatchString(group) {
			lines = append(lines, "🧩 "+group)
			break
		}
	}
	if stream.Hints.PublishDate != "" {
		lines = append(lines, "📅 "+strings.Split(stream.Hints.PublishDate, "T")[0])
	}
	stream.Name = name
	stream.Title = strings.Join(lines, "\n")
	return stream
}
