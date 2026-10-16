package aiosource

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStreamInteroperability(t *testing.T) {
	published := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	record := model.AIORecord{Provider: "example-id", Title: "Example.Movie.2021.1080p.WEB-DL.H264", Hash: strings.Repeat("a", 40), Size: 5000000000, Languages: []string{"fra", "eng"}, PublishedAt: &published, DurationSeconds: 5400}
	resource := Resource{Kind: "imdb", ID: "tt1234567", Type: "movie"}
	for _, service := range []string{"direct", "alldebrid", "torbox"} {
		for _, cache := range []string{"cached", "download", "unknown"} {
			if service == "direct" && cache != "unknown" {
				continue
			}
			t.Run(service+"/"+cache, func(t *testing.T) {
				stream := baseStream(record, resource, "Example Library")
				if service != "direct" {
					var cached *bool
					if cache != "unknown" {
						value := cache == "cached"
						cached = &value
					}
					stream = playbackStream(stream, record, service, cached, &model.AIOFile{Path: "Example.Movie.2021.1080p.WEB-DL.H264.mp4", Size: 4000000000}, "https://playback.example.invalid/movie")
				}
				data, err := json.Marshal(stream)
				if err != nil {
					t.Fatal(err)
				}
				var object map[string]any
				if err := json.Unmarshal(data, &object); err != nil {
					t.Fatal(err)
				}
				hints := object["behaviorHints"].(map[string]any)
				if object["description"] == "" || object["title"] != nil || object["infoHash"] != record.Hash || hints["indexer"] != "Example Library" || hints["publishDate"] != "2026-10-09T12:00:00Z" || hints["duration"] != float64(5400) {
					t.Fatalf("missing or conflicting standard metadata: %s", data)
				}
				if service != "direct" {
					if object["url"] != stream.URL || hints["service"] != service || hints["videoSize"] != float64(4000000000) || hints["folderSize"] != float64(record.Size) {
						t.Fatalf("debrid metadata lost: %s", data)
					}
					if cache == "unknown" {
						if _, exists := hints["cached"]; exists {
							t.Fatal("unknown cache was represented as a known result")
						}
					} else if hints["cached"] != (cache == "cached") {
						t.Fatal("cache is not an exact boolean")
					}
				}
				for _, private := range []string{"seeders", "peers", "attributes", "secret", "api_key", "videoHash"} {
					if _, exists := object[private]; exists {
						t.Fatalf("unsupported field %s returned", private)
					}
					if _, exists := hints[private]; exists {
						t.Fatalf("unsupported hint %s returned", private)
					}
				}
				// Synthetic responses also allow validation with the upstream
				// AIOStreams parser, without an account or live source request.
				t.Log("AIO_CONTRACT=" + string(data))
			})
		}
	}
}

func TestPlaybackMetadataSeparatesSeasonPacksAndWebReadiness(t *testing.T) {
	record := model.AIORecord{Title: "Example.Series.S01.1080p.WEB-DL", Hash: strings.Repeat("a", 40), Size: 10000000000, DurationSeconds: 20000}
	stream := baseStream(record, Resource{Type: "series"}, "Example Library")
	for _, fixture := range []struct {
		origin, path string
		webReady     bool
	}{
		{"https://playback.example.invalid/episode", "Example.Series.S01E02.mp4", true},
		{"https://playback.example.invalid/episode", "Example.Series.S01E02.mkv", false},
		{"http://playback.example.invalid/episode", "Example.Series.S01E02.mp4", false},
	} {
		result := playbackStream(stream, record, "torbox", nil, &model.AIOFile{Path: fixture.path, Size: 1000000000}, fixture.origin)
		if result.Hints.NotWebReady == fixture.webReady || result.Hints.Size != 1000000000 || result.Hints.FolderSize != record.Size || result.Hints.FolderName != record.Title || result.Hints.Duration != 0 {
			t.Fatalf("incorrect episode metadata: %+v", result.Hints)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		t.Log("AIO_PACK=" + string(data))
	}
	if result := playbackStream(stream, record, "torbox", nil, nil, "https://playback.example.invalid/episode"); !result.Hints.NotWebReady {
		t.Fatal("an unknown file was claimed to be web-ready")
	}
}

func TestPublishedStreamMetadataUsesOrdinaryAndTorznabLanguageFields(t *testing.T) {
	ctx, service, connection := testService(t)
	fields := map[string]any{"title": "Example.Movie.2021.1080p.WEB-DL", "info_hash": strings.Repeat("b", 40), "size": 1234, "published_at": "2026-10-09T14:00:00+02:00", "duration": "5400.25", "audiolangs": []string{"fra"}, "languages": []string{"eng"}, "attributes": map[string]any{"language": []string{"deu", "eng"}, "private_key": "not-for-publication"}}
	data, _ := json.Marshal(fields)
	if _, err := connection.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example','1',$1)`, data); err != nil {
		t.Fatal(err)
	}
	record, err := service.DB.AIOTorrent(ctx, "example", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(record.Languages, []string{"fra", "eng", "deu"}) || record.PublishedAt == nil || record.PublishedAt.Format(time.RFC3339) != "2026-10-09T12:00:00Z" || record.DurationSeconds != 5400.25 {
		t.Fatalf("published metadata was dropped: %+v", record)
	}
	stream := baseStream(record, Resource{Type: "movie"}, "Example Library")
	encoded, _ := json.Marshal(stream)
	if strings.Contains(string(encoded), "not-for-publication") || !strings.Contains(stream.Name, "🇩🇪") {
		t.Fatal("private attributes exposed or audio metadata lost")
	}
	fields["published_at"], fields["duration"] = "invalid", "NaN"
	data, _ = json.Marshal(fields)
	if _, err := connection.Exec(ctx, `UPDATE ingest.torrents SET fields=$1 WHERE provider_id='example'`, data); err != nil {
		t.Fatal(err)
	}
	record, err = service.DB.AIOTorrent(ctx, "example", "1")
	if err != nil || record.PublishedAt != nil || record.DurationSeconds != 0 {
		t.Fatal("invalid optional metadata should be omitted without dropping the record")
	}
}
