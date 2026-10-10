package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDistinctRecordsMigrationPreservesHistoryAndResume(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "distinct-migration", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	save := func(page model.Page, fingerprint string) {
		t.Helper()
		var err error
		run, err = db.SavePage(ctx, run, page, fingerprint)
		if err != nil {
			t.Fatal(err)
		}
	}
	save(model.Page{Items: []model.Record{coverageRecord("old"), coverageRecord("keep")}, Next: json.RawMessage(`{"page":2}`)}, "old")
	checkpoint := append([]byte(nil), run.Cursor...)
	ignored := coverageRecord("ignored")
	ignored.Ignored = true
	auxiliary := coverageRecord("auxiliary")
	auxiliary.Auxiliary = true
	invalid := coverageRecord("invalid")
	invalid.Error = "Invalid source record"
	save(model.Page{
		ResetStaging: true,
		Items:        []model.Record{coverageRecord("keep"), coverageRecord("new"), coverageRecord("keep"), ignored, auxiliary, invalid},
		Next:         json.RawMessage(`{"page":3}`),
	}, "reset")
	if run.DistinctRecords != 2 || !bytes.Equal(run.Cursor, checkpoint) {
		t.Fatalf("rejected full reset changed its accepted generation: %+v", run)
	}
	save(model.Page{
		ResetStaging: true,
		Items:        []model.Record{coverageRecord("keep"), coverageRecord("new"), coverageRecord("keep"), ignored, auxiliary},
		Next:         json.RawMessage(`{"page":3}`),
	}, "reset")
	save(model.Page{Items: []model.Record{coverageRecord("rejected")}, Error: "Rejected source page", ResetStaging: true}, "")
	if run.DistinctRecords != 2 {
		t.Fatalf("rejected pages changed the current traversal's identity count: %+v", run)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	preview := mirrorClaim(t, ctx, db, model.Provider{ID: "preview-migration", Adapter: "http_json"}, model.ModePreview)
	preview, err := db.SavePage(ctx, preview, model.Page{Items: []model.Record{coverageRecord("keep"), coverageRecord("new"), coverageRecord("keep")}, Done: true}, "preview")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, preview.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	before, err := db.ListRaw(ctx, model.ListOptions{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the immediately preceding schema while retaining its original
	// archives, checkpoints and raw counters. Migration must rebuild evidence
	// for previews as well as only the latest accepted full-run generation.
	if _, err := db.pool.Exec(ctx, `DROP TABLE ingest.run_record_identities;
ALTER TABLE ingest.runs DROP COLUMN distinct_records;
DELETE FROM ingest.schema_migrations WHERE version=14`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, prior := range []model.Run{run, preview} {
		restored, err := db.GetRun(ctx, prior.ID)
		if err != nil || restored.DistinctRecords != 2 || restored.Records != prior.Records || restored.Pages != prior.Pages || !bytes.Equal(restored.Cursor, prior.Cursor) {
			t.Fatalf("historical run count or checkpoint was not reconstructed: %+v %v", restored, err)
		}
	}
	after, err := db.ListRaw(ctx, model.ListOptions{Limit: 200})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("count reconstruction changed retained observations: before=%+v after=%+v error=%v", before, after, err)
	}
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || resumed.ID != run.ID {
		t.Fatalf("could not resume migrated run: %+v %v", resumed, err)
	}
	run = *resumed
	save(model.Page{Items: []model.Record{coverageRecord("keep"), coverageRecord("new"), coverageRecord("additional")}, Next: json.RawMessage(`{"page":4}`)}, "after-migration")
	if run.DistinctRecords != 3 {
		t.Fatalf("resume counted repeated historical IDs or merged different IDs sharing a hash: %+v", run)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
}

func TestKnownIDsKeepRunCreationBaselineAcrossWritesAndResume(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := model.Provider{ID: "baseline", Adapter: "http_json"}
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	seed, err := db.SavePage(ctx, seed, model.Page{Error: "Temporary source failure"}, "")
	if err != nil {
		t.Fatal(err)
	}
	seed, err = db.SavePage(ctx, seed, model.Page{Items: []model.Record{coverageRecord("249627")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	interrupted := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	interrupted, err = db.SavePage(ctx, interrupted, model.Page{
		Items: []model.Record{coverageRecord("interrupted")}, Next: json.RawMessage(`{"page":2}`),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, interrupted, model.StatusFailed)
	warnings := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	warnings, err = db.SavePage(ctx, warnings, model.Page{
		Items: []model.Record{coverageRecord("warnings"), {Error: "Missing source identity"}}, Done: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, warnings, model.StatusSucceeded)
	run := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{
		coverageRecord("249627"), coverageRecord("249961"), coverageRecord("boundary"),
	}, Next: json.RawMessage(`{"page":2}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$1 WHERE provider_id=$2 AND source_id='boundary'", run.CreatedAt, provider.ID); err != nil {
		t.Fatal(err)
	}
	assertBaseline := func(database *Store, baseline model.Run) {
		t.Helper()
		known, err := database.Known(ctx, provider.ID, []string{"249627", "249961", "boundary", "absent", "interrupted", "warnings"}, baseline.CreatedAt)
		if err != nil || !reflect.DeepEqual(known, map[string]bool{"249627": true}) {
			t.Fatalf("current-run writes or equal timestamps changed the prior-known boundary: %v %v", known, err)
		}
		published := mirrorRows(t, ctx, database, provider.ID)
		if published["249627"].SourceID != "249627" || published["249961"].SourceID != "249961" {
			t.Fatalf("distinct published native identities sharing one hash were collapsed: %+v", published)
		}
	}
	assertBaseline(db, run)
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	resumed, err := reopened.ResumeRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertBaseline(reopened, resumed)
}

func TestNativeDetailAttributesSurviveLaterListUpdates(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeIncremental, model.ModeFull} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, _, db := activityDatabase(t)
			provider := model.Provider{ID: "native-details", Adapter: "http_json"}
			enriched := coverageRecord("249627")
			enriched.Fields["attributes"] = map[string]any{"imdbid": []string{"tt123"}, "quality": []string{"10"}}
			enriched.Fields["external_ids"] = []any{map[string]any{"kind": "imdb", "value": "tt123"}}
			run := mirrorClaim(t, ctx, db, provider, mode)
			run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{enriched, coverageRecord("249961")}, Next: json.RawMessage(`{"page":2}`)}, "")
			if err != nil {
				t.Fatal(err)
			}
			// A later list page supplies some attribute values, not the
			// entire detail metadata. Supplied arrays still replace old ones.
			update := coverageRecord("249627")
			update.Fields["title"] = "Latest JSON title"
			update.Fields["attributes"] = map[string]any{"quality": []string{"20"}, "leechers": []string{"3"}}
			run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{update}, Done: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
			rows := mirrorRows(t, ctx, db, provider.ID)
			record, sibling := rows["249627"], rows["249961"]
			wantAttributes := map[string]any{"imdbid": []any{"tt123"}, "quality": []any{"20"}, "leechers": []any{"3"}}
			if len(rows) != 2 || sibling.SourceID != "249961" || sibling.Fields["attributes"] != nil || record.Fields["title"] != update.Fields["title"] ||
				!reflect.DeepEqual(record.Fields["attributes"], wantAttributes) ||
				!reflect.DeepEqual(record.Fields["external_ids"], []any{map[string]any{"kind": "imdb", "value": "tt123"}}) {
				t.Fatalf("detail metadata was erased, supplied arrays stayed stale, or hash siblings were merged: %+v", rows)
			}
		})
	}
}

func TestKnownPageStreakChangesOnlyWithAcceptedCheckpoint(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "known-streak"}, model.ModeFull)
	streak := 1
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("old")}, Next: json.RawMessage(`{"page":2}`), KnownPageStreak: &streak}, "")
	if err != nil {
		t.Fatal(err)
	}
	invalid := coverageRecord("invalid")
	invalid.Fields["title"] = "bad\u0000value"
	streak = 2
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{invalid}, Done: true, KnownPageStreak: &streak}, "")
	if err != nil || run.KnownPageStreak != 1 {
		t.Fatalf("rejected page changed known-page stopping state: streak=%d error=%v", run.KnownPageStreak, err)
	}
	run, err = db.SavePage(ctx, run, model.Page{}, "")
	if err != nil || run.KnownPageStreak != 1 {
		t.Fatalf("control page cleared known-page stopping state: streak=%d error=%v", run.KnownPageStreak, err)
	}
	streak = 0
	run, err = db.SavePage(ctx, run, model.Page{KnownPageStreak: &streak}, "")
	if err != nil || run.KnownPageStreak != 0 {
		t.Fatalf("accepted unknown page did not reset stopping state: streak=%d error=%v", run.KnownPageStreak, err)
	}
}
