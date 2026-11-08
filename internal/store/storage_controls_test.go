package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestGranularStorageSnapshotsAndHistoryAreIndependent(t *testing.T) {
	ctx, db := sharingDatabase(t)
	settings, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Settings.Options.AdditionalFields = []string{"description"}
	settings.Settings.Options.SuccessfulRecords = true
	settings.Settings.Options.RecordFields = true
	settings.Settings.Options.PublicationHistory = false
	if err = db.UpdateStorageSettings(ctx, settings.Settings); err != nil {
		t.Fatal(err)
	}
	p := model.Provider{ID: "fixture", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	current, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Settings.Options.AdditionalFields = []string{}
	current.Settings.Options.SuccessfulRecords = false
	if err = db.UpdateStorageSettings(ctx, current.Settings); err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{{SourceID: "one", Fields: map[string]any{"title": "Example", "info_hash": strings.Repeat("a", 40), "description": "selected", "discarded": "omitted"}}}, Done: true}, "one")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	var description, discarded string
	var copies, history int
	if err = db.pool.QueryRow(ctx, `SELECT fields->>'description',COALESCE(fields->>'discarded','') FROM ingest.torrents`).Scan(&description, &discarded); err != nil || description != "selected" || discarded != "" {
		t.Fatal("snapshot selection lost", description, discarded, err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.raw_records WHERE fields->>'description'='selected'`).Scan(&copies); err != nil || copies != 1 {
		t.Fatal("diagnostic choice not snapshotted", copies, err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.torrent_history`).Scan(&history); err != nil || history != 0 {
		t.Fatal("disabled history kept new copies", history, err)
	}
	current, _ = db.StorageOverview(ctx)
	current.Settings.Options.PublicationHistory = true
	if err = db.UpdateStorageSettings(ctx, current.Settings); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "fixture", "two", `{"title":"Second example"}`, nil)
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.torrent_history`).Scan(&history); err != nil || history != 1 {
		t.Fatal("history did not resume", history, err)
	}
}

func TestRetentionOverridesDoNotErasePublishedDataOrBorrowOtherCutoffs(t *testing.T) {
	ctx, db := sharingDatabase(t)
	settings, err := db.StorageRetention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Days["requests"] = 30
	settings.Days["logs"] = 0
	settings.RequestLimit = 0
	settings.RequestResults = false
	if err = db.UpdateStorageRetention(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = db.RecordActivity(ctx, "info", "service.started", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	item := model.StremioRequest{CreatedAt: time.Now().Add(-15 * 24 * time.Hour), MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "results", Returned: 1, Results: []model.StremioResult{{Title: "Example", Service: "direct", Cache: "direct", Size: 100}}}
	if err = db.RecordStremioRequest(ctx, item); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	page, err := db.ListStremioRequests(ctx, model.ListOptions{})
	if err != nil || page.Total != 1 {
		t.Fatal("default cutoff replaced request override", page.Total, err)
	}
	detail, err := db.StremioRequest(ctx, page.Items[0].ID)
	if err != nil || detail.Returned != 1 || len(detail.Results) != 0 || !detail.Truncated {
		t.Fatal("optional result details changed counts", detail, err)
	}
	var logs int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs`).Scan(&logs); err != nil || logs != 0 {
		t.Fatal("zero retention did not clean logs", logs, err)
	}
	settings, _ = db.StorageRetention(ctx)
	settings.Days["requests"] = 0
	if err = db.UpdateStorageRetention(ctx, settings); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	page, err = db.ListStremioRequests(ctx, model.ListOptions{})
	if err != nil || page.Total != 0 {
		t.Fatal("zero retention did not clean requests", err)
	}
}

func TestOptionalStorageIndexesHaveBoundedReversibleDDL(t *testing.T) {
	ctx, db := sharingDatabase(t)
	settings, err := db.StorageIndexes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range settings.Status {
		if !status.Ready || !status.Enabled {
			t.Fatal("enabled indexes not ready", status)
		}
	}
	settings.Values["raw_records_fragments"] = false
	if err = db.UpdateStorageIndexes(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateStorageIndexes(ctx, settings); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale index settings accepted", err)
	}
	if changed, err := db.ReconcileStorageIndexes(ctx); err != nil || !changed {
		t.Fatal("optional index was not dropped", changed, err)
	}
	settings, _ = db.StorageIndexes(ctx)
	for _, status := range settings.Status {
		if !status.Ready {
			t.Fatal("disabled index still pending", status)
		}
	}
	settings.Values["raw_records_fragments"] = true
	if err = db.UpdateStorageIndexes(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if changed, err := db.ReconcileStorageIndexes(ctx); err != nil || !changed {
		t.Fatal("optional index was not rebuilt", changed, err)
	}
	settings, _ = db.StorageIndexes(ctx)
	settings.Values["torrents_pkey"] = false
	if err = db.UpdateStorageIndexes(ctx, settings); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("required index could be configured", err)
	}
}
