package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDeduplicatedArchivesKeepObservationIdentityBytesAndSearch(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := metadataSource("deduplicated-archives")
	body, raw := []byte{'p', 0, 0xff, 'g'}, []byte{'r', 0, 0xfe, 'w'}
	var records []model.RawRecord
	var runs []model.Run
	for range 2 {
		run := mirrorClaim(t, ctx, db, provider, model.ModePreview)
		run, err := db.SavePage(ctx, run, model.Page{ContentType: "application/octet-stream", Items: []model.Record{coverageRecord("native")}, Next: json.RawMessage(`{"done":true}`), Done: true}, "")
		if err != nil {
			t.Fatal(err)
		}
		run = mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
		observations, err := db.ListRaw(ctx, model.ListOptions{RunID: run.ID})
		if err != nil || len(observations.Items) != 1 {
			t.Fatalf("missing observation fixture: %+v %v", observations, err)
		}
		observation := observations.Items[0]
		if _, err := db.pool.Exec(ctx, `UPDATE ingest.raw_records SET raw=$2,fields='{"title":"dedupe-marker","exact":9007199254740993}'::jsonb,payload_retained=TRUE WHERE id=$1`, observation.ID, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := db.pool.Exec(ctx, `UPDATE ingest.pages SET body=$2,payload_retained=TRUE WHERE id=$1`, observation.PageID, body); err != nil {
			t.Fatal(err)
		}
		observation, err = db.Raw(ctx, observation.ID)
		if err != nil {
			t.Fatal(err)
		}
		records, runs = append(records, observation), append(runs, run)
	}
	keeper, duplicate := records[0], records[1]
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.raw_records SET payload_id=$2,raw=''::bytea,fields='{}'::jsonb WHERE id=$1`, duplicate.ID, keeper.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.pages SET payload_id=$2,body=''::bytea WHERE id=$1`, duplicate.PageID, keeper.PageID); err != nil {
		t.Fatal(err)
	}
	got, err := db.Raw(ctx, duplicate.ID)
	if err != nil || !reflect.DeepEqual(got, duplicate) {
		t.Fatalf("deduplication changed the observation or its exact archived content: %+v %v", got, err)
	}
	response, contentType, err := db.RawPage(ctx, duplicate.PageID)
	if err != nil || !bytes.Equal(response, body) || contentType != "application/octet-stream" {
		t.Fatalf("deduplication changed the response download: %q %q %v", response, contentType, err)
	}
	matches, err := db.ListRaw(ctx, model.ListOptions{RunID: duplicate.RunID, Query: "dedupe-marker"})
	if err != nil || matches.Total != 1 || len(matches.Items) != 1 || matches.Items[0].ID != duplicate.ID || !reflect.DeepEqual(matches.Items[0].Fields, duplicate.Fields) {
		t.Fatalf("search lost referenced fields or returned the keeper's identity: %+v %v", matches, err)
	}
	var storedRaw, storedBodies int64
	if err := db.pool.QueryRow(ctx, `SELECT sum(octet_length(raw)) FROM ingest.raw_records WHERE provider_id=$1`, provider.ID).Scan(&storedRaw); err != nil {
		t.Fatal(err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT sum(octet_length(body)) FROM ingest.pages WHERE provider_id=$1`, provider.ID).Scan(&storedBodies); err != nil {
		t.Fatal(err)
	}
	if storedRaw != int64(len(raw)) || storedBodies != int64(len(body)) {
		t.Fatalf("duplicate physical bytes remain: raw=%d bodies=%d", storedRaw, storedBodies)
	}
	for _, before := range runs {
		after, err := db.GetRun(ctx, before.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("archive deduplication changed run state: before=%+v after=%+v %v", before, after, err)
		}
	}
}
