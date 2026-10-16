package store

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

func readAIOPresentation(f map[string]any, r *model.AIORecord) {
	// Retain only presentation fields already supported by compact storage.
	// Never carry arbitrary source attributes into addon output or history.
	attrs, _ := f["attributes"].(map[string]any)
	r.Presentation = map[string][]string{}
	var values func(any) []string
	values = func(v any) []string {
		out := []string{}
		switch v := v.(type) {
		case []any:
			for _, part := range v {
				out = append(out, values(part)...)
				if len(out) >= 32 {
					break
				}
			}
		case string:
			if len(v) <= 256 {
				out = append(out, v)
			}
		case json.Number:
			out = append(out, string(v))
		}
		if len(out) > 32 {
			out = out[:32]
		}
		return out
	}
	for group, keys := range map[string][]string{
		"resolution": {"resolution"}, "video": {"quality", "source", "codec", "video_codec", "videocodec"},
		"visual": {"hdr"}, "audio": {"audio_codec", "audiocodec", "audio_channels", "audiochannels"},
		"edition": {"edition"}, "group": {"release_group"},
	} {
		for _, key := range keys {
			r.Presentation[group] = append(r.Presentation[group], values(f[key])...)
			r.Presentation[group] = append(r.Presentation[group], values(attrs[key])...)
		}
	}
	for _, key := range []string{"proper", "repack", "hdr"} {
		if f[key] == true || f[key] == "true" || f[key] == json.Number("1") || attrs[key] == true || attrs[key] == "true" || attrs[key] == json.Number("1") {
			group := "video"
			if key == "hdr" {
				group = "visual"
			}
			r.Presentation[group] = append(r.Presentation[group], strings.ToUpper(key))
		}
	}
	for _, value := range append(values(f["sublangs"]), values(attrs["sublangs"])...) {
		r.SubtitleLanguages = append(r.SubtitleLanguages, value)
	}
	// Attached subtitle URLs are deliberately not copied or fetched.
	if subtitles, ok := f["subtitles"].([]any); ok {
		for _, subtitle := range subtitles {
			if object, ok := subtitle.(map[string]any); ok {
				r.SubtitleLanguages = append(r.SubtitleLanguages, append(values(object["lang"]), values(object["language"])...)...)
			} else {
				r.SubtitleLanguages = append(r.SubtitleLanguages, values(subtitle)...)
			}
			if len(r.SubtitleLanguages) >= 32 {
				break
			}
		}
	}
	if r.DurationSeconds == 0 {
		if seconds, err := strconv.ParseFloat(stringValue(attrs["duration"]), 64); err == nil && seconds > 0 && !math.IsInf(seconds, 0) && seconds <= float64(1<<53-1)/1000 {
			r.DurationSeconds = seconds
		}
	}
	bitrate := f["bitrate"]
	if bitrate == nil {
		bitrate = attrs["bitrate"]
	}
	if value, err := strconv.ParseFloat(stringValue(bitrate), 64); err == nil && value > 0 && value <= 1e12 && !math.IsNaN(value) {
		r.Bitrate = value
	}
}
