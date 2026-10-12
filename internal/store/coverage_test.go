package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func coverageRecord(id string, scopes ...string) model.Record {
	return model.Record{
		SourceID: id, CoverageScopes: scopes,
		Fields: map[string]any{"title": id, "info_hash": "0123456789012345678901234567890123456789"},
		Raw:    []byte(`{"original":true}`),
	}
}

func assertCoverage(t *testing.T, ctx context.Context, db *Store, runID string, want map[string]int64, requested []string, wantIDs map[string]bool) {
	t.Helper()
	counts, err := db.ScopeCounts(ctx, runID)
	if err != nil || !reflect.DeepEqual(counts, want) {
		t.Fatalf("coverage counts = %v, %v; want %v", counts, err, want)
	}
	observed, err := db.ObservedIDs(ctx, runID, requested)
	if err != nil || !reflect.DeepEqual(observed, wantIDs) {
		t.Fatalf("observed IDs = %v, %v; want %v", observed, err, wantIDs)
	}
}

func TestScopeCoverageSurvivesResumeWithoutPublishingPreview(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	sharingGrant(t, ctx, db)
	provider := model.Provider{Version: 1, ID: "coverage-preview", Name: "Coverage", Adapter: "http_json"}
	// A preexisting live identity is not evidence for a new collection.
	publishSharingRow(t, ctx, db, provider.ID, "prior", `{"title":"Prior"}`, nil)
	run := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	run, err := db.SavePage(ctx, run, model.Page{
		Items: []model.Record{coverageRecord("1", "root", "root", "overlap"), coverageRecord("2", "root"), coverageRecord("1", "root")},
		Next:  json.RawMessage(`{"page":2}`),
	}, "first")
	if err != nil {
		t.Fatal(err)
	}
	if run.DistinctRecords != 2 || run.Records != 3 {
		t.Fatalf("repeated IDs or shared hashes changed the distinct record count: %+v", run)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 2}, []string{"1", "2", "prior"}, map[string]bool{"1": true, "2": true})
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	assertCoverage(t, ctx, reopened, run.ID, map[string]int64{"root": 2}, []string{"2", "prior"}, map[string]bool{"2": true})
	if _, err := reopened.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := reopened.ClaimNext(ctx)
	if err != nil || resumed == nil || !bytes.Equal(resumed.Cursor, run.Cursor) {
		t.Fatalf("resumed checkpoint: %+v %v", resumed, err)
	}
	run, err = reopened.SavePage(ctx, *resumed, model.Page{
		Items: []model.Record{coverageRecord("1", "root", "overlap"), coverageRecord("2", "overlap")},
		Next:  json.RawMessage(`{"page":3}`), Done: true,
	}, "second")
	if err != nil {
		t.Fatal(err)
	}
	if run.DistinctRecords != 2 || run.Records != 5 {
		t.Fatalf("resume recounted existing source identities: %+v", run)
	}
	if _, err := reopened.FinishRun(ctx, run.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, reopened, run.ID, map[string]int64{"root": 1, "overlap": 2}, []string{"1", "2", "prior"}, map[string]bool{"1": true, "2": true})
	rows, err := reopened.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || rows.Total != 1 || len(rows.Items) != 1 || rows.Items[0].SourceID != "prior" {
		t.Fatalf("preview published rows: %+v %v", rows, err)
	}
	var journalCount int
	if err := reopened.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.catalog_journal WHERE provider_id=$1", provider.ID).Scan(&journalCount); err != nil || journalCount != 1 {
		t.Fatalf("preview changed publication history: %d %v", journalCount, err)
	}
	next := mirrorClaim(t, ctx, reopened, provider, model.ModePreview)
	if next.DistinctRecords != 0 {
		t.Fatalf("a new run inherited earlier source identities: %+v", next)
	}
	assertCoverage(t, ctx, reopened, next.ID, map[string]int64{}, []string{"1", "2", "prior"}, map[string]bool{})
}

func TestScopeCoverageExcludesInvalidInterpretations(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "coverage-invalid", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	failed := coverageRecord("failed", "root")
	failed.Error = "Invalid record"
	ignored := coverageRecord("ignored", "root")
	ignored.Ignored = true
	auxiliary := coverageRecord("auxiliary", "root")
	auxiliary.Auxiliary = true
	unencodable := coverageRecord("unencodable", "root")
	unencodable.Fields["unsupported"] = make(chan int)
	unrepresentable := coverageRecord("unrepresentable", "root")
	unrepresentable.Fields["title"] = "null\x00character"
	overflow := coverageRecord("overflow", "root")
	overflow.Fields["number"] = json.Number("1e1000000")
	items := []model.Record{
		coverageRecord("valid", "root"), failed, ignored, auxiliary, unencodable, unrepresentable, overflow,
		coverageRecord("", "root"), coverageRecord("bad\x00identity", "root"), coverageRecord("unscoped"),
	}
	run, err := db.SavePage(ctx, run, model.Page{Items: items, Next: json.RawMessage(`{"page":2}`)}, "invalid-records")
	if err != nil {
		t.Fatal(err)
	}
	if run.DistinctRecords != 2 {
		t.Fatalf("distinct records must include unscoped valid IDs but exclude invalid, ignored and auxiliary records: %+v", run)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 1}, []string{"valid", "failed", "ignored", "auxiliary", "unencodable", "unrepresentable", "overflow", "unscoped"}, map[string]bool{"valid": true})
	raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || raw.Total != int64(len(items)) {
		t.Fatalf("interpretation failures lost raw evidence: %+v %v", raw, err)
	}
}

func TestScopeCoverageRejectedPagesLeaveProgressUnchanged(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "coverage-rejection", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("1", "root")}, Next: json.RawMessage(`{"page":2}`)}, "accepted")
	if err != nil {
		t.Fatal(err)
	}
	cursor := append([]byte(nil), run.Cursor...)
	attempts := []struct {
		name        string
		page        model.Page
		fingerprint string
		stalled     bool
	}{
		{"failed response", model.Page{Error: "Response failed", ResetStaging: true, Items: []model.Record{coverageRecord("2", "root")}}, "failure", false},
		{"duplicate fingerprint", model.Page{ResetStaging: true, Items: []model.Record{coverageRecord("2", "root")}}, "accepted", true},
		{"duplicate identity", model.Page{RequireUniqueIDs: true, Items: []model.Record{coverageRecord("1", "root"), coverageRecord("2", "root")}}, "duplicate", true},
	}
	for _, attempt := range attempts {
		t.Run(attempt.name, func(t *testing.T) {
			attempt.page.Next = json.RawMessage(`{"page":99}`)
			attempt.page.Done = true
			updated, err := db.SavePage(ctx, run, attempt.page, attempt.fingerprint)
			if (attempt.stalled && !errors.Is(err, model.ErrStalled)) || (!attempt.stalled && err != nil) {
				t.Fatalf("rejected page error = %v", err)
			}
			run = updated
			if run.Pages != 1 || run.TraversalDone || !bytes.Equal(run.Cursor, cursor) {
				t.Fatalf("rejected page advanced checkpoint: %+v", run)
			}
			assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 1}, []string{"1", "2"}, map[string]bool{"1": true})
		})
	}
}

func TestScopeCoverageResetsOnlyDerivedGeneration(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	sharingGrant(t, ctx, db)
	provider := model.Provider{Version: 1, ID: "coverage-reset", Name: "Coverage", Adapter: "http_json"}
	publishSharingRow(t, ctx, db, provider.ID, "live", `{"title":"Published"}`, nil)
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("old", "root", "old-scope")}, Next: json.RawMessage(`{"generation":1}`)}, "old")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{ResetStaging: true, Items: []model.Record{coverageRecord("new", "root")}, Next: json.RawMessage(`{"generation":2}`)}, "new")
	if err != nil {
		t.Fatal(err)
	}
	if run.DistinctRecords != 1 || run.Records != 2 {
		t.Fatalf("reset must replace derived identities without erasing observations: %+v", run)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 1}, []string{"old", "new"}, map[string]bool{"new": true})
	raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || raw.Total != 2 {
		t.Fatalf("generation reset lost original history: %+v %v", raw, err)
	}
	var journalCount int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.catalog_journal WHERE provider_id=$1", provider.ID).Scan(&journalCount); err != nil || journalCount != 1 {
		t.Fatalf("generation reset changed publication history: %d %v", journalCount, err)
	}
	var staged string
	if err := db.pool.QueryRow(ctx, "SELECT source_id FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || staged != "new" {
		t.Fatalf("generation staging = %q %v", staged, err)
	}
}

func TestScopeCoverageRollsBackWithCheckpointFailure(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := model.Provider{Version: 1, ID: "coverage-rollback", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("old", "root")}, Next: json.RawMessage(`{"page":2}`)}, "old")
	if err != nil {
		t.Fatal(err)
	}
	// Fail after membership insertion, including after the old generation was
	// cleared, to prove that neither counts nor observations outrun the cursor.
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_coverage_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'checkpoint unavailable'; END;
$$;
CREATE TRIGGER reject_coverage_checkpoint BEFORE UPDATE ON ingest.runs
FOR EACH ROW EXECUTE FUNCTION ingest.reject_coverage_checkpoint()`); err != nil {
		t.Fatal(err)
	}
	page := model.Page{ResetStaging: true, Items: []model.Record{coverageRecord("new", "root")}, Next: json.RawMessage(`{"page":3}`)}
	if _, err := db.SavePage(ctx, run, page, "new"); err == nil {
		t.Fatal("expected rejected checkpoint transaction")
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 1}, []string{"old", "new"}, map[string]bool{"old": true})
	persisted, err := db.GetRun(ctx, run.ID)
	if err != nil || persisted.Pages != run.Pages || persisted.Records != run.Records || persisted.DistinctRecords != run.DistinctRecords || !bytes.Equal(persisted.Cursor, run.Cursor) {
		t.Fatalf("failed transaction changed checkpoint: %+v %v", persisted, err)
	}
	raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || raw.Total != 1 {
		t.Fatalf("failed transaction partially retained raw page: %+v %v", raw, err)
	}
	if _, err := db.pool.Exec(ctx, "DROP TRIGGER reject_coverage_checkpoint ON ingest.runs; DROP FUNCTION ingest.reject_coverage_checkpoint()"); err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, page, "new")
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 1}, []string{"old", "new"}, map[string]bool{"new": true})
}

func TestScopeRefreshResumesAtomicallyWithoutPublishingOrLosingOtherScopes(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := model.Provider{Version: 1, ID: "scope-refresh", Name: "Coverage", Adapter: "http_json"}
	publishSharingRow(t, ctx, db, provider.ID, "published", `{"title":"Published"}`, nil)
	publishSharingRow(t, ctx, db, "other-provider", "1", `{"title":"Independent","info_hash":"0123456789012345678901234567890123456789"}`, nil)
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{
		Items: []model.Record{coverageRecord("1", "a", "b"), coverageRecord("2", "a"), coverageRecord("3", "c"), coverageRecord("4", "a")},
		Next:  json.RawMessage(`{"phase":"reconcile"}`),
	}, "initial")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{
		Items: []model.Record{coverageRecord("1", "a"), coverageRecord("1", "b", "c")},
		Next:  json.RawMessage(`{"phase":"check"}`),
	}, "moved")
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 2, "b": 1, "c": 2}, []string{"1", "2", "3", "4"}, map[string]bool{"1": true, "2": true, "3": true, "4": true})
	checkpoint := append(json.RawMessage(nil), run.Cursor...)
	run, err = db.SavePage(ctx, run, model.Page{
		RefreshScopes: []string{"a", "b"}, Error: "Response failed",
		Items: []model.Record{coverageRecord("1", "a")}, Next: json.RawMessage(`{"phase":"incorrect"}`),
	}, "rejected-refresh")
	if err != nil || !bytes.Equal(run.Cursor, checkpoint) {
		t.Fatalf("errored refresh advanced state: %+v %v", run, err)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 2, "b": 1, "c": 2}, []string{"1", "2"}, map[string]bool{"1": true, "2": true})
	run, err = db.SavePage(ctx, run, model.Page{RefreshScopes: []string{"a", "b"}, Next: json.RawMessage(`{"phase":"root","round":1}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if run.DistinctRecords != 2 {
		t.Fatalf("refresh retained orphaned derived identities: %+v", run)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"c": 2}, []string{"1", "2", "3", "4"}, map[string]bool{"3": true})
	var retainedScopes []string
	var applied bool
	if err := db.pool.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(metadata->'coverage_refresh_scopes')),
(metadata->>'coverage_refresh_applied')::boolean FROM ingest.pages WHERE run_id=$1 ORDER BY id DESC LIMIT 1`, run.ID).Scan(&retainedScopes, &applied); err != nil || !reflect.DeepEqual(retainedScopes, []string{"a", "b"}) || !applied {
		t.Fatalf("refresh provenance was not retained: %v %t %v", retainedScopes, applied, err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	assertCoverage(t, ctx, reopened, run.ID, map[string]int64{"c": 2}, []string{"1", "2", "3", "4"}, map[string]bool{"3": true})
	published, err := reopened.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 1 || published.Items[0].SourceID != "published" {
		t.Fatalf("interrupted refresh changed publication: %+v %v", published, err)
	}
	if _, err := reopened.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := reopened.ClaimNext(ctx)
	if err != nil || claimed == nil || !bytes.Equal(claimed.Cursor, run.Cursor) {
		t.Fatalf("refresh cursor was not durable: %+v %v", claimed, err)
	}
	failed, ignored, auxiliary := coverageRecord("1", "a"), coverageRecord("1", "a"), coverageRecord("1", "a")
	failed.Error, ignored.Ignored, auxiliary.Auxiliary = "Invalid interpretation", true, true
	run, err = reopened.SavePage(ctx, *claimed, model.Page{
		Items: []model.Record{failed, ignored, auxiliary}, Next: json.RawMessage(`{"phase":"root","round":1,"page":2}`),
	}, "invalid")
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, reopened, run.ID, map[string]int64{"c": 2}, []string{"1", "3"}, map[string]bool{"3": true})
	run, err = reopened.SavePage(ctx, run, model.Page{
		Items: []model.Record{coverageRecord("1", "b", "c"), coverageRecord("4", "a"), coverageRecord("5", "a")},
		Next:  json.RawMessage(`{"phase":"done"}`), Done: true,
	}, "refreshed")
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, reopened, run.ID, map[string]int64{"a": 2, "b": 1, "c": 2}, []string{"1", "2", "3", "4", "5"}, map[string]bool{"1": true, "3": true, "4": true, "5": true})
	if _, err := reopened.FinishRun(ctx, run.ID, model.StatusSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	published, err = reopened.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || published.Total != 4 {
		t.Fatalf("fresh successful traversal did not publish all distinct IDs: %+v %v", published, err)
	}
	for _, item := range published.Items {
		if item.SourceID == "2" || item.SourceID == "published" {
			t.Fatalf("retired identity survived full replacement: %+v", item)
		}
	}
	other, err := reopened.ListTorrents(ctx, model.ListOptions{ProviderID: "other-provider"})
	if err != nil || other.Total != 1 || other.Items[0].SourceID != "1" {
		t.Fatalf("same-hash other-provider occurrence changed: %+v %v", other, err)
	}
	raw, err := reopened.ListRaw(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil || raw.Total != 13 {
		t.Fatalf("scope refresh lost original observations: %+v %v", raw, err)
	}
}

func TestScopeRefreshRollbackKeepsCoverageStagingAndCursorTogether(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "scope-refresh-rollback", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("1", "a"), coverageRecord("2", "b")}, Next: json.RawMessage(`{"round":0}`)}, "original")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_scope_refresh() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'checkpoint unavailable'; END;
$$;
CREATE TRIGGER reject_scope_refresh BEFORE UPDATE ON ingest.runs
FOR EACH ROW EXECUTE FUNCTION ingest.reject_scope_refresh()`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePage(ctx, run, model.Page{RefreshScopes: []string{"a"}, Next: json.RawMessage(`{"round":1}`)}, ""); err == nil {
		t.Fatal("checkpoint rejection did not abort refresh")
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 1, "b": 1}, []string{"1", "2"}, map[string]bool{"1": true, "2": true})
	var staged, candidates int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || staged != 2 {
		t.Fatalf("rolled-back refresh deleted staging: %d %v", staged, err)
	}
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.run_scope_refresh_candidates WHERE run_id=$1", run.ID).Scan(&candidates); err != nil || candidates != 0 {
		t.Fatalf("rolled-back refresh retained candidates: %d %v", candidates, err)
	}
	persisted, err := db.GetRun(ctx, run.ID)
	if err != nil || !bytes.Equal(persisted.Cursor, run.Cursor) || persisted.DistinctRecords != 2 {
		t.Fatalf("failed refresh changed checkpoint: %+v %v", persisted, err)
	}
}

func TestLatestCoverageMigrationBackfillsOnlyAcceptedCurrentObservations(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	// This scenario verifies the optional complete processing history.
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := model.Provider{
		Version: 1, ID: "coverage-backfill", Name: "Coverage", Adapter: "http_json",
		Traversal: &model.JSONTraversal{Scopes: []model.JSONScope{
			{ID: "a", Match: map[string]any{"category_id": 1}},
			{ID: "b", Match: map[string]any{"category_id": 2}},
			{ID: "common", Match: map[string]any{"group_id": 1}},
		}},
	}
	var legacyFields []map[string]any
	record := func(id string, category int, scopes ...string) model.Record {
		item := coverageRecord(id, scopes...)
		item.Fields["category_id"], item.Fields["group_id"] = category, 1
		legacyFields = append(legacyFields, item.Fields)
		return item
	}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{
		Items: []model.Record{record("retired-generation", 1, "a", "common")}, Next: json.RawMessage(`{"generation":0}`),
	}, "old-generation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{
		ResetStaging: true, Items: []model.Record{record("1", 1, "a", "common"), record("2", 1, "a", "common"), record("outside", 1, "a", "common")}, Next: json.RawMessage(`{"generation":1}`),
	}, "current-generation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{record("1", 2, "b", "common")}, Next: json.RawMessage(`{"generation":1,"page":2}`)}, "moved")
	if err != nil {
		t.Fatal(err)
	}
	invalid, ignored, auxiliary := record("1", 1, "a"), record("1", 1, "a"), record("1", 1, "a")
	invalid.Error, ignored.Ignored, auxiliary.Auxiliary = "Invalid record", true, true
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{invalid, ignored, auxiliary}, Next: json.RawMessage(`{"generation":1,"page":3}`)}, "invalid")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{Error: "Rejected response", Items: []model.Record{record("1", 1, "a")}}, "rejected")
	if err != nil {
		t.Fatal(err)
	}
	// Older bounded adapters archived valid out-of-scope observations without
	// emitting explicit empty coverage, leaving their previous staging intact.
	outside := record("outside", 99)
	outside.Fields["group_id"], outside.Ignored = 2, true
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{outside}, Next: json.RawMessage(`{"generation":1,"page":4}`)}, "legacy-outside")
	if err != nil {
		t.Fatal(err)
	}
	// This fixture represents a pre-migration archive, when accepted and
	// rejected observations still carried their original interpreted fields.
	archivedFields, err := json.Marshal(legacyFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `WITH observed AS (
SELECT id,row_number() OVER (ORDER BY id) AS ordinal FROM ingest.raw_records WHERE run_id=$1)
UPDATE ingest.raw_records AS raw SET fields=$2::jsonb->(observed.ordinal::integer-1)
FROM observed WHERE raw.id=observed.id`, run.ID, archivedFields); err != nil {
		t.Fatal(err)
	}
	// Restore the old union-of-ever-seen bug, then apply the new migration to
	// prove that only derived state changes and its evidence filter is sound.
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.run_scope_memberships(run_id,scope_id,source_id) VALUES($1,'a','1');
`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.run_scope_counts SET observed_count=3 WHERE run_id=$1 AND scope_id='a'", run.ID); err != nil {
		t.Fatal(err)
	}
	var beforeRaw, beforePages string
	if err := db.pool.QueryRow(ctx, `SELECT md5(string_agg(encode(raw,'hex'),'|' ORDER BY id)) FROM ingest.raw_records WHERE run_id=$1`, run.ID).Scan(&beforeRaw); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT md5(string_agg(encode(body,'hex'),'|' ORDER BY id)) FROM ingest.pages WHERE run_id=$1`, run.ID).Scan(&beforePages); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "DROP TABLE ingest.run_scope_refresh_candidates"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, coverageLatestSchema); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 1, "b": 1, "common": 2}, []string{"1", "2", "retired-generation"}, map[string]bool{"1": true, "2": true})
	var afterRaw, afterPages string
	if err := db.pool.QueryRow(ctx, `SELECT md5(string_agg(encode(raw,'hex'),'|' ORDER BY id)) FROM ingest.raw_records WHERE run_id=$1`, run.ID).Scan(&afterRaw); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT md5(string_agg(encode(body,'hex'),'|' ORDER BY id)) FROM ingest.pages WHERE run_id=$1`, run.ID).Scan(&afterPages); err != nil {
		t.Fatal(err)
	}
	if beforeRaw != afterRaw || beforePages != afterPages {
		t.Fatal("coverage backfill rewrote archived bytes")
	}
	var staged int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || staged != 2 {
		t.Fatalf("backfill changed staged independent same-hash identities: %d %v", staged, err)
	}
	persisted, err := db.GetRun(ctx, run.ID)
	if err != nil || persisted.DistinctRecords != 2 {
		t.Fatalf("backfill retained an identity known to be outside selected scopes: %+v %v", persisted, err)
	}
}

func TestScopeMembershipMovesOutAndBackWithoutChangingOtherOccurrences(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModeFull, model.ModeIncremental} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, _, db := activityDatabase(t)
			// This scenario verifies the optional complete processing history.
			setStorageMode(t, ctx, db, model.StorageFull)
			provider := model.Provider{Version: 1, ID: "scope-move-out", Name: "Coverage", Adapter: "http_json"}
			run := mirrorClaim(t, ctx, db, provider, mode)
			run, err := db.SavePage(ctx, run, model.Page{
				Items: []model.Record{coverageRecord("1", "a", "b"), coverageRecord("2", "a")},
				Next:  json.RawMessage(`{"step":1}`),
			}, "inside")
			if err != nil {
				t.Fatal(err)
			}
			outside := coverageRecord("1")
			outside.Ignored, outside.CoverageScopes = true, []string{}
			unspecified, invalid, auxiliary := coverageRecord("1"), outside, outside
			unspecified.Ignored, invalid.Error, auxiliary.Auxiliary = true, "Invalid interpretation", true
			run, err = db.SavePage(ctx, run, model.Page{
				Items: []model.Record{unspecified, invalid, auxiliary}, Next: json.RawMessage(`{"step":2}`),
			}, "non-authoritative")
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := append(json.RawMessage(nil), run.Cursor...)
			run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{outside}, Error: "Rejected response", Next: json.RawMessage(`{"step":99}`)}, "rejected-negative")
			if err != nil || !bytes.Equal(run.Cursor, checkpoint) {
				t.Fatalf("rejected negative evidence changed cursor: %+v %v", run, err)
			}
			assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 2, "b": 1}, []string{"1", "2"}, map[string]bool{"1": true, "2": true})
			run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{outside}, Next: json.RawMessage(`{"step":3}`)}, "outside")
			if err != nil || run.DistinctRecords != 1 {
				t.Fatalf("valid negative evidence did not retire derived identity: %+v %v", run, err)
			}
			assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 1}, []string{"1", "2"}, map[string]bool{"2": true})
			if mode == model.ModeFull {
				var staged []string
				if err := db.pool.QueryRow(ctx, "SELECT array_agg(source_id ORDER BY source_id) FROM ingest.staged_torrents WHERE run_id=$1", run.ID).Scan(&staged); err != nil || !reflect.DeepEqual(staged, []string{"2"}) {
					t.Fatalf("move-out left stale full staging or removed same-hash peer: %v %v", staged, err)
				}
			} else {
				live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
				if err != nil || live.Total != 2 {
					t.Fatalf("move-out changed native incremental canonical rows: %+v %v", live, err)
				}
			}
			// Last valid observation wins even if an ID leaves and returns
			// several times within one retained response.
			run, err = db.SavePage(ctx, run, model.Page{
				Items: []model.Record{coverageRecord("1", "a"), outside, coverageRecord("1", "b")},
				Next:  json.RawMessage(`{"step":4}`),
			}, "back-inside")
			if err != nil || run.DistinctRecords != 2 {
				t.Fatalf("move-back failed to restore only the latest membership: %+v %v", run, err)
			}
			assertCoverage(t, ctx, db, run.ID, map[string]int64{"a": 1, "b": 1}, []string{"1", "2"}, map[string]bool{"1": true, "2": true})
			raw, err := db.ListRaw(ctx, model.ListOptions{ProviderID: provider.ID})
			if err != nil || raw.Total != 10 {
				t.Fatalf("move-out/back lost raw evidence: %+v %v", raw, err)
			}
		})
	}
}

func TestRefreshCandidatesUseCanonicalNumericOrderAndCommittedEvidence(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "refresh-candidates", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	var records []model.Record
	for _, id := range []string{"2", "10", "20", "9223372036854775807", "9223372036854775808", "02", "0", "-1", "other"} {
		records = append(records, coverageRecord(id, "changed"))
	}
	records = append(records, coverageRecord("1", "stable"))
	run, err := db.SavePage(ctx, run, model.Page{Items: records, Next: json.RawMessage(`{"page":2}`)}, "initial")
	if err != nil {
		t.Fatal(err)
	}
	run, err = db.SavePage(ctx, run, model.Page{RefreshScopes: []string{"changed"}, Next: json.RawMessage(`{"page":3}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	reopened, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	const maximum = int64(9223372036854775807)
	var got []int64
	for after := int64(0); ; {
		id, err := reopened.NextRefreshID(ctx, run.ID, after, maximum)
		if err != nil {
			t.Fatal(err)
		}
		if id == 0 {
			break
		}
		got = append(got, id)
		if len(got) > 4 {
			t.Fatalf("refresh cursor repeated or included ineligible IDs: %v", got)
		}
		after = id
	}
	if !reflect.DeepEqual(got, []int64{2, 10, 20, maximum}) {
		t.Fatalf("refresh IDs lost numeric ordering or canonical identity: %v", got)
	}
	if id, err := reopened.NextRefreshID(ctx, run.ID, 2, 9); err != nil || id != 0 {
		t.Fatalf("candidate escaped the scanned prefix: %d %v", id, err)
	}
	if id, err := reopened.NextRefreshID(ctx, "another-run", 0, maximum); err != nil || id != 0 {
		t.Fatalf("another run inherited refresh candidates: %d %v", id, err)
	}
	if _, err := reopened.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := reopened.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("resume: %+v %v", claimed, err)
	}
	negative := coverageRecord("2")
	negative.CoverageScopes, negative.Ignored = []string{}, true
	run, err = reopened.SavePage(ctx, *claimed, model.Page{Items: []model.Record{negative}, Error: "Response failed", Next: json.RawMessage(`{"page":4}`)}, "rejected")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := reopened.NextRefreshID(ctx, run.ID, 0, maximum); err != nil || id != 2 {
		t.Fatalf("rejected observation consumed a pending identity: %d %v", id, err)
	}
	_, err = reopened.SavePage(ctx, run, model.Page{Items: []model.Record{negative}, Next: json.RawMessage(`{"page":4}`)}, "accepted")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := reopened.NextRefreshID(ctx, run.ID, 0, maximum); err != nil || id != 10 {
		t.Fatalf("valid out-of-scope evidence did not retire the candidate: %d %v", id, err)
	}
}

func TestFilterCoverageCountsOnlyCurrentDistinctNativeIdentities(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := model.Provider{Version: 1, ID: "filter-coverage", Name: "Coverage", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModePreview)
	pageNumber := 0
	save := func(fingerprint string, total any, records ...model.Record) {
		t.Helper()
		pageNumber++
		var err error
		run, err = db.SavePage(ctx, run, model.Page{
			Items: records,
			Metadata: map[string]any{
				"traversal_phase": "options_list", "fingerprint_scope": fingerprint, "total": total,
			},
			Next: json.RawMessage(fmt.Sprintf(`{"page":%d}`, pageNumber)),
		}, fmt.Sprintf("filter-page-%d", pageNumber))
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func(want bool, fingerprints ...string) {
		t.Helper()
		complete, err := db.FilterComplete(ctx, run.ID, "root", fingerprints)
		if err != nil || complete != want {
			t.Fatalf("filter coverage %v = %v, %v; want %v", fingerprints, complete, err, want)
		}
	}
	check(false, "base")
	rejected := coverageRecord("2", "root")
	rejected.Error = "Invalid record"
	ignored := coverageRecord("3", "root")
	ignored.Ignored = true
	auxiliary := coverageRecord("4", "root")
	auxiliary.Auxiliary = true
	save("base", 3, coverageRecord("1", "root"), coverageRecord("1", "root"), rejected, ignored, auxiliary)
	check(false, "base")
	save("reverse", 3, coverageRecord("1", "root"), coverageRecord("2", "root"))
	check(false, "base", "reverse")
	save("other", 3, coverageRecord("3", "root"))
	check(true, "base", "reverse", "other")
	check(false, "base", "other")
	if complete, err := db.FilterComplete(ctx, "another-run", "root", []string{"base", "reverse", "other"}); err != nil || complete {
		t.Fatalf("filter evidence leaked between runs: %v %v", complete, err)
	}
	if complete, err := db.FilterComplete(ctx, run.ID, "another-scope", []string{"base", "reverse", "other"}); err != nil || complete {
		t.Fatalf("filter evidence leaked between scopes: %v %v", complete, err)
	}
	if _, err := db.FinishRun(ctx, run.ID, model.StatusPaused, "", model.PauseBudget); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var err error
	db, err = Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	check(true, "base", "reverse", "other")
	if _, err := db.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("resume: %v %v", claimed, err)
	}
	run = *claimed
	save("changed", 4, coverageRecord("4", "root"))
	check(false, "base", "reverse", "other", "changed")
	save("empty", 0)
	check(true, "empty")
	save("missing-total", nil)
	check(false, "missing-total")
	save("invalid-total", "three")
	check(false, "invalid-total")
	run, err = db.SavePage(ctx, run, model.Page{
		RefreshScopes: []string{"root"}, Next: json.RawMessage(`{"phase":"refresh"}`),
	}, "retire-filter-evidence")
	if err != nil {
		t.Fatal(err)
	}
	check(false, "base", "reverse", "other")
}
