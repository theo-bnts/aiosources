package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestTorrentSearchPrecisionCombinedFiltersAndIndependentOccurrences(t *testing.T) {
	ctx, db := sharingDatabase(t)
	hash := strings.Repeat("a", 40)
	for _, pair := range [][2]string{{"one", "first"}, {"one", "second"}, {"two", "first"}} {
		publishSharingRow(t, ctx, db, pair[0], pair[1], `{"title":"Blue Ocean","size":9007199254740993,"seeders":"12","categories":["Films",2000],"published_at":"2026-09-01T12:00:00Z","info_hash":"`+hash+`"}`, nil)
	}
	publishSharingRow(t, ctx, db, "one", "malformed", `{"title":"Blue Ocean","size":"1e9999999999","seeders":{},"published_at":"2026-02-30T00:00:00Z","categories":null}`, nil)
	if _, err := db.pool.Exec(ctx, "UPDATE ingest.torrents SET last_seen_at='2026-09-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	options := model.ListOptions{Query: `"blue ocean"`, Providers: []string{"one", "two"}, Category: "fILMS", MinSize: "9007199254740993", MaxSize: "9007199254740993", PublishedAfter: "2026-09-01T00:00:00Z", PublishedBefore: "2026-09-02T00:00:00Z", InfoHash: hash, Sort: "relevance", Limit: 2}
	first, err := db.ListTorrents(ctx, options)
	if err != nil || first.Total != 3 || len(first.Items) != 2 {
		t.Fatalf("combined search: %+v %v", first, err)
	}
	if first.Items[0].SourceID != "first" || first.Items[1].SourceID != "second" || first.Items[0].ProviderID != "one" {
		t.Fatalf("unstable source tie-break: %+v", first.Items)
	}
	if first.Items[0].Fields["size"] != json.Number("9007199254740993") {
		t.Fatalf("size lost precision: %#v", first.Items[0].Fields["size"])
	}
	if !ValidOccurrenceID(first.Items[0].OccurrenceID) || first.Items[0].OccurrenceID == first.Items[1].OccurrenceID {
		t.Fatalf("occurrence identity collapsed: %+v", first.Items)
	}
	options.Offset = 2
	last, err := db.ListTorrents(ctx, options)
	if err != nil || last.Total != 3 || len(last.Items) != 1 || last.Items[0].ProviderID != "two" || last.Items[0].OccurrenceID == first.Items[0].OccurrenceID {
		t.Fatalf("cross-provider occurrence hidden: %+v %v", last, err)
	}
	related, err := db.RelatedTorrents(ctx, first.Items[0].OccurrenceID, model.ListOptions{})
	if err != nil || related.Total != 2 {
		t.Fatalf("related occurrence count: %+v %v", related, err)
	}
	if related.Items[0].SourceID != "second" || related.Items[1].ProviderID != "two" {
		t.Fatalf("same-provider related occurrence hidden: %+v", related.Items)
	}
	// JSON property names must not be searchable keywords.
	properties, err := db.ListTorrents(ctx, model.ListOptions{Query: "size"})
	if err != nil || properties.Total != 0 {
		t.Fatalf("JSON property names polluted full-text search: %+v %v", properties, err)
	}
}

func TestTorrentSearchOversizedFieldsAndNumericPrefixCollisions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	prefix := strings.Repeat("9", 300)
	for _, suffix := range []string{"1", "2"} {
		fields := map[string]any{"title": "Needle " + strings.Repeat("界 ", 100000), "category": strings.Repeat("category", 20000), "size": json.Number(prefix + suffix), "published_at": map[string]any{"invalid": true}}
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		publishSharingRow(t, ctx, db, "large", suffix, string(encoded), nil)
	}
	rows, err := db.ListTorrents(ctx, model.ListOptions{Query: "needle", MinSize: prefix + "2", MaxSize: prefix + "2", Sort: "size_desc"})
	if err != nil || rows.Total != 1 || rows.Items[0].SourceID != "2" || rows.Items[0].Fields["size"] != json.Number(prefix+"2") {
		t.Fatalf("numeric prefix recheck lost exactness: %+v %v", rows, err)
	}
	if rows.Items[0].Fields["title"] != "Needle "+strings.Repeat("界 ", 100000) {
		t.Fatal("index bounds truncated canonical fields")
	}
}

func TestSemanticPublicationHistoryIgnoresStagingAndObservations(t *testing.T) {
	ctx, db := sharingDatabase(t)
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := model.Provider{Version: 1, ID: "source", Name: "Source", Adapter: "http_json", URL: "https://source.invalid/items"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	page := model.Page{Body: []byte("first"), Items: []model.Record{
		{SourceID: "keep", Raw: []byte("keep"), Fields: map[string]any{"title": "Original"}},
		{SourceID: "remove", Raw: []byte("remove"), Fields: map[string]any{"title": "Removed later", "arbitrary": json.Number("9007199254740993")}},
	}}
	var err error
	run, err = db.SavePage(ctx, run, page, "")
	if err != nil {
		t.Fatal(err)
	}
	page.Body = []byte("second")
	page.Items[0].Fields["title"] = "Changed"
	run, err = db.SavePage(ctx, run, page, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusFailed)
	changes, err := db.RunChanges(ctx, run.ID, model.ListOptions{})
	if err != nil || changes.Counts != (model.PublicationCounts{Added: 2, Updated: 1}) || changes.Total != 3 || !changes.HistoryComplete {
		t.Fatalf("partial committed incremental history: %+v %v", changes, err)
	}
	live, err := db.ListTorrents(ctx, model.ListOptions{ProviderID: provider.ID})
	if err != nil {
		t.Fatal(err)
	}
	var deletedID string
	for _, row := range live.Items {
		if row.SourceID == "remove" {
			deletedID = row.OccurrenceID
		}
	}
	staged := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	stagePage := model.Page{Body: []byte("staged"), Done: true, Items: []model.Record{{SourceID: "keep", Raw: []byte("staged"), Fields: map[string]any{"title": "Changed"}}}}
	staged, err = db.SavePage(ctx, staged, stagePage, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.RunChanges(ctx, staged.ID, model.ListOptions{})
	if err != nil || before.Total != 0 {
		t.Fatalf("unpublished staging counted: %+v %v", before, err)
	}
	mirrorFinish(t, ctx, db, staged, model.StatusFailed)
	failed, err := db.RunChanges(ctx, staged.ID, model.ListOptions{})
	if err != nil || failed.Counts != (model.PublicationCounts{}) {
		t.Fatalf("failed staging published: %+v %v", failed, err)
	}
	full := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	full, err = db.SavePage(ctx, full, stagePage, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, full, model.StatusSucceeded)
	published, err := db.RunChanges(ctx, full.ID, model.ListOptions{})
	if err != nil || published.Counts != (model.PublicationCounts{Deleted: 1}) {
		t.Fatalf("full publication semantic counts: %+v %v", published, err)
	}
	history, err := db.TorrentHistory(ctx, deletedID, model.ListOptions{})
	if err != nil || history.Total != 2 || history.Items[0].Kind != "deleted" || history.Items[0].After != nil || history.Items[0].Before["arbitrary"] != json.Number("9007199254740993") || history.Items[0].Origin == nil {
		t.Fatalf("deleted history missing exact before/provenance: %+v %v", history, err)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM ingest.torrent_history"); err == nil {
		t.Fatal("semantic history was mutable")
	}
}

func TestSemanticHistoryMigrationBaselineDoesNotInventTransitions(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.pool.Exec(ctx, "BEGIN; CREATE SCHEMA ingest;"+initialSchema+sharingSchema+catalogIdentityPrivacySchema+`
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('legacy','live','{"title":"Legacy"}'),('legacy','deleted','{"title":"Gone"}');
DELETE FROM ingest.torrents WHERE source_id='deleted';`+searchHistorySchema+"COMMIT;")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"live", "deleted"} {
		var id string
		if err := db.pool.QueryRow(ctx, "SELECT ingest.torrent_occurrence_id('legacy',$1)", source).Scan(&id); err != nil {
			t.Fatal(err)
		}
		history, err := db.TorrentHistory(ctx, id, model.ListOptions{})
		if err != nil || history.Total != 0 || history.Baseline == nil || history.Baseline.Deleted != (source == "deleted") {
			t.Fatalf("legacy baseline fabricated changes: %+v %v", history, err)
		}
	}
}

func TestSearchBoundValidation(t *testing.T) {
	for _, filters := range []model.SearchFilters{
		{MinSize: "9007199254740994", MaxSize: "9007199254740993"},
		{PublishedAfter: "2026-01-01T00:00:00Z", PublishedBefore: "0001-01-01T00:00:00Z"},
		{PublishedAfter: "0001-01-01T00:00:00+23:00"},
		{PublishedBefore: "9999-12-31T23:59:59.999999999-23:00"},
		{Sort: "seeders_desc"}, {Sort: "size;delete"},
	} {
		if _, err := NormalizeSearchFilters(filters); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid search filters accepted: %+v %v", filters, err)
		}
	}
}

func TestSearchHistoryMigrationPreservesMalformedTimestampOccurrences(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fields := `{"title":"Legacy malformed zone","published_at":"2026-09-01T00:00:00+99:00","size":9007199254740993,"arbitrary":{"keep":null},"info_hash":"` + strings.Repeat("b", 40) + `"}`
	_, err = db.pool.Exec(ctx, "BEGIN; CREATE SCHEMA ingest;"+initialSchema+sharingSchema+catalogIdentityPrivacySchema+`
INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('legacy-one','same','`+fields+`'),('legacy-two','same','`+fields+`');`+searchHistorySchema+"COMMIT;")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListTorrents(ctx, model.ListOptions{})
	if err != nil || rows.Total != 2 || len(rows.Items) != 2 {
		t.Fatalf("migration lost legacy occurrences: %+v %v", rows, err)
	}
	if rows.Items[0].OccurrenceID == rows.Items[1].OccurrenceID || rows.Items[0].ProviderID == rows.Items[1].ProviderID {
		t.Fatal("migration merged provider occurrences")
	}
	for _, row := range rows.Items {
		encoded, err := json.Marshal(row.Fields)
		if err != nil {
			t.Fatal(err)
		}
		var equal, unindexed bool
		if err := db.pool.QueryRow(ctx, "SELECT $1::jsonb=$2::jsonb,search_published_at IS NULL FROM ingest.torrents WHERE occurrence_id=$3", encoded, fields, row.OccurrenceID).Scan(&equal, &unindexed); err != nil || !equal || !unindexed {
			t.Fatalf("invalid projection changed legacy fields: equal=%v unindexed=%v %v", equal, unindexed, err)
		}
		history, err := db.TorrentHistory(ctx, row.OccurrenceID, model.ListOptions{})
		if err != nil || history.Total != 0 || history.Baseline == nil || !reflect.DeepEqual(history.Baseline.Fields, row.Fields) {
			t.Fatalf("legacy baseline lost exact fields: %+v %v", history, err)
		}
	}
}

func TestRemoteMalformedTimestampPublicationPreservesFieldsAndSemanticHistory(t *testing.T) {
	ctx, db := sharingDatabase(t)
	setStorageMode(t, ctx, db, model.StorageFull)
	provider := mirrorSource("remote-dates")
	hash := strings.Repeat("c", 40)
	original := mirrorItem("invalid-zone", map[string]any{"title": "Original", "published_at": "2026-09-01T00:00:00+99:00", "size": json.Number("9007199254740993"), "info_hash": hash, "arbitrary": nil}, false)
	control := mirrorItem("valid-zone", map[string]any{"title": "Control", "published_at": "2026-09-01T00:00:00Z", "info_hash": hash}, false)
	full := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	full = mirrorPage(t, ctx, db, full, "full", "malformed-baseline", true, original, control)
	mirrorFinish(t, ctx, db, full, model.StatusSucceeded)
	mirrorCheckpoint(t, ctx, db, provider, "malformed-baseline")
	changed := mirrorItem("invalid-zone", map[string]any{"title": "Changed", "published_at": "2026-09-01T00:00:00-99:00", "size": json.Number("9007199254740993"), "info_hash": hash, "arbitrary": nil}, false)
	delta := mirrorClaim(t, ctx, db, provider, model.ModeIncremental)
	delta = mirrorPage(t, ctx, db, delta, "incremental", "malformed-delta", true, changed)
	mirrorFinish(t, ctx, db, delta, model.StatusSucceeded)
	mirrorCheckpoint(t, ctx, db, provider, "malformed-delta")
	rows := mirrorRows(t, ctx, db, provider.ID)
	row := rows[changed.SourceID]
	if len(rows) != 2 || !reflect.DeepEqual(row.Fields, changed.Fields) || row.Origin == nil || *row.Origin != *changed.Origin {
		t.Fatalf("malformed timestamp changed remote occurrence fields: %+v", rows)
	}
	if row.RawID == nil {
		t.Fatal("remote publication lost raw record reference")
	}
	raw, err := db.Raw(ctx, *row.RawID)
	if err != nil || len(raw.Raw) != 0 || len(raw.Fields) != 0 || raw.PayloadRetained || raw.SourceID != changed.SourceID {
		t.Fatalf("remote publication lost its observation or duplicated its payload: %+v %v", raw, err)
	}
	changes, err := db.RunChanges(ctx, delta.ID, model.ListOptions{})
	if err != nil || changes.Counts != (model.PublicationCounts{Updated: 1}) || len(changes.Items) != 1 {
		t.Fatalf("malformed date update lost semantic transition: %+v %v", changes, err)
	}
	if !reflect.DeepEqual(changes.Items[0].Before, original.Fields) || !reflect.DeepEqual(changes.Items[0].After, changed.Fields) {
		t.Fatalf("malformed dates changed semantic history: %+v", changes.Items[0])
	}
	filtered, err := db.ListTorrents(ctx, model.ListOptions{PublishedAfter: "2026-08-31T00:00:00Z"})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].SourceID != control.SourceID {
		t.Fatalf("invalid timestamp projection matched date bound: %+v %v", filtered, err)
	}
}

func TestSearchOffsetBoundsPreserveInstantAndFractions(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, provider := range []string{"one", "two"} {
		publishSharingRow(t, ctx, db, provider, "match", `{"published_at":"2026-08-31T01:00:00.600Z"}`, nil)
	}
	publishSharingRow(t, ctx, db, "one", "before", `{"published_at":"2026-08-31T01:00:00.500Z"}`, nil)
	publishSharingRow(t, ctx, db, "one", "after", `{"published_at":"2026-08-31T01:00:00.901Z"}`, nil)
	filters := model.SearchFilters{PublishedAfter: "2026-09-01T00:00:00.500123456+23:00", PublishedBefore: "2026-08-30T02:00:00.900987654-23:00"}
	live, err := db.ListTorrents(ctx, filters.ListOptions())
	if err != nil || live.Total != 2 || len(live.Items) != 2 {
		t.Fatalf("accepted offset bounds failed live query: %+v %v", live, err)
	}
	for _, row := range live.Items {
		if row.SourceID != "match" {
			t.Fatalf("date fractions changed query boundary: %+v", live.Items)
		}
	}
	if live.Items[0].OccurrenceID == live.Items[1].OccurrenceID || live.Items[0].ProviderID == live.Items[1].ProviderID {
		t.Fatal("date query merged provider occurrences")
	}
	normalized, err := NormalizeSearchFilters(filters)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.PublishedAfter != "2026-08-31T01:00:00.500123456Z" || normalized.PublishedBefore != "2026-08-31T01:00:00.900987654Z" {
		t.Fatalf("search lost canonical instant or nanoseconds: %+v", normalized)
	}
	roundtrip, err := db.ListTorrents(ctx, normalized.ListOptions())
	if err != nil || !reflect.DeepEqual(roundtrip.Items, live.Items) {
		t.Fatalf("normalized search changed semantics: %+v %v", roundtrip, err)
	}
}
