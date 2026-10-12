package store

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func mirrorSource(id string) model.Provider {
	return model.Provider{Version: 1, ID: id, Name: id, Adapter: "http_json", URL: "https://catalogue.example/api/catalogs/friend", HTTP: model.HTTPConfig{Catalog: true}}
}

func mirrorClaim(t *testing.T, ctx context.Context, db *Store, provider model.Provider, mode model.RunMode) model.Run {
	t.Helper()
	created, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.ClaimNext(ctx)
	if err != nil || run == nil || run.ID != created.ID {
		t.Fatalf("claim: %+v %v", run, err)
	}
	return *run
}

func mirrorItem(source string, fields map[string]any, deleted bool) model.Record {
	origin := model.CatalogOrigin{InstanceID: "owner-instance", ProviderID: "original-source", SourceID: source}
	item := model.CatalogItem{ID: model.CatalogItemID(origin), Origin: origin, Fields: fields, Deleted: deleted}
	raw, _ := json.Marshal(item)
	return model.Record{SourceID: item.ID, Origin: &origin, Fields: fields, Deleted: deleted, Raw: raw, ContentType: "application/json"}
}

func mirrorPage(t *testing.T, ctx context.Context, db *Store, run model.Run, mode, token string, done bool, items ...model.Record) model.Run {
	t.Helper()
	state := model.CatalogCursor{Version: 1, InstanceID: "owner-instance", Mode: mode, Done: done}
	var previous model.CatalogCursor
	if err := json.Unmarshal(run.Cursor, &previous); err != nil {
		t.Fatal(err)
	}
	state.BaseEndpoint, state.BaseCheckpoint = previous.BaseEndpoint, previous.BaseCheckpoint
	if done {
		state.Checkpoint = token
	} else {
		state.Cursor = token
	}
	next, _ := json.Marshal(state)
	page := model.Page{Body: []byte("original envelope"), ContentType: "application/json", Items: items, Next: next, Done: done, RequireUniqueIDs: true}
	updated, err := db.SavePage(ctx, run, page, "")
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func mirrorFinish(t *testing.T, ctx context.Context, db *Store, run model.Run, status model.RunStatus) model.Run {
	t.Helper()
	finished, err := db.FinishRun(ctx, run.ID, status, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return finished
}

func mirrorRows(t *testing.T, ctx context.Context, db *Store, id string) map[string]model.Torrent {
	t.Helper()
	rows, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: id})
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]model.Torrent, len(rows.Items))
	for _, row := range rows.Items {
		result[row.SourceID] = row
	}
	return result
}

func mirrorCheckpoint(t *testing.T, ctx context.Context, db *Store, p model.Provider, expected string) {
	t.Helper()
	checkpoint, err := db.RemoteCheckpoint(ctx, p.ID, p.URL)
	if err != nil || checkpoint != expected {
		t.Fatalf("published checkpoint = %q, want %q: %v", checkpoint, expected, err)
	}
}

func TestRemoteMirrorAtomicBaselineDeltaScopeResetAndIsolation(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	p := mirrorSource("remote-copy")
	a := mirrorItem("a", map[string]any{"title": "First", "seeders": json.Number("7")}, false)
	b := mirrorItem("b", map[string]any{"title": "Removed"}, false)
	// A native source deliberately uses the same identity as an imported row.
	local := model.Provider{ID: "local", Name: "Local"}
	native := mirrorClaim(t, ctx, db, local, model.ModeFull)
	var err error
	native, err = db.SavePage(ctx, native, model.Page{Items: []model.Record{{SourceID: a.SourceID, Fields: map[string]any{"title": "Local value"}, Raw: []byte("local raw")}}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, native, model.StatusSucceeded)

	run := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	run = mirrorPage(t, ctx, db, run, "full", "baseline", true, a, b)
	if len(mirrorRows(t, ctx, db, p.ID)) != 0 {
		t.Fatal("initial snapshot leaked before publication")
	}
	mirrorCheckpoint(t, ctx, db, p, "")
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	mirrorCheckpoint(t, ctx, db, p, "baseline")

	changed := mirrorItem("a", map[string]any{"title": "  Exact replacement  ", "size": json.Number("9007199254740993"), "category": nil}, false)
	added := mirrorItem("c", map[string]any{"title": "New"}, false)
	delta := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	var captured model.CatalogCursor
	if json.Unmarshal(delta.Cursor, &captured) != nil || captured.Checkpoint != "baseline" || delta.Mode != model.ModeIncremental {
		t.Fatalf("incremental attempt did not capture completed checkpoint: %+v", delta)
	}
	delta = mirrorPage(t, ctx, db, delta, "incremental", "next-delta-page", false, changed)
	paused := mirrorFinish(t, ctx, db, delta, model.StatusPaused)
	if row := mirrorRows(t, ctx, db, p.ID)[a.SourceID]; row.Fields["title"] != "First" {
		t.Fatal("paused delta changed the published copy")
	}
	mirrorCheckpoint(t, ctx, db, p, "baseline")
	if _, err := db.ResumeRun(ctx, paused.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || !bytes.Equal(resumed.Cursor, paused.Cursor) {
		t.Fatalf("resume replaced its immutable cursor: %+v %v", resumed, err)
	}
	delta = mirrorPage(t, ctx, db, *resumed, "incremental", "after-delta", true, mirrorItem("b", nil, true), added)
	mirrorFinish(t, ctx, db, delta, model.StatusSucceeded)
	rows := mirrorRows(t, ctx, db, p.ID)
	if len(rows) != 2 || !reflect.DeepEqual(rows[a.SourceID].Fields, changed.Fields) || rows[a.SourceID].Origin == nil || *rows[a.SourceID].Origin != *a.Origin || rows[b.SourceID].SourceID != "" || rows[added.SourceID].SourceID == "" {
		t.Fatalf("delta did not exact-replace/delete/preserve provenance: %+v", rows)
	}
	if row := mirrorRows(t, ctx, db, local.ID)[a.SourceID]; row.Fields["title"] != "Local value" || row.Origin != nil {
		t.Fatal("remote publication changed the local source namespace")
	}
	mirrorCheckpoint(t, ctx, db, p, "after-delta")
	// A valid older-scope checkpoint can return a full empty authoritative copy.
	reset := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	reset = mirrorPage(t, ctx, db, reset, "full", "narrowed-scope", true)
	mirrorFinish(t, ctx, db, reset, model.StatusSucceeded)
	if len(mirrorRows(t, ctx, db, p.ID)) != 0 || len(mirrorRows(t, ctx, db, local.ID)) != 1 {
		t.Fatal("scope reset did not remove only remote-owned records")
	}
	mirrorCheckpoint(t, ctx, db, p, "narrowed-scope")
}

func TestRemoteMirrorFailureCancellationAndCheckpointRollback(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	p := mirrorSource("remote-copy")
	original := mirrorItem("a", map[string]any{"title": "Original"}, false)
	baseline := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	baseline = mirrorPage(t, ctx, db, baseline, "full", "baseline", true, original)
	mirrorFinish(t, ctx, db, baseline, model.StatusSucceeded)
	changed := mirrorItem("a", map[string]any{"title": "Unpublished"}, false)
	failed := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	failed = mirrorPage(t, ctx, db, failed, "incremental", "unfinished", false, changed)
	mirrorFinish(t, ctx, db, failed, model.StatusFailed)
	mirrorCheckpoint(t, ctx, db, p, "baseline")
	cancelled := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	cancelled = mirrorPage(t, ctx, db, cancelled, "incremental", "cancelled", true, changed)
	if _, err := db.RequestCancel(ctx, cancelled.ID); err != nil {
		t.Fatal(err)
	}
	if finished := mirrorFinish(t, ctx, db, cancelled, model.StatusSucceeded); finished.Status != model.StatusCancelled {
		t.Fatal("late cancellation did not take precedence over publication")
	}
	mirrorCheckpoint(t, ctx, db, p, "baseline")
	// Force the checkpoint write to fail after canonical SQL has executed. Both
	// the copy and checkpoint must roll back in the same database transaction.
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_fixture_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture checkpoint failure'; END; $$;
CREATE TRIGGER reject_fixture_checkpoint BEFORE UPDATE ON ingest.remote_checkpoints FOR EACH ROW EXECUTE FUNCTION ingest.reject_fixture_checkpoint()`); err != nil {
		t.Fatal(err)
	}
	rollback := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	rollback = mirrorPage(t, ctx, db, rollback, "incremental", "rollback", true, changed)
	if _, err := db.FinishRun(ctx, rollback.ID, model.StatusSucceeded, "", ""); err == nil {
		t.Fatal("checkpoint fixture did not fail publication")
	}
	mirrorCheckpoint(t, ctx, db, p, "baseline")
	if row := mirrorRows(t, ctx, db, p.ID)[original.SourceID]; !reflect.DeepEqual(row.Fields, original.Fields) {
		t.Fatalf("failed/cancelled/rolled-back publication changed existing values: %+v", row)
	}
	raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || raw.Total != 4 {
		t.Fatalf("unpublished records were not archived: %+v %v", raw, err)
	}
}

func TestRemoteMirrorPreviewFullAndChangedEndpointStartSnapshots(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	p := mirrorSource("remote-copy")
	baseline := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	baseline = mirrorPage(t, ctx, db, baseline, "full", "baseline", true, mirrorItem("a", map[string]any{"title": "Published"}, false))
	mirrorFinish(t, ctx, db, baseline, model.StatusSucceeded)
	preview := mirrorClaim(t, ctx, db, p, model.ModePreview)
	var state model.CatalogCursor
	if json.Unmarshal(preview.Cursor, &state) != nil || state.Checkpoint != "" {
		t.Fatal("preview inherited the incremental checkpoint")
	}
	preview = mirrorPage(t, ctx, db, preview, "full", "preview-only", true, mirrorItem("b", map[string]any{"title": "Preview"}, false))
	mirrorFinish(t, ctx, db, preview, model.StatusSucceeded)
	mirrorCheckpoint(t, ctx, db, p, "baseline")
	if len(mirrorRows(t, ctx, db, p.ID)) != 1 {
		t.Fatal("preview published records")
	}
	full := mirrorClaim(t, ctx, db, p, model.ModeFull)
	if json.Unmarshal(full.Cursor, &state) != nil || state.Checkpoint != "" {
		t.Fatal("explicit full resync inherited the incremental checkpoint")
	}
	mirrorFinish(t, ctx, db, full, model.StatusCancelled)
	p.URL = "https://another.example/api/catalogs/friend"
	changed := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	if json.Unmarshal(changed.Cursor, &state) != nil || state.Checkpoint != "" {
		t.Fatal("changed endpoint inherited another catalogue's checkpoint")
	}
	mirrorFinish(t, ctx, db, changed, model.StatusCancelled)
}

func TestRemoteMirrorRefusesStaleResumedPublication(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	p := mirrorSource("remote-copy")
	original := mirrorItem("a", map[string]any{"title": "Baseline"}, false)
	first := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	first = mirrorPage(t, ctx, db, first, "full", "baseline", true, original)
	mirrorFinish(t, ctx, db, first, model.StatusSucceeded)
	old := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	old = mirrorPage(t, ctx, db, old, "incremental", "old-page", false, mirrorItem("a", map[string]any{"title": "Stale"}, false))
	mirrorFinish(t, ctx, db, old, model.StatusPaused)

	newer := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	newer = mirrorPage(t, ctx, db, newer, "incremental", "newer-checkpoint", true, mirrorItem("a", map[string]any{"title": "Newer"}, false))
	mirrorFinish(t, ctx, db, newer, model.StatusSucceeded)
	if _, err := db.ResumeRun(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil || !bytes.Equal(resumed.Cursor, old.Cursor) {
		t.Fatalf("resume discarded immutable state: %+v %v", resumed, err)
	}
	old = mirrorPage(t, ctx, db, *resumed, "incremental", "stale-checkpoint", true)
	if finished := mirrorFinish(t, ctx, db, old, model.StatusSucceeded); finished.Status != model.StatusFailed {
		t.Fatal("stale run was allowed to overwrite a newer committed mirror")
	}
	mirrorCheckpoint(t, ctx, db, p, "newer-checkpoint")
	if row := mirrorRows(t, ctx, db, p.ID)[original.SourceID]; row.Fields["title"] != "Newer" {
		t.Fatalf("stale resume changed a newer record: %+v", row)
	}
}

func TestRemoteMirrorUnrepresentablePageRetainsRawAndRetriesAtomically(t *testing.T) {
	ctx, _, db := scheduleDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	p := mirrorSource("remote-copy")
	run := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
	initial := append([]byte(nil), run.Cursor...)
	good := mirrorItem("a", map[string]any{"title": "Valid"}, false)
	invalid := mirrorItem("b", map[string]any{"title": "invalid\u0000value"}, false)
	run = mirrorPage(t, ctx, db, run, "full", "not-published", true, good, invalid)
	if run.TraversalDone || run.Pages != 0 || run.Errors != 1 || !bytes.Equal(run.Cursor, initial) {
		t.Fatalf("partial database interpretation certified a snapshot: %+v", run)
	}
	mirrorFinish(t, ctx, db, run, model.StatusFailed)
	mirrorCheckpoint(t, ctx, db, p, "")
	raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: p.ID})
	if err != nil || raw.Total != 2 {
		t.Fatalf("rejected page lost original records: %+v %v", raw, err)
	}
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.ClaimNext(ctx)
	if err != nil || resumed == nil {
		t.Fatalf("resume rejected retryable source response: %+v %v", resumed, err)
	}
	valid := mirrorItem("b", map[string]any{"title": "Corrected upstream"}, false)
	run = mirrorPage(t, ctx, db, *resumed, "full", "corrected", true, good, valid)
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	mirrorCheckpoint(t, ctx, db, p, "corrected")
	if rows := mirrorRows(t, ctx, db, p.ID); !reflect.DeepEqual(rows[good.SourceID].Fields, good.Fields) || !reflect.DeepEqual(rows[valid.SourceID].Fields, valid.Fields) {
		t.Fatalf("retry retained partial staging or lost valid records: %+v", rows)
	}
}

func TestRemoteMirrorCannotReplaceRetainedNativeNamespace(t *testing.T) {
	for _, empty := range []bool{true, false} {
		name := "colliding snapshot"
		if empty {
			name = "empty snapshot"
		}
		t.Run(name, func(t *testing.T) {
			ctx, _, db := scheduleDatabase(t)
			p := mirrorSource("previous-native")
			collision := mirrorItem("native-id", map[string]any{"title": "Remote replacement"}, false)
			publishSharingRow(t, ctx, db, p.ID, collision.SourceID, `{"title":"Retained local value"}`, nil)
			before := mirrorRows(t, ctx, db, p.ID)
			run := mirrorClaim(t, ctx, db, p, model.ModeIncremental)
			if empty {
				run = mirrorPage(t, ctx, db, run, "full", "must-not-commit", true)
			} else {
				run = mirrorPage(t, ctx, db, run, "full", "must-not-commit", true, collision)
			}
			if finished := mirrorFinish(t, ctx, db, run, model.StatusSucceeded); finished.Status != model.StatusFailed {
				t.Fatal("remote snapshot was allowed to own a retained native namespace")
			}
			if after := mirrorRows(t, ctx, db, p.ID); !reflect.DeepEqual(before, after) {
				t.Fatalf("remote import modified retained local values: %+v", after)
			}
			mirrorCheckpoint(t, ctx, db, p, "")
		})
	}
}
