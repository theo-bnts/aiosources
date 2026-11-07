package aiosource

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestFormatterOrdersHighlightsAndGroupsTechnicalDetails(t *testing.T) {
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	record := model.AIORecord{
		Provider: "internal-source", Hash: strings.Repeat("a", 40), Size: 8_000_000_000,
		Title:           "Example.Film.2024.2160p.H265.PROPER.WEB-DL.HDR10+.3D.H-SBS.5.1.TRUEHD.EXTENDED.MULTI",
		DurationSeconds: 5400, Languages: []string{"fr"}, SubtitleLanguages: []string{"en"},
		Presentation: map[string][]string{"group": {"ExampleGroup"}}, PublishedAt: &at,
	}
	base := baseStream(record, Resource{Type: "movie"}, "Example Library")
	cached := true
	stream := playbackStream(base, record, "alldebrid", &cached, nil, "https://files.example/video")
	name := strings.Split(stream.Name, "\n")
	wantName := []string{
		"[AD+] ⚡ Cached", "🕶️ 3D", "✨ EXTENDED (1h 30m)",
		"🌍 MULTI / French 🇫🇷 • 💬 English subtitles",
	}
	if !reflect.DeepEqual(name, wantName) {
		t.Fatalf("name ordering: %q, want %q", name, wantName)
	}
	wantDescription := []string{
		"🖼️ 2160P", "🎞️ WEB-DL • H265 • PROPER", "🎨 HDR10+ • H-SBS",
		"🔊 TRUEHD • 5.1", "📦 7.45 GB • 11.9 Mbps",
		"🧩 ExampleGroup • 📅 2026-10-09",
		"🔍 AIOSources • 🧭 Example Library • 🌐 AllDebrid",
	}
	if got := strings.Split(stream.Title, "\n"); !reflect.DeepEqual(got, wantDescription) {
		t.Fatalf("description ordering: %q, want %q", got, wantDescription)
	}
	if stream.InfoHash != record.Hash || stream.Hints.Filename != record.Title || stream.Hints.Size != record.Size || stream.Hints.Duration != record.DurationSeconds || stream.Hints.Indexer != "Example Library" || stream.Hints.Cached != &cached || stream.Hints.Service != "alldebrid" || stream.Hints.BingeGroup != base.Hints.BingeGroup+"|alldebrid" {
		t.Fatal("presentation changed playback or structured metadata", stream)
	}
	if strings.Contains(stream.Name+stream.Title, "internal-source") || strings.Contains(stream.Title, "🕶️") || strings.Contains(stream.Title, "🎨 3D") {
		t.Fatal("private identities or duplicate 3D labels escaped the formatter", stream)
	}
}

func TestFormatterOmitsMissingHighlightsAndKeepsAudioSeparateFromSubtitles(t *testing.T) {
	for _, test := range []struct {
		name     string
		record   model.AIORecord
		wantName string
	}{
		{"no metadata", model.AIORecord{Title: "Example Film"}, "AIOSources"},
		{"short duration", model.AIORecord{Title: "Example.2024", DurationSeconds: 45}, "⏱️ 45s"},
		{"duration without edition", model.AIORecord{Title: "Example.2024", DurationSeconds: 1800}, "⏱️ 30m"},
		{"subtitle language only", model.AIORecord{Title: "Example.2024", SubtitleLanguages: []string{"eng"}}, "💬 English subtitles"},
		{"title words are not metadata", model.AIORecord{Title: "Extended 3D Story.2024.1080p"}, "AIOSources"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := baseStream(test.record, Resource{Type: "movie"}, "")
			if stream.Name != test.wantName {
				t.Fatal(stream.Name, test.wantName)
			}
			for _, invented := range []string{"📦", "Unknown size", "🏅", "👥", "📚", "✨", "🇬🇧", "🇫🇷"} {
				if strings.Contains(stream.Name+stream.Title, invented) {
					t.Fatal("missing metadata was invented", invented, stream)
				}
			}
			if strings.Contains(stream.Name+stream.Title, "\n\n") {
				t.Fatal("empty presentation line", stream)
			}
		})
	}
}
