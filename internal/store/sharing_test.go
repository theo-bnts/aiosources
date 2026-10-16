package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

// Partial-schema publication tests already install unrelated access-control
// columns needed by the current reader. These columns do not alter the catalog
// identity/publication migration being exercised, and those fixtures are never
// passed through the complete migration sequence.
const testShareFormatColumns = `ALTER TABLE ingest.shares ADD COLUMN format TEXT NOT NULL DEFAULT 'catalog', ADD COLUMN media_types TEXT[] NOT NULL DEFAULT '{}';`

// Historical upgrade fixtures must be written in their original schema, rather
// than by a current writer which depends on columns added in later migrations.
func legacyShareFixture(t *testing.T, ctx context.Context, db *Store, input model.ShareInput) model.ShareCredential {
	t.Helper()
	id, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	password, hash, err := sharingPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	if input.SourceIDs == nil {
		input.SourceIDs = []string{}
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.shares(id,name,enabled,scope,source_ids,fields,password_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, input.Name, input.Enabled, input.Scope, input.SourceIDs, input.Fields, hash); err != nil {
		t.Fatal(err)
	}
	return model.ShareCredential{Share: model.Share{ID: id, Revision: 1}, Password: password}
}

func legacyCatalogPageFixture(t *testing.T, ctx context.Context, db *Store, grant model.ShareCredential, q CatalogPageRequest) model.CatalogEnvelope {
	t.Helper()
	id, err := newRunID()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(CatalogDownloadTimeout)
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.share_requests(id,share_id,requested_at,lease_until) VALUES($1,$2,clock_timestamp(),$3)`, id, grant.Share.ID, deadline.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	d := &CatalogDownload{store: db, id: id, shareID: grant.Share.ID, revision: grant.Share.Revision, format: "catalog", deadline: deadline}
	defer d.Close()
	v, err := d.Page(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sharingDatabase(t testing.TB) (context.Context, *Store) {
	t.Helper()
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

func publishSharingRow(t *testing.T, ctx context.Context, db *Store, provider, source, fields string, origin *model.CatalogOrigin) {
	t.Helper()
	var encoded any
	if origin != nil {
		data, err := json.Marshal(origin)
		if err != nil {
			t.Fatal(err)
		}
		encoded = data
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields,origin) VALUES($1,$2,$3::jsonb,$4::jsonb) ON CONFLICT(provider_id,source_id) DO UPDATE SET fields=excluded.fields,origin=excluded.origin`, provider, source, fields, encoded); err != nil {
		t.Fatal(err)
	}
}

func sharingGrant(t *testing.T, ctx context.Context, db *Store, sources ...string) model.ShareCredential {
	t.Helper()
	input := model.ShareInput{Name: "Test friend", Enabled: true, Scope: "selected", SourceIDs: sources, Fields: CatalogFields()}
	if len(sources) == 0 {
		input.Scope = "all"
	}
	grant, err := db.CreateShare(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func sharingPage(t *testing.T, ctx context.Context, db *Store, grant model.ShareCredential, request CatalogPageRequest) model.CatalogEnvelope {
	t.Helper()
	page, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, request)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestCatalogProjectionPreservesSafeValuesAndRejectsPrivateStructures(t *testing.T) {
	fields, err := projectCatalogFields([]byte(`{"title":"Unmodified title","size":9007199254740993,"seeders":null,"category":["a",2,false,null],"categories":[{"url":"private"}],"published_at":{"secret":"private"},"info_hash":"exact-hash","download_url":"private","raw_id":24,"magnet":"private"}`), append(CatalogFields(), "download_url", "raw_id", "magnet"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "Unmodified title", "size": json.Number("9007199254740993"), "category": []any{"a", json.Number("2"), false, nil}, "info_hash": "exact-hash"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("unsafe or modified projection: %#v", fields)
	}
}

func TestCatalogSnapshotRetriesAndMutationDelta(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "allowed", "a", `{"title":"original a","size":9007199254740993,"download_url":"private"}`, nil)
	publishSharingRow(t, ctx, db, "allowed", "b", `{"title":"original b"}`, nil)
	publishSharingRow(t, ctx, db, "private", "x", `{"title":"not authorized"}`, nil)
	grant := sharingGrant(t, ctx, db, "allowed")
	first := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	if first.Mode != "full" || len(first.Items) != 1 || first.Items[0].Fields["title"] != "original a" || first.NextCursor == "" || first.Checkpoint != "" || first.Items[0].Fields["size"] != json.Number("9007199254740993") {
		t.Fatalf("first snapshot: %+v", first)
	}
	publishSharingRow(t, ctx, db, "allowed", "a", `{"title":"updated a"}`, nil)
	publishSharingRow(t, ctx, db, "allowed", "c", `{"title":"new c"}`, nil)
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.torrents WHERE provider_id='allowed' AND source_id='b'`); err != nil {
		t.Fatal(err)
	}
	secondRequest := CatalogPageRequest{Cursor: first.NextCursor}
	second := sharingPage(t, ctx, db, grant, secondRequest)
	if len(second.Items) != 1 || second.Items[0].Deleted || second.Items[0].Fields["title"] != "original b" || second.NextCursor != "" || second.Checkpoint == "" {
		t.Fatalf("snapshot changed beneath continuation: %+v", second)
	}
	publishSharingRow(t, ctx, db, "allowed", "b", `{"title":"restored b"}`, nil)
	retry := sharingPage(t, ctx, db, grant, secondRequest)
	if !reflect.DeepEqual(second, retry) {
		t.Fatalf("same cursor was not stable: %+v / %+v", second, retry)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.torrents WHERE provider_id='allowed' AND source_id='b'`); err != nil {
		t.Fatal(err)
	}
	var delta []model.CatalogItem
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1, Checkpoint: second.Checkpoint})
	for {
		if page.Mode != "incremental" {
			t.Fatalf("unexpected delta mode: %+v", page)
		}
		delta = append(delta, page.Items...)
		if page.NextCursor == "" {
			break
		}
		page = sharingPage(t, ctx, db, grant, CatalogPageRequest{Cursor: page.NextCursor})
	}
	byID := make(map[string]model.CatalogItem, len(delta))
	hasNewRecord := false
	for _, item := range delta {
		byID[item.ID] = item
		hasNewRecord = hasNewRecord || (!item.Deleted && item.Fields["title"] == "new c")
	}
	if len(delta) != 3 || byID[first.Items[0].ID].Fields["title"] != "updated a" || !byID[second.Items[0].ID].Deleted || byID[second.Items[0].ID].Fields != nil || !hasNewRecord {
		t.Fatalf("delta lost updates, tombstones or isolation: %+v", delta)
	}
	empty := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: page.Checkpoint})
	if empty.Mode != "incremental" || len(empty.Items) != 0 || empty.Checkpoint == "" {
		t.Fatalf("empty delta did not advance safely: %+v", empty)
	}
	all := sharingGrant(t, ctx, db)
	baseline := sharingPage(t, ctx, db, all, CatalogPageRequest{})
	publishSharingRow(t, ctx, db, "future", "new", `{"title":"future provider"}`, nil)
	future := sharingPage(t, ctx, db, all, CatalogPageRequest{Checkpoint: baseline.Checkpoint})
	if len(future.Items) != 1 || future.Items[0].Origin.ProviderID != "future" {
		t.Fatalf("all scope excluded a future provider: %+v", future)
	}
}

func TestCatalogPermissionsRotationRevocationAndCursorIsolation(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "one", "a", `{"title":"one"}`, nil)
	publishSharingRow(t, ctx, db, "two", "b", `{"title":"two"}`, nil)
	grant := sharingGrant(t, ctx, db, "one", "two")
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	baseline := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	other := sharingGrant(t, ctx, db)
	for _, request := range []CatalogPageRequest{{Cursor: page.NextCursor, Limit: 2}, {Cursor: page.NextCursor + "x"}, {Cursor: baseline.Checkpoint}, {Checkpoint: page.NextCursor}, {Cursor: "malformed"}, {Cursor: page.NextCursor, Checkpoint: baseline.Checkpoint}} {
		if _, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, request); !errors.Is(err, ErrCatalogCursor) {
			t.Fatalf("unsafe continuation accepted: %+v, %v", request, err)
		}
	}
	for _, request := range []CatalogPageRequest{{Cursor: page.NextCursor}, {Checkpoint: baseline.Checkpoint}} {
		if _, err := db.CatalogPage(ctx, other.Share.ID, other.Password, request); !errors.Is(err, ErrCatalogCursor) {
			t.Fatalf("cross-share token accepted: %v", err)
		}
	}
	input := model.ShareInput{Name: grant.Share.Name, Enabled: true, Scope: "selected", SourceIDs: []string{"one"}, Fields: []string{"title"}, Revision: grant.Share.Revision, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads}
	updated, err := db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{Cursor: page.NextCursor}); !errors.Is(err, ErrCatalogCursor) {
		t.Fatalf("old page escaped current permissions: %v", err)
	}
	reset := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: baseline.Checkpoint})
	if reset.Mode != "full" || len(reset.Items) != 1 || reset.Items[0].Origin.ProviderID != "one" {
		t.Fatalf("permission narrowing was not authoritative: %+v", reset)
	}
	rotated, err := db.RotateShare(ctx, grant.Share.ID, updated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("rotated password remained usable: %v", err)
	}
	sharingPage(t, ctx, db, rotated, CatalogPageRequest{})
	input.Enabled, input.Revision = false, rotated.Share.Revision
	disabled, err := db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogPage(ctx, rotated.Share.ID, rotated.Password, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("disabled sharing remained accessible: %v", err)
	}
	input.Enabled, input.Revision = true, disabled.Revision
	enabled, err := db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteShare(ctx, grant.Share.ID, enabled.Revision-1); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale revision revoked new configuration: %v", err)
	}
	if err := db.DeleteShare(ctx, grant.Share.ID, enabled.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogPage(ctx, rotated.Share.ID, rotated.Password, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("revoked sharing remained accessible: %v", err)
	}
}

func TestCatalogDuplicateOriginsAndIdentityChanges(t *testing.T) {
	ctx, db := sharingDatabase(t)
	origin := &model.CatalogOrigin{InstanceID: "original-instance", ProviderID: "original-provider", SourceID: "original-record"}
	publishSharingRow(t, ctx, db, "copy-one", "local-one", `{"title":"first copy"}`, origin)
	publishSharingRow(t, ctx, db, "copy-two", "local-two", `{"title":"second copy"}`, origin)
	grant := sharingGrant(t, ctx, db)
	first := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if len(first.Items) != 1 || first.Items[0].Origin != *origin || first.Items[0].ID != model.CatalogItemID(*origin) || first.Items[0].Fields["title"] != "second copy" {
		t.Fatalf("original identity was not deduplicated faithfully: %+v", first)
	}
	if _, err := db.pool.Exec(ctx, `DELETE FROM ingest.torrents WHERE provider_id='copy-two'`); err != nil {
		t.Fatal(err)
	}
	remaining := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: first.Checkpoint})
	if len(remaining.Items) != 1 || remaining.Items[0].Deleted || remaining.Items[0].Fields["title"] != "first copy" {
		t.Fatalf("one copy's deletion erased a remaining copy: %+v", remaining)
	}
	newOrigin := &model.CatalogOrigin{InstanceID: "original-instance", ProviderID: "original-provider", SourceID: "replacement-record"}
	publishSharingRow(t, ctx, db, "copy-one", "local-one", `{"title":"replacement"}`, newOrigin)
	changed := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: remaining.Checkpoint})
	if len(changed.Items) != 2 || changed.Items[0].Origin != *origin || !changed.Items[0].Deleted || changed.Items[1].Origin != *newOrigin || changed.Items[1].Deleted {
		t.Fatalf("identity replacement lost its tombstone: %+v", changed)
	}
}

func TestCatalogPublicationLockRollbackAndObservationOnlyUpdates(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := sharingGrant(t, ctx, db)
	baseline := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	first, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(first)
	if _, err := first.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('one','a','{"title":"first"}')`); err != nil {
		t.Fatal(err)
	}
	// Different rows cannot block each other via row locks. A second writer
	// must wait before allocating/publishing a later journal sequence.
	waiting, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_, err = db.pool.Exec(waiting, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('two','b','{"title":"second"}')`)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later publication overtook an uncommitted transaction: %v", err)
	}
	inflight := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: baseline.Checkpoint})
	if len(inflight.Items) != 0 {
		t.Fatalf("uncommitted publication leaked: %+v", inflight)
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "two", "b", `{"title":"second"}`, nil)
	committed := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: inflight.Checkpoint})
	if len(committed.Items) != 2 {
		t.Fatalf("checkpoint skipped a late commit: %+v", committed)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.torrents SET last_seen_at=NOW(),historical=TRUE WHERE provider_id='one'`); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rolledBack.Exec(ctx, `DELETE FROM ingest.torrents`); err != nil {
		rollback(rolledBack)
		t.Fatal(err)
	}
	rollback(rolledBack)
	unchanged := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: committed.Checkpoint})
	if len(unchanged.Items) != 0 || unchanged.Checkpoint != committed.Checkpoint {
		t.Fatalf("observations or rollback changed publication: %+v", unchanged)
	}
}

func TestCatalogMigrationSeedsExistingRowsAndDurableIdentity(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.pool.Exec(ctx, "BEGIN; CREATE SCHEMA ingest;"+initialSchema+activitySchema+`INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('legacy','row','{"title":"existing"}');`+sharingSchema+catalogIdentityPrivacySchema+securitySchema+shareControlsSchema+catalogRetentionStateSchema+testShareFormatColumns+"COMMIT;"); err != nil {
		t.Fatal(err)
	}
	grant := sharingGrant(t, ctx, db)
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if len(page.Items) != 1 || page.Items[0].Fields["title"] != "existing" || page.Items[0].Origin.ProviderID != "legacy" || page.Items[0].Origin.SourceID == "" || page.Items[0].Origin.SourceID == "row" {
		t.Fatalf("existing catalogue absent from first snapshot: %+v", page)
	}
	other, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	identity, err := other.CatalogInstanceID(ctx)
	if err != nil || identity != page.InstanceID || identity != page.Items[0].Origin.InstanceID {
		t.Fatalf("installation identity was not durable: %q %v", identity, err)
	}
	resumed := sharingPage(t, ctx, other, grant, CatalogPageRequest{Checkpoint: page.Checkpoint})
	if resumed.Mode != "incremental" || len(resumed.Items) != 0 {
		t.Fatalf("signing identity did not survive new store: %+v", resumed)
	}
	items, err := db.Shares(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(items)
	if err != nil || strings.Contains(string(encoded), grant.Password) || strings.Contains(string(encoded), "password_hash") {
		t.Fatalf("listing disclosed credential material: %s %v", encoded, err)
	}
}

func TestCatalogLongImportedIdentityHasBoundedResumableCursor(t *testing.T) {
	ctx, db := sharingDatabase(t)
	origin := &model.CatalogOrigin{InstanceID: "remote-instance", ProviderID: "remote-source", SourceID: strings.Repeat("large-source-id-", 1000)}
	publishSharingRow(t, ctx, db, "remote", "hashed-local-one", `{"title":"long original identity"}`, origin)
	publishSharingRow(t, ctx, db, "local", "next", `{"title":"second record"}`, nil)
	grant := sharingGrant(t, ctx, db)
	first := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	if len(first.Items) != 1 || first.Items[0].Origin != *origin || first.NextCursor == "" || len(first.NextCursor) > 2048 {
		t.Fatal("long original identity was changed or made continuation unbounded")
	}
	last := sharingPage(t, ctx, db, grant, CatalogPageRequest{Cursor: first.NextCursor})
	if len(last.Items) != 1 || last.Items[0].Fields["title"] != "second record" || last.Checkpoint == "" {
		t.Fatalf("long original identity prevented resuming: %+v", last)
	}
}

func TestCatalogNativeSourceIdentifiersDoNotDiscloseCredentials(t *testing.T) {
	ctx, db := sharingDatabase(t)
	source := "https://tracker.invalid/download?passkey=private-passkey&torrent=one"
	publishSharingRow(t, ctx, db, "allowed", source, `{"title":"first"}`, nil)
	publishSharingRow(t, ctx, db, "allowed", source+"&second=true", `{"title":"second"}`, nil)
	grant := sharingGrant(t, ctx, db, "allowed")
	snapshot := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "passkey") || strings.Contains(string(encoded), "tracker.invalid") {
		t.Fatal("native source identity disclosed a credential-bearing URL")
	}
	if len(snapshot.Items) != 2 || snapshot.Items[0].ID == snapshot.Items[1].ID || snapshot.Items[0].Origin.SourceID == "" {
		t.Fatalf("opaque native identities are not distinct: %+v", snapshot)
	}
	original := snapshot.Items[0]
	publishSharingRow(t, ctx, db, "allowed", source, `{"title":"updated"}`, nil)
	updated := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: snapshot.Checkpoint})
	if len(updated.Items) != 1 || updated.Items[0].ID != original.ID || updated.Items[0].Origin != original.Origin || updated.Items[0].Fields["title"] != "updated" {
		t.Fatalf("native identity changed across an update: %+v", updated)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM ingest.torrents WHERE provider_id='allowed' AND source_id=$1", source); err != nil {
		t.Fatal(err)
	}
	deleted := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: updated.Checkpoint})
	if len(deleted.Items) != 1 || deleted.Items[0].ID != original.ID || deleted.Items[0].Origin != original.Origin || !deleted.Items[0].Deleted {
		t.Fatalf("opaque deletion identity changed: %+v", deleted)
	}
}

func TestCatalogIdentityPrivacyUpgradeResetsExistingReaders(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.pool.Exec(ctx, "CREATE SCHEMA ingest;"+initialSchema+activitySchema+sharingSchema+securitySchema+shareControlsSchema+catalogRetentionStateSchema+testShareFormatColumns); err != nil {
		t.Fatal(err)
	}
	source := "https://tracker.invalid/file?passkey=private-passkey"
	publishSharingRow(t, ctx, db, "native", source, `{"title":"native record"}`, nil)
	imported := &model.CatalogOrigin{InstanceID: "remote", ProviderID: "remote-provider", SourceID: "opaque-remote-id"}
	publishSharingRow(t, ctx, db, "mirror", "local-id", `{"title":"imported record"}`, imported)
	grant := sharingGrant(t, ctx, db)
	oldPage := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	oldSnapshot := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if _, err := db.pool.Exec(ctx, "BEGIN;"+catalogIdentityPrivacySchema+"COMMIT;"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{Cursor: oldPage.NextCursor}); !errors.Is(err, ErrCatalogCursor) {
		t.Fatalf("old page mixed identity schemes: %v", err)
	}
	reset := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: oldSnapshot.Checkpoint})
	encoded, err := json.Marshal(reset)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Mode != "full" || len(reset.Items) != 2 || strings.Contains(string(encoded), "passkey") || reset.Items[0].ID == oldSnapshot.Items[0].ID || reset.Items[1].Origin != *imported {
		t.Fatalf("privacy upgrade failed to reset native identity while preserving imports: %+v", reset)
	}
	publishSharingRow(t, ctx, db, "native", source, `{"title":"updated native"}`, nil)
	delta := sharingPage(t, ctx, db, grant, CatalogPageRequest{Checkpoint: reset.Checkpoint})
	if len(delta.Items) != 1 || delta.Items[0].ID != reset.Items[0].ID || delta.Items[0].Fields["title"] != "updated native" {
		t.Fatalf("post-upgrade native identity was not stable: %+v", delta)
	}
	if _, err := db.pool.Exec(ctx, "DELETE FROM ingest.catalog_journal"); err == nil {
		t.Fatal("privacy migration left history mutable")
	}
}
