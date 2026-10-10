package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestManualIdentificationSurvivesMetadataUpdatesButRequiresReviewOfChangedSources(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, source := range []string{"one", "two"} {
		publishSharingRow(t, ctx, db, source, "item", `{"title":"Source release","info_hash":"`+strings.Repeat("a", 40)+`","download_url":"https://source.invalid/synthetic-private-value"}`, nil)
	}
	work := model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt1234567", Titles: []string{"Example Film"}, Years: []int{2021}}
	if err := db.SaveAIOWork(ctx, work, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	items, err := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{ProviderID: "one"})
	if err != nil || len(items.Items) != 1 {
		t.Fatal(err)
	}
	occurrence := items.Items[0].OccurrenceID
	detail, err := db.AIOReviewDetail(ctx, occurrence)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := db.AIOReviewCandidates(ctx, "tt1234567", "tt1234567", "")
	if err != nil || len(candidates) != 1 {
		t.Fatal(err)
	}
	w := candidates[0]
	input := model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: w.ID, WorkRevision: w.Revision}
	if err = db.SaveAIOReview(ctx, occurrence, input); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveAIOReview(ctx, occurrence, input); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("concurrent review was accepted: %v", err)
	}
	streams, err := db.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 1 || streams[0].Provider != "one" {
		t.Fatalf("review crossed source identities: %+v %v", streams, err)
	}
	var native string
	if err = db.pool.QueryRow(ctx, `SELECT fields->>'download_url' FROM ingest.torrents WHERE provider_id='one'`).Scan(&native); err != nil || native != "https://source.invalid/synthetic-private-value" {
		t.Fatal("review rewrote source data", err)
	}
	work.Titles = append(work.Titles, "Example Alternative")
	if err = db.SaveAIOWork(ctx, work, []string{"example film", "example alternative"}); err != nil {
		t.Fatal(err)
	}
	decide := func(record model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
		return model.AIOMatchResult{Record: record, Status: "unmatched", Reason: "automatic test"}, nil
	}
	picks, err := db.AIOMatchBatch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	results, err := db.MatchAIOBatch(ctx, picks, decide)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Record.Provider == "one" && (result.Status != "matched" || result.Reason != "manually reviewed" || result.Work.Revision <= w.Revision) {
			t.Fatalf("automatic update replaced review: %+v", result)
		}
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{size}','1000') WHERE provider_id='one'`); err != nil {
		t.Fatal(err)
	}
	picks, _ = db.AIOMatchBatch(ctx, 10)
	results, err = db.MatchAIOBatch(ctx, picks, decide)
	if err != nil || len(results) != 1 || results[0].Status != "matched" || results[0].Reason != "manually reviewed" {
		t.Fatal("annex updates invalidated a reviewed identity", err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Changed release"') WHERE provider_id='one'`); err != nil {
		t.Fatal(err)
	}
	if err = db.SaveAIOReview(ctx, occurrence, model.AIOReviewInput{Fingerprint: input.Fingerprint, Revision: 1, WorkID: w.ID, WorkRevision: w.Revision}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale source was reviewed", err)
	}
	picks, _ = db.AIOMatchBatch(ctx, 10)
	results, err = db.MatchAIOBatch(ctx, picks, decide)
	if err != nil || len(results) != 1 || results[0].Reason != "reviewed source changed" {
		t.Fatalf("changed source silently reused review: %+v %v", results, err)
	}
	streams, err = db.AIOStreams(ctx, "imdb", "tt1234567", "movie")
	if err != nil || len(streams) != 0 {
		t.Fatal("obsolete manual correspondence published", err)
	}
	detail, err = db.AIOReviewDetail(ctx, occurrence)
	if err != nil || detail.Work != nil {
		t.Fatal("outdated work shown as current", err)
	}
	if err = db.SaveAIOReview(ctx, occurrence, model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, Revision: detail.Review.Revision, Automatic: true}); err != nil {
		t.Fatal(err)
	}
	picks, _ = db.AIOMatchBatch(ctx, 10)
	results, err = db.MatchAIOBatch(ctx, picks, decide)
	if err != nil || len(results) != 1 || results[0].Reason != "automatic test" {
		t.Fatal("automatic matching was not restored", err)
	}
}

func TestReviewedMetadataDeletionAndRestorationRemainSafe(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "one", "item", `{"title":"Example series","season":1,"episode":2}`, nil)
	work := model.AIOWork{Type: "tv", TMDB: "201", IMDb: "tt2345678", Titles: []string{"Example Series"}, Years: []int{2022}}
	if err := db.SaveAIOWork(ctx, work, []string{"example series"}); err != nil {
		t.Fatal(err)
	}
	items, _ := db.ListAIOMatchDiagnostics(ctx, model.ListOptions{})
	id := items.Items[0].OccurrenceID
	detail, _ := db.AIOReviewDetail(ctx, id)
	works, _ := db.AIOReviewCandidates(ctx, "tt2345678", "tt2345678", "")
	season, episode := 3, 4
	input := model.AIOReviewInput{Fingerprint: detail.Record.Fingerprint, WorkID: works[0].ID, WorkRevision: works[0].Revision, OverrideEpisode: true, Season: &season, Episode: &episode}
	if err := db.SaveAIOReview(ctx, id, input); err != nil {
		t.Fatal(err)
	}
	record, err := db.AIOTorrent(ctx, "one", "item")
	if err != nil || !record.ReviewedEpisode || *record.Season != 3 || *record.Episode != 4 {
		t.Fatal("reviewed episode not applied to playback", err)
	}
	if err = db.DeleteAIOWork(ctx, "tv", "201"); err != nil {
		t.Fatal(err)
	}
	decide := func(record model.AIORecord, _ AIOCandidateSearch) (model.AIOMatchResult, error) {
		t.Fatal("active review reached automatic matcher")
		return model.AIOMatchResult{}, nil
	}
	picks, _ := db.AIOMatchBatch(ctx, 10)
	results, err := db.MatchAIOBatch(ctx, picks, decide)
	if err != nil || len(results) != 1 || results[0].Reason != "reviewed information unavailable" {
		t.Fatalf("deleted reviewed work: %+v %v", results, err)
	}
	if err = db.SaveAIOWork(ctx, work, []string{"example series"}); err != nil {
		t.Fatal(err)
	}
	picks, _ = db.AIOMatchBatch(ctx, 10)
	results, err = db.MatchAIOBatch(ctx, picks, decide)
	if err != nil || len(results) != 1 || results[0].Status != "matched" {
		t.Fatalf("restored information did not preserve review: %+v %v", results, err)
	}
	if _, err = db.AIOReviewCandidates(ctx, "ex", "ex", ""); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("unbounded short search accepted")
	}
}
