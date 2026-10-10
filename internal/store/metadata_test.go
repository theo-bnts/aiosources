package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func metadataSource(id string) model.Provider {
	return model.Provider{ID: id, Adapter: "http_json",
		Mapping: model.Mapping{ID: "id", Fields: map[string]string{"title": "title", "info_hash": "infoHash", "language": "language"}},
		Traversal: &model.JSONTraversal{EnrichFields: []string{"external_ids", "metadata"}, IDRecovery: &model.JSONIDRecovery{
			Mapping: model.Mapping{ID: "id", Fields: map[string]string{
				"title": "title", "info_hash": "infoHash", "external_ids": "externalIds", "metadata": "metadata", "description": "description",
			}},
		}},
	}
}

func TestMetadataCandidatesUsePublishedNativeKeysetAndCreationBoundary(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("metadata-candidates")
	// Legacy definitions may still request counters: known durable metadata
	// must not become a permanent candidate merely because counters are retired.
	provider.Traversal.EnrichFields = append(provider.Traversal.EnrichFields, "seeders")
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	complete, partial := coverageRecord("20"), coverageRecord("30")
	complete.Fields["external_ids"], complete.Fields["metadata"] = []any{}, false
	partial.Fields["external_ids"] = []any{}
	hashless := coverageRecord("35")
	delete(hashless.Fields, "info_hash")
	seed, err := db.SavePage(ctx, seed, model.Page{Items: []model.Record{coverageRecord("10"), complete, partial, hashless, coverageRecord("40")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	// Explicit JSON null also needs enrichment; it differs from known emptiness.
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=fields || '{"metadata":null}'::jsonb WHERE provider_id=$1 AND source_id='40'`, provider.ID); err != nil {
		t.Fatal(err)
	}
	origin := model.CatalogOrigin{InstanceID: "friend", ProviderID: "remote", SourceID: "15"}
	publishSharingRow(t, ctx, db, provider.ID, model.CatalogItemID(origin), `{"info_hash":"0123456789012345678901234567890123456789"}`, &origin)
	run := mirrorClaim(t, ctx, db, provider, model.ModeMetadata)
	publishSharingRow(t, ctx, db, provider.ID, "50", `{"info_hash":"0123456789012345678901234567890123456789"}`, nil)
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET first_seen_at=$2 WHERE provider_id=$1 AND source_id='50'", provider.ID, run.CreatedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "another-provider", "05", `{"info_hash":"0123456789012345678901234567890123456789"}`, nil)
	first, err := db.MetadataCandidates(ctx, run, "", 2)
	want := []model.MetadataCandidate{{SourceID: "10", InfoHash: "0123456789012345678901234567890123456789"}, {SourceID: "30", InfoHash: "0123456789012345678901234567890123456789"}}
	if err != nil || !reflect.DeepEqual(first, want) {
		t.Fatalf("candidate selection collapsed hash siblings, exceeded its bound or ignored known values: %+v %v", first, err)
	}
	next, err := db.MetadataCandidates(ctx, run, first[1].SourceID, 2)
	if err != nil || !reflect.DeepEqual(next, []model.MetadataCandidate{{SourceID: "35"}, {SourceID: "40", InfoHash: want[0].InfoHash}}) {
		t.Fatalf("candidate continuation skipped null fields or a hashless native record: %+v %v", next, err)
	}
	last, err := db.MetadataCandidates(ctx, run, "40", 100)
	if err != nil || len(last) != 0 {
		t.Fatalf("metadata included newly published or remote-origin rows: %+v %v", last, err)
	}
	if _, err := db.MetadataCandidates(ctx, run, "", 101); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("unbounded metadata query accepted: %v", err)
	}
}

func TestMetadataUpdatesOnlyMissingValuesAndResumesCommittedCandidates(t *testing.T) {
	ctx, endpoint, db := activityDatabase(t)
	provider := metadataSource("metadata-update")
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	original := coverageRecord("10")
	original.Fields["external_ids"] = []any{}
	original.Fields["attributes"] = map[string]any{"language": []any{"7"}}
	seed, err := db.SavePage(ctx, seed, model.Page{Items: []model.Record{original, coverageRecord("20"), coverageRecord("30")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	before := mirrorRows(t, ctx, db, provider.ID)
	run := mirrorClaim(t, ctx, db, provider, model.ModeMetadata)
	detail := coverageRecord("10")
	detail.Fields["title"] = "Do not replace known title"
	detail.Fields["external_ids"] = []any{"do-not-replace-empty"}
	detail.Fields["metadata"] = map[string]any{"imdb": "tt123"}
	detail.Fields["attributes"] = map[string]any{"language": []any{"99"}, "imdbid": []any{"tt123"}}
	cursor := json.RawMessage(`{"metadata_version":1,"after":"10"}`)
	run, err = db.SavePage(ctx, run, model.Page{Body: []byte("detail response"), Items: []model.Record{detail}, Metadata: map[string]any{"auxiliary_response": true}, Next: cursor}, "detail-10")
	if err != nil {
		t.Fatal(err)
	}
	after := mirrorRows(t, ctx, db, provider.ID)
	if len(after) != len(before) || after["10"].Fields["title"] != "10" || !after["10"].FirstSeenAt.Equal(before["10"].FirstSeenAt) ||
		!reflect.DeepEqual(after["10"].Fields["external_ids"], []any{}) ||
		!reflect.DeepEqual(after["10"].Fields["metadata"], detail.Fields["metadata"]) ||
		!reflect.DeepEqual(after["10"].Fields["attributes"], map[string]any{"language": []any{"7"}, "imdbid": []any{"tt123"}}) ||
		!reflect.DeepEqual(after["20"], before["20"]) {
		t.Fatalf("metadata overwrote known values, timestamps or native membership: %+v", after)
	}
	// An unavailable/mismatched candidate stays incomplete, but its committed
	// keyset position must still prevent replay after a process restart.
	skipped := coverageRecord("different-native-id")
	skipped.Auxiliary, skipped.Ignored = true, true
	run, err = db.SavePage(ctx, run, model.Page{Body: []byte("mismatched detail"), Items: []model.Record{skipped}, Metadata: map[string]any{"auxiliary_response": true, "detail_skip_reason": "native_id_mismatch"}, Next: json.RawMessage(`{"metadata_version":1,"after":"20"}`)}, "")
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
	if _, err := reopened.ResumeRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	claimed, err := reopened.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("metadata could not resume: %+v %v", claimed, err)
	}
	run = *claimed
	var state struct {
		After string `json:"after"`
	}
	if err := json.Unmarshal(run.Cursor, &state); err != nil || state.After != "20" {
		t.Fatalf("metadata checkpoint changed on restart: %s %v", run.Cursor, err)
	}
	candidates, err := reopened.MetadataCandidates(ctx, run, state.After, 100)
	if err != nil || len(candidates) != 1 || candidates[0].SourceID != "30" {
		t.Fatalf("metadata replayed a committed skip or lost hash sibling: %+v %v", candidates, err)
	}
	wrongHash, nonexistent := coverageRecord("20"), coverageRecord("new")
	wrongHash.Fields["info_hash"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	wrongHash.Fields["metadata"], nonexistent.Fields["metadata"] = "wrong identity", "not a member"
	run, err = reopened.SavePage(ctx, run, model.Page{Items: []model.Record{wrongHash, nonexistent}, Metadata: map[string]any{"auxiliary_response": true}, Next: json.RawMessage(`{"metadata_version":1,"after":"30","done":true}`), Done: true}, "guards")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, reopened, run, model.StatusSucceeded)
	guarded := mirrorRows(t, ctx, reopened, provider.ID)
	if !reflect.DeepEqual(guarded, after) {
		t.Fatalf("metadata inserted/deleted membership or applied another hash: before=%+v after=%+v", after, guarded)
	}
	changes, err := reopened.RunChanges(ctx, run.ID, model.ListOptions{})
	if err != nil || changes.Counts != (model.PublicationCounts{Updated: 1}) {
		t.Fatalf("metadata history did not expose exactly one enriched occurrence: %+v %v", changes, err)
	}
}

func TestMetadataInvalidFieldsKeepWholePageAndCheckpointUnapplied(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("metadata-invalid")
	publishSharingRow(t, ctx, db, provider.ID, "10", `{"title":"Historical","info_hash":"0123456789012345678901234567890123456789"}`, nil)
	before := mirrorRows(t, ctx, db, provider.ID)
	run := mirrorClaim(t, ctx, db, provider, model.ModeMetadata)
	valid, invalid := coverageRecord("10"), coverageRecord("10")
	valid.Fields["metadata"] = "must not partially commit"
	invalid.Fields["metadata"] = "unrepresentable\u0000value"
	saved, err := db.SavePage(ctx, run, model.Page{Body: []byte("source bytes"), Items: []model.Record{valid, invalid}, Metadata: map[string]any{"auxiliary_response": true}, Next: json.RawMessage(`{"after":"10"}`), Done: true}, "invalid")
	if err != nil || saved.Errors != 1 || saved.TraversalDone || !bytes.Equal(saved.Cursor, run.Cursor) || saved.Pages != 0 {
		t.Fatalf("invalid metadata advanced its checkpoint: %+v %v", saved, err)
	}
	if after := mirrorRows(t, ctx, db, provider.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid metadata page partially published: %+v", after)
	}
	valid.Fields["external_ids"] = []any{}
	saved, err = db.SavePage(ctx, saved, model.Page{Items: []model.Record{valid}, Metadata: map[string]any{"auxiliary_response": true}, Next: json.RawMessage(`{"after":"10","done":true}`), Done: true}, "retry")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, saved, model.StatusSucceeded)
	row := mirrorRows(t, ctx, db, provider.ID)["10"]
	if row.Fields["metadata"] != valid.Fields["metadata"] || row.Fields["title"] != "Historical" || !row.FirstSeenAt.Equal(before["10"].FirstSeenAt) {
		t.Fatalf("metadata retry failed to enrich historical identity: %+v", row)
	}
}

func TestFullPreservesOnlySameIdentityDetailMetadataAtAtomicPublication(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("full-metadata")
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	enriched := func(id string) model.Record {
		r := coverageRecord(id)
		r.Fields["external_ids"], r.Fields["metadata"] = []any{"tt123"}, map[string]any{"imdb": "tt123"}
		r.Fields["description"], r.Fields["obsolete"] = "detail description", "not mapped detail"
		r.Fields["attributes"] = map[string]any{"imdbid": []any{"tt123"}, "language": []any{"1"}}
		return r
	}
	seed, err := db.SavePage(ctx, seed, model.Page{Items: []model.Record{enriched("keep"), enriched("changed"), enriched("removed")}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	before := mirrorRows(t, ctx, db, provider.ID)
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	// A moving catalogue can replace the hash between two observations within
	// the same Full; its earlier staged details must not contaminate the new hash.
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{enriched("changed")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	keep, changed, sibling := coverageRecord("keep"), coverageRecord("changed"), coverageRecord("sibling")
	keep.Fields["title"], keep.Fields["external_ids"] = "Authoritative list title", []any{}
	keep.Fields["attributes"] = map[string]any{"language": []any{"20"}}
	changed.Fields["info_hash"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{keep, changed, sibling}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows := mirrorRows(t, ctx, db, provider.ID); !reflect.DeepEqual(rows, before) {
		t.Fatalf("full metadata preservation published before completion: %+v", rows)
	}
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	rows := mirrorRows(t, ctx, db, provider.ID)
	got := rows["keep"]
	if len(rows) != 3 || rows["removed"].SourceID != "" || got.Fields["title"] != keep.Fields["title"] || got.Fields["description"] != "detail description" || got.Fields["obsolete"] != nil ||
		!reflect.DeepEqual(got.Fields["metadata"], before["keep"].Fields["metadata"]) || !reflect.DeepEqual(got.Fields["external_ids"], []any{}) ||
		!reflect.DeepEqual(got.Fields["attributes"], map[string]any{"imdbid": []any{"tt123"}, "language": []any{"20"}}) ||
		!got.FirstSeenAt.Equal(before["keep"].FirstSeenAt) || rows["changed"].Fields["metadata"] != nil || rows["changed"].Fields["attributes"] != nil || rows["sibling"].Fields["metadata"] != nil {
		t.Fatalf("full publication lost enrichment, authoritative values or exact native membership: %+v", rows)
	}
}

func TestIncrementalInvalidatesMetadataWhenNativeHashChanges(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("incremental-metadata")
	seed := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	unchanged, changed := coverageRecord("same"), coverageRecord("changed")
	for _, record := range []model.Record{unchanged, changed} {
		record.Fields["external_ids"] = []any{}
		record.Fields["metadata"] = map[string]any{"imdb": "tt123"}
		record.Fields["attributes"] = map[string]any{"imdbid": []any{"tt123"}}
	}
	seed, err := db.SavePage(ctx, seed, model.Page{Items: []model.Record{unchanged, changed}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, seed, model.StatusSucceeded)
	refresh := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	unchanged, changed = coverageRecord("same"), coverageRecord("changed")
	unchanged.Fields["title"] = "Refreshed listing"
	changed.Fields["info_hash"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	refresh, err = db.SavePage(ctx, refresh, model.Page{Items: []model.Record{unchanged, changed}, Done: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, refresh, model.StatusSucceeded)
	rows := mirrorRows(t, ctx, db, provider.ID)
	if len(rows) != 2 || rows["same"].Fields["metadata"] == nil || rows["same"].Fields["title"] != "Refreshed listing" ||
		rows["changed"].Fields["metadata"] != nil || rows["changed"].Fields["external_ids"] != nil || rows["changed"].Fields["attributes"] != nil {
		t.Fatalf("incremental either erased same-hash metadata or transferred it to a different hash: %+v", rows)
	}
	metadata := mirrorClaim(t, ctx, db, provider, model.ModeMetadata)
	candidates, err := db.MetadataCandidates(ctx, metadata, "", 100)
	if err != nil || len(candidates) != 1 || candidates[0].SourceID != "changed" || candidates[0].InfoHash != changed.Fields["info_hash"] {
		t.Fatalf("replacement hash did not become eligible for its own metadata: %+v %v", candidates, err)
	}
}
