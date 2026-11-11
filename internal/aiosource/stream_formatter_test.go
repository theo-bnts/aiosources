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
		"🔍 AIOSources 🧭 Example Library 🌐 AllDebrid",
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

func TestFormatterShowsReleasePlatformsWithoutInventingDesktopCompatibility(t *testing.T) {
	for _, test := range []struct {
		name, title, want string
		platforms         []string
	}{
		{"native platform", "Example.Adventure.v1.2.3", "🎮 Windows", []string{"PC (Microsoft Windows)"}},
		{"platform in title", "Example.Adventure.PS5.v1.2.3", "🎮 PlayStation 5", nil},
		{"generic pc", "Example.Adventure.PC.v1.2.3", "🎮 PC", nil},
		{"linux desktop", "Example.Adventure.PC.Linux.v1.2.3", "🎮 Linux", nil},
		{"windows architecture", "Example.Adventure.WIN.X64.v1.2.3", "🎮 Windows", nil},
		{"switch generation", "Example.Adventure.Nintendo.Switch.2.v1.2.3", "🎮 Nintendo Switch 2", nil},
		{"mobile native platform", "Example.Adventure.v1.2.3", "🎮 Android", []string{"Android"}},
		{"unknown native field", "Example.Adventure.v1.2.3", "", []string{"https://private.example/credential"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := model.AIORecord{Title: test.title, GameHints: model.GameHints{Kind: "game", Platforms: test.platforms}}
			presentation := formatReleasePresentation(record, Behavior{})
			details := strings.Join(presentation.Details, "\n")
			if test.want != "" && !strings.Contains(details, test.want) || test.want == "" && strings.Contains(details, "🎮") {
				t.Fatal(details, test.want)
			}
			if !strings.Contains(details, "🏷️ v1.2.3") || strings.Contains(details, "private.example") || !strings.Contains(test.want, "Windows") && strings.Contains(details, "Windows") {
				t.Fatal("missing release version or invented platform", details)
			}
		})
	}
	video := model.AIORecord{Title: "Example.Linux.Story.2026.1080p", VideoType: "movie"}
	if details := strings.Join(formatReleasePresentation(video, Behavior{}).Details, "\n"); strings.Contains(details, "🎮") || strings.Contains(details, "📖") {
		t.Fatal("video presentation gained game or book metadata", details)
	}
	identified := model.AIORecord{Title: "Example Adventure", IdentificationType: "game", GameHints: model.GameHints{Platforms: []string{"Windows"}}}
	if details := strings.Join(formatReleasePresentation(identified, Behavior{}).Details, "\n"); !strings.Contains(details, "🎮 Windows") {
		t.Fatal("an identified game lost its declared release platform", details)
	}
}

func TestFormatterShowsOnlyUsableBibliographicDetails(t *testing.T) {
	record := model.AIORecord{
		Title:     "Morgan Vale - The Amber Orchard EPUB",
		BookHints: model.BookHints{Kind: "ebook", Author: "Morgan Vale", ISBN: "9780306406157", Format: "epub"},
	}
	details := formatReleasePresentation(record, Behavior{}).Details
	want := []string{"📖 Ebook • EPUB", "✍️ Morgan Vale", "🔖 ISBN 9780306406157"}
	if !reflect.DeepEqual(details[:min(len(details), 3)], want) {
		t.Fatal(details, want)
	}
	record.BookHints.Author = "https://private.example/credential"
	record.BookHints.Format = "https://private.example/secret-format"
	record.BookHints.ISBN = "not-an-isbn"
	if label := strings.Join(formatReleasePresentation(record, Behavior{}).Details, "\n"); strings.Contains(label, "private.example") || strings.Contains(label, "not-an-isbn") {
		t.Fatal("unvalidated source fields entered public presentation", label)
	}
	audio := model.AIORecord{Title: "Morgan Vale - The Amber Orchard M4B", BookHints: model.BookHints{Kind: "audiobook"}}
	if label := strings.Join(formatReleasePresentation(audio, Behavior{}).Details, "\n"); !strings.Contains(label, "📖 Audiobook • M4B") {
		t.Fatal(label)
	}
}
