package store

import (
	"context"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func sourceStats(t *testing.T, ctx context.Context, db *Store, source string) model.SourceTorrentStats {
	t.Helper()
	items, err := db.SourceTorrentStats(ctx, []string{source})
	if err != nil || len(items.Items) != 1 {
		t.Fatalf("source counts: %+v %v", items, err)
	}
	return items.Items[0]
}

func TestSourceTorrentStatsKeepFullProgressSeparateAcrossResumeAndPublication(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := model.Provider{ID: "native", Adapter: "http_json"}
	fields := `{"title":"Original film","info_hash":"0123456789012345678901234567890123456789"}`
	for _, source := range []string{"one", "two"} {
		publishSharingRow(t, ctx, db, provider.ID, source, fields, nil)
	}
	// Two distinct source IDs with the same hash must remain two torrents.
	if counts := sourceStats(t, ctx, db, provider.ID); counts.Imported != 2 || counts.Pending != 2 || counts.PendingImport != nil {
		t.Fatalf("hashes or observations replaced source identities: %+v", counts)
	}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	one := model.Record{SourceID: "one", Fields: map[string]any{"title": "Changed film"}}
	three := model.Record{SourceID: "three", Fields: map[string]any{"title": "New film"}}
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{one, three}}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertProgress := func(staged int64) {
		t.Helper()
		counts := sourceStats(t, ctx, db, provider.ID)
		if counts.Imported != 2 || counts.Pending != 2 || counts.PendingImport == nil || counts.PendingImport.RunID != run.ID || counts.PendingImport.Torrents != staged {
			t.Fatalf("incomplete Full changed published totals or lost progress: %+v", counts)
		}
		if counts.PendingImport.New != 1 || counts.PendingImport.Updates != 1 {
			t.Fatalf("pending imports confused new records with updates: %+v", counts.PendingImport)
		}
	}
	assertProgress(2)
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	assertProgress(2)
	// A later Preview is not a replacement for a resumable Full import.
	preview := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	preview, err = db.SavePage(ctx, preview, model.Page{Items: []model.Record{one}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, preview, model.StatusSucceeded)
	assertProgress(2)
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || resumed.ID != run.ID {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	// Observing the same identity again must not inflate staged progress.
	run, err = db.SavePage(ctx, *resumed, model.Page{Items: []model.Record{one}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertProgress(2)
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	if counts := sourceStats(t, ctx, db, provider.ID); counts.Imported != 2 || counts.Pending != 2 || counts.PendingImport != nil {
		t.Fatalf("completed replacement was added to the previous catalogue: %+v", counts)
	}
	rows := mirrorRows(t, ctx, db, provider.ID)
	if rows["one"].Fields["title"] != "Changed film" || rows["three"].SourceID != "three" || len(rows) != 2 {
		t.Fatalf("published counts do not describe the current catalogue: %+v", rows)
	}
}

func TestSourceTorrentStatsSupersedeRetainedFailedProgressAndExcludeRemoteDeletions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	p := mirrorSource("remote")
	a := mirrorItem("a", map[string]any{"title": "First"}, false)
	b := mirrorItem("b", map[string]any{"title": "Removed later"}, false)
	run := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	run = mirrorPage(t, ctx, db, run, "full", "first-page", false, a, b)
	mirrorFinish(t, ctx, db, run, model.StatusFailed)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 0 || counts.PendingImport == nil || counts.PendingImport.Torrents != 2 {
		t.Fatalf("failed remote snapshot lost retained progress: %+v", counts)
	}
	// A newer attempt supersedes old retained staging immediately, even before
	// its first page. A later completion must not revive the older failed count.
	newRun := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 0 || counts.PendingImport != nil {
		t.Fatalf("old failed progress leaked into a new attempt: %+v", counts)
	}
	newRun = mirrorPage(t, ctx, db, newRun, "full", "baseline", true, a, b)
	mirrorFinish(t, ctx, db, newRun, model.StatusSucceeded)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 2 || counts.Pending != 2 || counts.PendingImport != nil {
		t.Fatalf("old failed progress revived after completion: %+v", counts)
	}
	// Explicitly resuming an older retained attempt makes it the current import,
	// without adding its overlapping records to the published catalogue.
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || resumed.ID != run.ID {
		t.Fatalf("resume old import: %+v %v", resumed, err)
	}
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 2 || counts.PendingImport == nil || counts.PendingImport.RunID != run.ID || counts.PendingImport.Torrents != 2 {
		t.Fatalf("resumed import hidden by a newer completed attempt: %+v", counts)
	}
	mirrorFinish(t, ctx, db, *resumed, model.StatusCancelled)
	delta := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	deleted := mirrorItem("b", nil, true)
	changed := mirrorItem("a", map[string]any{"title": "Changed"}, false)
	delta = mirrorPage(t, ctx, db, delta, "incremental", "next-page", false, deleted, changed)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 2 || counts.PendingImport == nil || counts.PendingImport.RunID != delta.ID || counts.PendingImport.Torrents != 1 {
		t.Fatalf("remote deletion was counted as an incoming torrent: %+v", counts)
	}
	delta = mirrorPage(t, ctx, db, delta, "incremental", "new-baseline", true)
	mirrorFinish(t, ctx, db, delta, model.StatusSucceeded)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 1 || counts.Pending != 1 || counts.PendingImport != nil {
		t.Fatalf("completed remote deletion did not update source counts: %+v", counts)
	}
	cancelled := mirrorClaim(t, ctx, db, p, model.ModeFull)
	cancelled = mirrorPage(t, ctx, db, cancelled, "full", "held-page", false, b)
	mirrorFinish(t, ctx, db, cancelled, model.StatusCancelled)
	if counts := sourceStats(t, ctx, db, p.ID); counts.Imported != 1 || counts.PendingImport != nil {
		t.Fatalf("cancelled progress remained in current counts: %+v", counts)
	}
	if items, err := db.SourceTorrentStats(ctx, nil); err != nil || items.Items == nil || len(items.Items) != 0 {
		t.Fatalf("empty source list was not an empty response: %+v %v", items, err)
	}
}
