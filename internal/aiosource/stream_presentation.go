package aiosource

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

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

func releaseBehavior(rec model.AIORecord, sourceName string) Behavior {
	hints := Behavior{Size: max(0, rec.Size), Filename: rec.Title, Indexer: sourceName, Duration: rec.DurationSeconds}
	if rec.PublishedAt != nil {
		hints.PublishDate = rec.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	return hints
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
