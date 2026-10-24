package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

var detailedCatalogEntities = model.CatalogEntities{Identifiers: true, Languages: true, Technical: true, Files: true}

func TestCatalogDetailsKeepIdentifierConflictSemantics(t *testing.T) {
	for _, raw := range []string{
		`{"tmdb_id":[["101"]],"tmdb":"202"}`,
		`{"imdb_id":"null","external_ids":{"imdb_id":"TT0000101"}}`,
		`{"tmdb_id":9007199254740993,"attributes":{"tmdbid":"9007199254740993"}}`,
		`{"tvdb_id":[null,""],"tvdb":"202"}`,
		`{"tmdb_id":{"secret":"must-not-travel"},"tmdb":"202"}`,
		`{"season":[["1"]],"episode":["1","2"]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			fields, err := decodeCatalogFields([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			projected, _ := json.Marshal(model.ProjectCatalogDetails(fields, detailedCatalogEntities))
			native, err := scanAIORecord(aioFakeRow{raw})
			if err != nil {
				t.Fatal(err)
			}
			remote, err := scanAIORecord(aioFakeRow{string(projected)})
			if err != nil || !reflect.DeepEqual(native, remote) {
				t.Fatalf("sharing changed source evidence/confidence: native=%+v remote=%+v %v", native, remote, err)
			}
		})
	}
}

func detailedCatalogPages(t *testing.T, ctx context.Context, db *Store, grant model.ShareCredential, checkpoint string, selection model.CatalogEntities) []model.CatalogEnvelope {
	t.Helper()
	query := CatalogPageRequest{Limit: 1, Checkpoint: checkpoint, Entities: selection}
	var pages []model.CatalogEnvelope
	for range 30 {
		page, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, query)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
		if page.Checkpoint != "" {
			return pages
		}
		query.Checkpoint, query.Cursor = "", page.NextCursor
	}
	t.Fatal("detail traversal did not finish")
	return nil
}

func detailedCatalogGrant(t *testing.T, ctx context.Context, db *Store) model.ShareCredential {
	t.Helper()
	grant, err := db.CreateShare(ctx, model.ShareInput{Name: "Example recipient", Enabled: true, Scope: "selected", SourceIDs: []string{"source"}, Fields: CatalogFields(), CatalogEntities: detailedCatalogEntities})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestCatalogDetailsRetainMatchingAndPresentationWithoutPrivateValues(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	publishSharingRow(t, ctx, sender, "source", "one", `{"title":"Example.Series.S01E02.1080p","info_hash":"`+catalogTestHash+`","size":9007199254740993,"tmdb_id":"101","imdb_id":"null","external_ids":{"imdb_id":"tt0000101"},"season":"1","episode":"2","languages":["fr","en"],"resolution":"1080p","hdr":"HDR10","duration":2400,"bitrate":12345678,"file_name":"Example.mkv","attributes":{"tvdbid":["201","202"],"audiocodec":"AAC","audiochannels":"5.1","secret":"not-to-share"},"subtitles":[{"language":"fr","url":"https://private.invalid/file"}],"announce":"https://private.invalid/announce"}`, nil)
	grant := detailedCatalogGrant(t, ctx, sender)
	legacy, err := sender.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{})
	if err != nil || legacy.Version != 1 || len(legacy.Items[0].Details) != 0 {
		t.Fatal("old clients received optional facts", err)
	}
	pages := detailedCatalogPages(t, ctx, sender, grant, "", detailedCatalogEntities)
	ctx, receiver := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = detailedCatalogEntities
	run := mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range pages {
		data, _ := json.Marshal(p)
		if strings.Contains(string(data), "private.invalid") || strings.Contains(string(data), "not-to-share") || p.Version != 3 {
			t.Fatal("source credentials or incorrect protocol in export")
		}
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	native, err := sender.AIOTorrent(ctx, "source", "one")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := receiver.AIOTorrent(ctx, "remote", pages[0].Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	native.Provider, native.SourceID, native.Fingerprint = remote.Provider, remote.SourceID, remote.Fingerprint
	if !reflect.DeepEqual(native, remote) {
		t.Fatalf("matching/presentation facts changed: native=%+v remote=%+v", native, remote)
	}
}

func TestCatalogDetailOnlyUpdatesAreAtomicAndPermissionsRefreshTheMirror(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	publishSharingRow(t, ctx, sender, "source", "one", `{"title":"Example","size":123,"language":"fr","tmdb_id":"101"}`, nil)
	grant := detailedCatalogGrant(t, ctx, sender)
	pages := detailedCatalogPages(t, ctx, sender, grant, "", detailedCatalogEntities)
	ctx, receiver := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = detailedCatalogEntities
	apply := func(pages []model.CatalogEnvelope, status model.RunStatus) {
		t.Helper()
		run := mirrorClaim(t, ctx, receiver, provider, model.ModeIncremental)
		for _, p := range pages {
			run = applyCatalogEntityPage(t, ctx, receiver, run, p)
		}
		mirrorFinish(t, ctx, receiver, run, status)
	}
	apply(pages, model.StatusSucceeded)
	checkpoint := pages[len(pages)-1].Checkpoint
	if _, err := sender.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=(fields-'language')||'{"tmdb_id":"202","languages":["en"]}' WHERE provider_id='source'`); err != nil {
		t.Fatal(err)
	}
	delta := detailedCatalogPages(t, ctx, sender, grant, checkpoint, detailedCatalogEntities)
	count := 0
	for _, page := range delta {
		if page.Mode != "incremental" || len(page.Items) > 0 {
			t.Fatal("a detail-only change unnecessarily redownloaded torrents")
		}
		count += len(page.Supplement.Torrents)
	}
	if count != 1 {
		t.Fatal("detail change was missed or duplicated", count)
	}
	apply(delta, model.StatusCancelled)
	row, err := receiver.AIOTorrent(ctx, "remote", pages[0].Items[0].ID)
	if err != nil || row.TMDB != "101" {
		t.Fatal("cancelled details altered the live mirror", err)
	}
	apply(delta, model.StatusSucceeded)
	row, err = receiver.AIOTorrent(ctx, "remote", pages[0].Items[0].ID)
	if err != nil || row.TMDB != "202" || !reflect.DeepEqual(row.Languages, []string{"en"}) {
		t.Fatal("detail replacement lost current IDs/languages or retained removed language", row, err)
	}
	// A malformed/unrelated version cannot overwrite facts for a different base.
	for i := range delta {
		for j := range delta[i].Supplement.Torrents {
			delta[i].Supplement.Torrents[j].BaseFingerprint = strings.Repeat("a", 64)
			delta[i].Supplement.Torrents[j].Fields = map[string]any{"tmdb_id": "999"}
		}
	}
	apply(delta, model.StatusSucceeded)
	row, _ = receiver.AIOTorrent(ctx, "remote", pages[0].Items[0].ID)
	if row.TMDB != "202" {
		t.Fatal("detail delta crossed a base-version fence")
	}
	provider.CatalogEntities = model.CatalogEntities{Languages: true}
	fresh := detailedCatalogPages(t, ctx, sender, grant, checkpoint, provider.CatalogEntities)
	if fresh[0].Mode != "full" {
		t.Fatal("changed detail choices did not remove previously authorized facts")
	}
	apply(fresh, model.StatusSucceeded)
	row, err = receiver.AIOTorrent(ctx, "remote", pages[0].Items[0].ID)
	if err != nil || row.TMDB != "" || !reflect.DeepEqual(row.Languages, []string{"en"}) {
		t.Fatal("permission refresh retained deselected facts", row, err)
	}
}

func TestCatalogDetailsLateCommitAndBaseVersionChangesAreNotSkipped(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "source", "one", `{"title":"Example","language":"fr"}`, nil)
	grant := detailedCatalogGrant(t, ctx, db)
	first := detailedCatalogPages(t, ctx, db, grant, "", detailedCatalogEntities)
	writer, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(writer)
	if _, err = writer.Exec(ctx, `UPDATE ingest.torrents SET fields=fields||'{"language":"en"}' WHERE provider_id='source'`); err != nil {
		t.Fatal(err)
	}
	before := detailedCatalogPages(t, ctx, db, grant, first[len(first)-1].Checkpoint, detailedCatalogEntities)
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	after := detailedCatalogPages(t, ctx, db, grant, before[len(before)-1].Checkpoint, detailedCatalogEntities)
	count := 0
	for _, page := range after {
		for _, detail := range page.Supplement.Torrents {
			if detail.Fields["language"] != "en" {
				t.Fatal("late commit exported stale source facts")
			}
			count++
		}
	}
	if count != 1 {
		t.Fatal("late source-fact commit disappeared behind the checkpoint", count)
	}
	// When the basic version changes, core pages also carry its matching facts.
	// This covers a change between the journal and transaction watermarks.
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=fields||'{"title":"Updated Example","language":"de"}' WHERE provider_id='source'`); err != nil {
		t.Fatal(err)
	}
	changed := detailedCatalogPages(t, ctx, db, grant, after[len(after)-1].Checkpoint, detailedCatalogEntities)
	if len(changed[0].Items) != 1 || changed[0].Items[0].Fields["title"] != "Updated Example" || changed[0].Items[0].Details["language"] != "de" {
		t.Fatal("new basic torrent version lost its matching source information")
	}
}

func TestCatalogDetailsRespectBasicFieldWhitelistAndBoundedPages(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, id := range []string{"one", "two", "three"} {
		publishSharingRow(t, ctx, db, "source", id, `{"title":"Example","info_hash":"`+catalogTestHash+`","attributes":{"infohash":"must-not-bypass-whitelist","imdbid":"tt0000101"},"size":123}`, nil)
	}
	grant, err := db.CreateShare(ctx, model.ShareInput{Name: "Limited", Enabled: true, Scope: "selected", SourceIDs: []string{"source"}, Fields: []string{"title"}, CatalogEntities: detailedCatalogEntities})
	if err != nil {
		t.Fatal(err)
	}
	pages := detailedCatalogPages(t, ctx, db, grant, "", detailedCatalogEntities)
	count := 0
	for _, page := range pages {
		if len(page.Items) > 1 || len(page.Supplement.Torrents) > 1 {
			t.Fatal("detail enrichment changed the requested pagination bound")
		}
		for _, item := range page.Items {
			if len(item.Fields) != 1 || item.Fields["title"] != "Example" {
				t.Fatal("details bypassed the basic field whitelist")
			}
			data, _ := json.Marshal(item.Details)
			if strings.Contains(string(data), "hash") || strings.Contains(string(data), "size") {
				t.Fatal("details smuggled a separately restricted field")
			}
			count++
		}
	}
	if count != 3 {
		t.Fatal("detail pagination lost or duplicated torrents", count)
	}
}

func TestCatalogDetailsKeepAcknowledgementWhenOtherDataChoicesChange(t *testing.T) {
	ctx, db := sharingDatabase(t)
	// PostgreSQL acknowledgements are cluster-wide. An older in-flight writer
	// legitimately causes replay, even though this catalog has not changed.
	writer, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(writer)
	if _, err = writer.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "source", "one", `{"title":"Example","language":"fr"}`, nil)
	grant := detailedCatalogGrant(t, ctx, db)
	first := detailedCatalogPages(t, ctx, db, grant, "", detailedCatalogEntities)
	selection := detailedCatalogEntities
	selection.Metadata, selection.Availability = true, true
	delta := detailedCatalogPages(t, ctx, db, grant, first[len(first)-1].Checkpoint, selection)
	var key []byte
	if err = db.pool.QueryRow(ctx, `SELECT signing_key FROM ingest.catalog_identity WHERE singleton`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := parseCatalogToken(key, first[len(first)-1].Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	continuation, err := parseCatalogToken(key, delta[0].NextCursor)
	if err != nil || checkpoint.AuxHigh == 0 || continuation.DetailsLow != checkpoint.AuxHigh {
		t.Fatal("metadata/cache choices reset the previously acknowledged torrent-fact boundary", err)
	}
	for _, page := range delta {
		if page.Mode != "incremental" || len(page.Items) != 0 {
			t.Fatal("metadata/cache permission change unnecessarily downloaded torrents again")
		}
	}
}

func TestCatalogDetailBatchesKeepVersionFencesAndProviderIsolation(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = detailedCatalogEntities
	items := make([]model.CatalogItem, 105)
	updates := make([]model.CatalogTorrentDetails, len(items))
	for i := range items {
		origin := model.CatalogOrigin{InstanceID: "owner", ProviderID: "source", SourceID: strconv.Itoa(i)}
		base := map[string]any{"title": "Example"}
		items[i] = model.CatalogItem{ID: model.CatalogItemID(origin), Origin: origin, Fields: base, Details: map[string]any{"language": "fr"}}
		updates[i] = model.CatalogTorrentDetails{ID: items[i].ID, Origin: origin, BaseFingerprint: model.CatalogFieldsFingerprint(base), Fields: map[string]any{"language": "en"}}
	}
	apply := func(full bool) {
		t.Helper()
		mode := model.ModeIncremental
		if full {
			mode = model.ModeFull
		}
		run := mirrorClaim(t, ctx, db, provider, mode)
		for start := 0; start < len(items); start += 100 {
			end := min(start+100, len(items))
			page := model.CatalogEnvelope{Version: 3, InstanceID: "owner", Entities: detailedCatalogEntities, Mode: string(mode), Items: []model.CatalogItem{}, NextCursor: "next"}
			if full {
				page.Items = items[start:end]
			} else {
				page.Supplement.Torrents = updates[start:end]
			}
			if end == len(items) {
				page.NextCursor, page.Checkpoint = "", "completed-"+string(mode)
			}
			run = applyCatalogEntityPage(t, ctx, db, run, page)
		}
		mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	}
	apply(true)
	origin := items[0].Origin
	publishSharingRow(t, ctx, db, "unrelated", items[0].ID, `{"title":"Example","language":"fr"}`, &origin)
	updates[5].BaseFingerprint = strings.Repeat("a", 64)
	apply(false)
	var updated, unchanged, unrelated int
	err := db.pool.QueryRow(ctx, `SELECT
count(*) FILTER(WHERE provider_id='remote' AND fields->>'language'='en'),
count(*) FILTER(WHERE provider_id='remote' AND fields->>'language'='fr'),
count(*) FILTER(WHERE provider_id='unrelated' AND fields->>'language'='fr') FROM ingest.torrents`).Scan(&updated, &unchanged, &unrelated)
	if err != nil || updated != 104 || unchanged != 1 || unrelated != 1 {
		t.Fatalf("batch lost boundaries, version fences or provider isolation: %d %d %d %v", updated, unchanged, unrelated, err)
	}
}
