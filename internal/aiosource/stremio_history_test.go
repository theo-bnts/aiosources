package aiosource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestStremioHistoryRecordsActualSelectionWithoutCredentialOrPlaybackURLs(t *testing.T) {
	ctx, s, conn := testService(t)
	seedMovie(t, ctx, s, conn, "one", "1", strings.Repeat("a", 40))
	seedMovie(t, ctx, s, conn, "two", "1", strings.Repeat("b", 40))
	if _, err := conn.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{info_hash}','"invalid hash"') WHERE provider_id='two'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Match(ctx); err != nil {
		t.Fatal(err)
	}
	token, _ := testPrivateInstallation(t, ctx, s)
	out, err := s.Streams(ctx, "https://public.invalid", token, "movie", "tt1234567")
	if err != nil || len(out) != 1 {
		t.Fatalf("stream selection changed: %d %v", len(out), err)
	}
	list, err := s.DB.ListStremioRequests(ctx, model.ListOptions{})
	if err != nil || len(list.Items) != 1 {
		t.Fatal("request was not saved", err)
	}
	request, err := s.DB.StremioRequest(ctx, list.Items[0].ID)
	if err != nil || request.Returned != len(out) || request.Matched != 2 || request.Eligible != 1 || request.InvalidHash != 1 || len(request.Results) != 1 || request.Results[0].Title != out[0].Title || request.Results[0].Filename != out[0].Hints.Filename {
		t.Fatalf("history does not explain the actual response: %+v %v", request, err)
	}
	encoded, _ := json.Marshal(request)
	for _, private := range []string{token, "https://public.invalid", "magnet:", "behaviorHints", "sources", "infoHash"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("history retained private playback data")
		}
	}
	if _, err = s.Streams(ctx, "https://public.invalid", token, "movie", "https://private.invalid/secret"); err != nil {
		t.Fatal(err)
	}
	list, err = s.DB.ListStremioRequests(ctx, model.ListOptions{Status: "invalid"})
	if err != nil || len(list.Items) != 1 || list.Items[0].Resource != "Invalid resource" {
		t.Fatal("invalid request persisted its raw resource", err)
	}
	s.RecordRejectedStream("movie", "tt1234567", true)
	list, err = s.DB.ListStremioRequests(ctx, model.ListOptions{Status: "busy"})
	if err != nil || len(list.Items) != 1 || list.Items[0].Resource != "imdb:tt1234567" {
		t.Fatal("overload rejection missing from history", err)
	}
}

func TestReviewedEpisodeOverridesOnlyListingEvidence(t *testing.T) {
	season, episode := 3, 4
	resource, _ := ParseID("tt1234567:3:4", "series")
	record := model.AIORecord{Title: "Example.S01E02", Season: &season, Episode: &episode, ReviewedEpisode: true}
	if !MatchesEpisode(record, ParseRelease(record.Title), resource) {
		t.Fatal("reviewed numbering not applied")
	}
	other, _ := ParseID("tt1234567:3:5", "series")
	if MatchesEpisode(record, ParseRelease(record.Title), other) {
		t.Fatal("review publishes a different episode")
	}
	if _, ok := SelectFile([]model.AIOFile{{Path: "Example.S01E02.mkv"}}, resource); ok {
		t.Fatal("review bypassed final file verification")
	}
}
