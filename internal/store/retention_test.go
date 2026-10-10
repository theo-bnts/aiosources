package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSavePageKeepsObservationsWithoutPayloadDuplication(t *testing.T) {
	for _, mode := range []model.RunMode{model.ModePreview, model.ModeIncremental, model.ModeFull, model.ModeMetadata} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, _, db := activityDatabase(t)
			provider := metadataSource("compact-observations")
			if mode == model.ModeMetadata {
				publishSharingRow(t, ctx, db, provider.ID, "10", `{"info_hash":"0123456789012345678901234567890123456789"}`, nil)
			}
			run := mirrorClaim(t, ctx, db, provider, mode)
			record := coverageRecord("10")
			record.Fields["external_ids"], record.Fields["metadata"] = []any{}, map[string]any{"imdb": "tt123"}
			page := model.Page{Body: []byte("large response bytes"), ContentType: "application/json", Items: []model.Record{record}, Next: json.RawMessage(`{"page":2}`)}
			if mode == model.ModeMetadata {
				page.Metadata = map[string]any{"auxiliary_response": true}
			}
			var err error
			run, err = db.SavePage(ctx, run, page, "accepted")
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := append([]byte(nil), run.Cursor...)
			page.Error, page.Body, page.Next = "Source rate limit reached", []byte("quota response bytes"), json.RawMessage(`{"page":99}`)
			run, err = db.SavePage(ctx, run, page, "")
			if err != nil || !bytes.Equal(run.Cursor, checkpoint) || run.Pages != 1 || run.Records != 2 || run.Errors != 1 {
				t.Fatalf("compact error observation changed checkpoint or counters: %+v %v", run, err)
			}
			raw, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || raw.Total != 2 {
				t.Fatalf("compact audit lost observations: %+v %v", raw, err)
			}
			for _, observation := range raw.Items {
				stored, err := db.Raw(ctx, observation.ID)
				if err != nil || stored.PayloadRetained || len(stored.Raw) != 0 || stored.SourceID != record.SourceID {
					t.Fatalf("new record payload was retained: %+v %v", stored, err)
				}
				if mode == model.ModePreview {
					if !reflect.DeepEqual(stored.Fields, record.Fields) {
						t.Fatalf("preview lost parsed sample fields: %+v", stored.Fields)
					}
				} else if len(stored.Fields) != 0 {
					t.Fatalf("non-preview observation duplicated interpreted fields: %+v", stored.Fields)
				}
				if body, _, err := db.RawPage(ctx, observation.PageID); !errors.Is(err, model.ErrNotFound) || len(body) != 0 {
					t.Fatalf("new response was available as an archive: %q %v", body, err)
				}
			}
			var bytesStored int64
			if err := db.pool.QueryRow(ctx, `SELECT
COALESCE((SELECT sum(octet_length(body)) FROM ingest.pages WHERE run_id=$1),0)
+ COALESCE((SELECT sum(octet_length(raw)) FROM ingest.raw_records WHERE run_id=$1),0)`, run.ID).Scan(&bytesStored); err != nil || bytesStored != 0 {
				t.Fatalf("source payload bytes were persisted: %d %v", bytesStored, err)
			}
			events, err := db.Events(ctx, run.ID, model.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var saved, failed bool
			for _, event := range events.Items {
				if event.Kind == "page_saved" || event.Kind == "page_error" {
					if retained, ok := event.Data["payload_retained"].(bool); !ok || retained {
						t.Fatalf("page event does not disclose absent payload: %+v", event)
					}
					saved = saved || event.Kind == "page_saved"
					failed = failed || event.Kind == "page_error"
				}
			}
			if !saved || !failed {
				t.Fatalf("page success/error observations missing: %+v", events.Items)
			}
			if mode == model.ModeFull {
				var fields []byte
				if err := db.pool.QueryRow(ctx, "SELECT fields FROM ingest.staged_torrents WHERE run_id=$1 AND source_id='10'", run.ID).Scan(&fields); err != nil {
					t.Fatal(err)
				}
				parsed, err := decodeObject(fields)
				if err != nil || !reflect.DeepEqual(parsed, record.Fields) {
					t.Fatalf("compact audit discarded real staged values: %+v %v", parsed, err)
				}
			} else if mode != model.ModePreview {
				row := mirrorRows(t, ctx, db, provider.ID)["10"]
				if !reflect.DeepEqual(row.Fields["metadata"], record.Fields["metadata"]) {
					t.Fatalf("compact audit discarded canonical metadata: %+v", row)
				}
			}
		})
	}
}

func TestCompactObservationsPreservePostgresInterpretationErrors(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{ID: "invalid-fields", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	invalid, overflow, badID, valid := coverageRecord("nul"), coverageRecord("overflow"), coverageRecord("bad\u0000id"), coverageRecord("valid")
	invalid.Fields["title"] = "bad\u0000value"
	overflow.Fields["size"] = json.Number("1e1000000")
	run, err := db.SavePage(ctx, run, model.Page{Body: []byte("source body"), Items: []model.Record{invalid, overflow, badID, valid}, Done: true}, "")
	if err != nil || run.Errors != 3 || run.DistinctRecords != 1 {
		t.Fatalf("database field rejection escaped its compact observation: %+v %v", run, err)
	}
	rows := mirrorRows(t, ctx, db, provider.ID)
	if len(rows) != 1 || rows["valid"].SourceID != "valid" {
		t.Fatalf("unrepresentable records entered canonical catalogue: %+v", rows)
	}
	observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
	if err != nil || observations.Total != 4 {
		t.Fatalf("invalid records lost their observations: %+v %v", observations, err)
	}
	failures := 0
	for _, record := range observations.Items {
		if record.Error != "" {
			failures++
		}
		if record.PayloadRetained || len(record.Fields) != 0 {
			t.Fatalf("invalid interpretation retained duplicate data: %+v", record)
		}
	}
	if failures != 3 {
		t.Fatalf("database errors were hidden after disabling raw fields: %+v", observations.Items)
	}
}

func TestMetadataRetentionMigrationLeavesLegacyArchivesAndCheckpointsUntouched(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("legacy-archives")
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("old")}, Next: json.RawMessage(`{"old_cursor":{"unmodified":true}}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	run = mirrorFinish(t, ctx, db, run, model.StatusPaused)
	observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
	if err != nil || len(observations.Items) != 1 {
		t.Fatalf("missing legacy fixture: %+v %v", observations, err)
	}
	observation := observations.Items[0]
	oldBody, oldRaw := []byte{'p', 0, 'g'}, []byte{'r', 0, 'w'}
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.pages SET body=$2 WHERE id=$1", observation.PageID, oldBody); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.raw_records SET raw=$2,fields='{"legacy":{"exact":true}}'::jsonb WHERE id=$1`, observation.ID, oldRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.pages DROP COLUMN payload_retained;
ALTER TABLE ingest.raw_records DROP COLUMN payload_retained;
ALTER TABLE ingest.runs DROP CONSTRAINT runs_mode_check;
ALTER TABLE ingest.runs ADD CONSTRAINT runs_mode_check CHECK(mode IN ('preview','incremental','full'));
DELETE FROM ingest.schema_migrations WHERE version=20`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	archived, err := db.Raw(ctx, observation.ID)
	if err != nil || !archived.PayloadRetained || !bytes.Equal(archived.Raw, oldRaw) || !reflect.DeepEqual(archived.Fields, map[string]any{"legacy": map[string]any{"exact": true}}) {
		t.Fatalf("migration altered historical raw bytes or fields: %+v %v", archived, err)
	}
	body, _, err := db.RawPage(ctx, observation.PageID)
	if err != nil || !bytes.Equal(body, oldBody) {
		t.Fatalf("migration hid or changed historical response: %q %v", body, err)
	}
	restored, err := db.GetRun(ctx, run.ID)
	if err != nil || !bytes.Equal(restored.Cursor, run.Cursor) || !reflect.DeepEqual(restored.Config, run.Config) || !reflect.DeepEqual(restored.Policy, run.Policy) || restored.Status != run.Status {
		t.Fatalf("migration changed immutable run continuation: %+v %v", restored, err)
	}
	metadata := mirrorClaim(t, ctx, db, provider, model.ModeMetadata)
	if metadata.Mode != model.ModeMetadata {
		t.Fatalf("migrated run-mode constraint rejected metadata: %+v", metadata)
	}
}

func TestRejectedAtomicPagesPreserveStagingAndCoverage(t *testing.T) {
	for _, remote := range []bool{false, true} {
		name := "native-full"
		if remote {
			name = "remote"
		}
		t.Run(name, func(t *testing.T) {
			ctx, _, db := activityDatabase(t)
			provider := model.Provider{ID: name, Adapter: "http_json"}
			item := func(id, title string) model.Record {
				record := coverageRecord(id, "root")
				record.Fields["title"] = title
				return record
			}
			if remote {
				provider = mirrorSource(name)
				item = func(id, title string) model.Record {
					record := mirrorItem(id, map[string]any{"title": title}, false)
					record.CoverageScopes = []string{"root"}
					return record
				}
			}
			run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
			original, sibling := item("old", "Original"), item("keep", "Unchanged")
			next := json.RawMessage(`{"page":2}`)
			final := json.RawMessage(`{"done":true}`)
			if remote {
				var state model.CatalogCursor
				if err := json.Unmarshal(run.Cursor, &state); err != nil {
					t.Fatal(err)
				}
				state.InstanceID, state.Mode, state.Cursor = "owner-instance", "full", "next"
				next, _ = json.Marshal(state)
				state.Cursor, state.Checkpoint, state.Done = "", "complete", true
				final, _ = json.Marshal(state)
			}
			var err error
			run, err = db.SavePage(ctx, run, model.Page{Items: []model.Record{original, sibling}, Next: next}, "first")
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := append([]byte(nil), run.Cursor...)
			invalid := item("invalid", "invalid\u0000value")
			for _, mutation := range []string{"overwrite", "reset", "refresh"} {
				page := model.Page{Items: []model.Record{item("old", "Rejected replacement"), invalid}, Next: final, Done: true}
				if mutation == "reset" {
					page.ResetStaging = true
				}
				if mutation == "refresh" {
					page.RefreshScopes = []string{"root"}
				}
				run, err = db.SavePage(ctx, run, page, mutation)
				if err != nil {
					t.Fatal(err)
				}
				if run.Pages != 1 || run.TraversalDone || run.DistinctRecords != 2 || !bytes.Equal(run.Cursor, checkpoint) {
					t.Fatalf("%s rejection changed checkpoint: %+v", mutation, run)
				}
				assertCoverage(t, ctx, db, run.ID, map[string]int64{"root": 2}, []string{original.SourceID, sibling.SourceID, invalid.SourceID}, map[string]bool{original.SourceID: true, sibling.SourceID: true})
			}
			observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
			if err != nil || observations.Total != 8 || run.Errors != 3 {
				t.Fatalf("rejected pages lost observations: total=%d errors=%d error=%v", observations.Total, run.Errors, err)
			}
			run, err = db.SavePage(ctx, run, model.Page{Next: final, Done: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
			published := mirrorRows(t, ctx, db, provider.ID)
			if len(published) != 2 || !reflect.DeepEqual(published[original.SourceID].Fields, original.Fields) || !reflect.DeepEqual(published[sibling.SourceID].Fields, sibling.Fields) {
				t.Fatalf("rejected page changed retained publication: %+v", published)
			}
		})
	}
}
