package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestPublicCatalogGatesScopesContinuationsAndNoVisitorRows(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES
 ('alpha','https://source.invalid/item?apikey=private-native-key','{"title":"Example Alpha","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":100,"download_url":"https://source.invalid/private-download?passkey=never-export","magnet":"magnet:?tr=https://private.invalid/announce"}'),
 ('beta','two','{"title":"Example Beta","info_hash":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":200}')`); err != nil {
		t.Fatal(err)
	}
	access, err := db.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	grant := model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration}
	if _, _, err = db.PublicCatalogPage(ctx, grant, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("disabled public export admitted", err)
	}
	access.Enabled = true
	access.CatalogEnabled = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	var tracked int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal`).Scan(&tracked); err != nil || tracked != 0 {
		t.Fatal("source exposure was not required for tracking", tracked, err)
	}
	access, _ = db.PublicAccess(ctx)
	access.ExposeSources = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	grant.Generation, grant.CatalogGeneration = access.Generation, access.CatalogGeneration
	first, revision, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" || first.Checkpoint != "" {
		t.Fatal("initial public page", err)
	}
	second, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 1 || second.Checkpoint == "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("public continuation lost or duplicated a record", err)
	}
	raw, _ := json.Marshal([]model.CatalogEnvelope{first, second})
	for _, sensitive := range []string{"private-native-key", "private-download", "passkey", "magnet:", "private.invalid"} {
		if strings.Contains(string(raw), sensitive) {
			t.Fatal("public export disclosed sensitive input")
		}
	}
	var saved int
	if err = db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.shares)+(SELECT count(*) FROM ingest.share_requests)`).Scan(&saved); err != nil || saved != 0 {
		t.Fatal("visitor profiles were stored", saved, err)
	}
	if err = db.ValidatePublicCatalog(ctx, grant, revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.torrents SET fields=jsonb_set(fields,'{title}','"Example Alpha Updated"') WHERE provider_id='alpha'`); err != nil {
		t.Fatal(err)
	}
	delta, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Checkpoint: second.Checkpoint})
	if err != nil || delta.Mode != "incremental" || len(delta.Items) != 1 || delta.Items[0].Fields["title"] != "Example Alpha Updated" {
		t.Fatal("public delta", err)
	}
	policy, _ := db.PublicCatalogPolicy(ctx)
	policy.Sources = []string{"beta"}
	policy.Fields = []string{"title"}
	policy.CatalogEntities = model.CatalogEntities{}
	if err = db.SetPublicCatalogPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	if err = db.ValidatePublicCatalog(ctx, grant, revision); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("old encoded response outlived a permission change", err)
	}
	refreshed, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Checkpoint: delta.Checkpoint})
	if err != nil || refreshed.Mode != "full" || len(refreshed.Items) != 1 || refreshed.Items[0].Origin.ProviderID != "beta" || len(refreshed.Items[0].Fields) != 1 {
		t.Fatal("changed ceiling did not require an authoritative restricted full", err)
	}
	restricted := grant
	restricted.Sources = []string{"alpha"}
	if _, _, err = db.PublicCatalogPage(ctx, restricted, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("an empty intersection broadened access", err)
	}
	access, _ = db.PublicAccess(ctx)
	access.ExposeSources = false
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.PublicCatalogPage(ctx, grant, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("withdrawing source exposure did not revoke", err)
	}
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.catalog_journal`).Scan(&tracked); err != nil || tracked != 0 {
		t.Fatal("inactive public sharing retained snapshots", tracked, err)
	}
	access, _ = db.PublicAccess(ctx)
	access.ExposeSources = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.PublicCatalogPage(ctx, grant, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("re-enabling restored a revoked link", err)
	}
}

func TestPublicSharingDataUsesExistingSafeProjectionsAndSeedsMetadata(t *testing.T) {
	ctx, db := sharingDatabase(t)
	fixture := catalogEntityFixture(t, ctx, db)
	if err := db.DeleteShare(ctx, fixture.Share.ID, fixture.Share.Revision); err != nil {
		t.Fatal(err)
	}
	// No named recipient remains to keep the metadata change index alive.
	access, _ := db.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	all := model.CatalogEntities{Metadata: true, Availability: true, Identifiers: true, Languages: true, Technical: true, Files: true}
	grant := model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration, PublicCatalogOptions: model.PublicCatalogOptions{CatalogEntities: all}}
	request := CatalogPageRequest{Limit: 1, Entities: all}
	var metadata, availability, items int
	var pages []model.CatalogEnvelope
	for range 20 {
		page, _, err := db.PublicCatalogPage(ctx, grant, request)
		if err != nil {
			t.Fatal(err)
		}
		if page.Version != 3 || page.Entities != all || !page.Supplement.Valid(all, time.Now()) {
			t.Fatal("public supplement contract changed")
		}
		metadata += len(page.Supplement.Metadata)
		pages = append(pages, page)
		availability += len(page.Supplement.Availability)
		items += len(page.Items)
		body, _ := json.Marshal(page)
		for _, secret := range []string{"must-not-travel", "private.invalid/download", "\"id\":555", "\"link\""} {
			if strings.Contains(string(body), secret) {
				t.Fatal("private provider data exported")
			}
		}
		if page.Checkpoint != "" {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("public supplement lost continuation")
		}
		request.Cursor = page.NextCursor
	}
	if metadata != 1 || availability != 1 || items != 2 {
		t.Fatal("public export lost existing metadata or cache observations", metadata, availability, items)
	}
	if pages[len(pages)-1].Checkpoint == "" {
		t.Fatal("public traversal did not complete")
	}
	// The ordinary remote-import path consumes public envelopes unchanged and
	// publishes their optional data only after the complete import succeeds.
	receiverContext, receiver := sharingDatabase(t)
	for _, id := range []string{"public-copy", "other-copy"} {
		provider := mirrorSource(id)
		provider.CatalogEntities = all
		run := mirrorClaim(t, receiverContext, receiver, provider, model.ModeFull)
		for _, page := range pages {
			run = applyCatalogEntityPage(t, receiverContext, receiver, run, page)
		}
		mirrorFinish(t, receiverContext, receiver, run, model.StatusSucceeded)
		if works, cache := catalogEntityCounts(t, receiverContext, receiver); works != 1 || cache != 1 {
			t.Fatal("public import lost or duplicated supplemental data", works, cache)
		}
	}
	reexport, err := receiver.CreateShare(receiverContext, model.ShareInput{Name: "Receiver catalog", Enabled: true, Scope: "all", Fields: CatalogFields()})
	if err != nil {
		t.Fatal(err)
	}
	copyPage, err := receiver.CatalogPage(receiverContext, reexport.Share.ID, reexport.Password, CatalogPageRequest{})
	if err != nil || len(copyPage.Items) != 2 {
		t.Fatal("public copies lost their canonical origins", err)
	}
	// Receiver cannot request data the visitor left out of the issued capability.
	grant.CatalogEntities = model.CatalogEntities{}
	page, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Entities: all})
	if err != nil || !page.Entities.Empty() || !page.Supplement.Valid(model.CatalogEntities{}, time.Now()) {
		t.Fatal("receiver widened the issued capability", err)
	}
}

func TestPublicSharingResetRequiresAuthoritativeFullWithoutRevivingOldRows(t *testing.T) {
	ctx, db := sharingDatabase(t)
	access, _ := db.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	publishSharingRow(t, ctx, db, "example", "one", `{"title":"Example release"}`, nil)
	access, _ = db.PublicAccess(ctx)
	grant := model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration}
	before, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{})
	if err != nil || len(before.Items) != 1 || before.Checkpoint == "" {
		t.Fatal("initial export", err)
	}
	settings, err := db.StorageOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ResetSourceData(ctx, settings.Settings.Revision); err != nil {
		t.Fatal(err)
	}
	after, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{Checkpoint: before.Checkpoint})
	if err != nil || after.Mode != "full" || len(after.Items) != 0 || after.Checkpoint == "" {
		t.Fatal("reset retained old public rows", err)
	}
}

func TestPublicSharingSelectedSourceDeletionNeverBecomesAllSources(t *testing.T) {
	ctx, db := sharingDatabase(t)
	policy, _ := db.PublicCatalogPolicy(ctx)
	policy.Sources = []string{"removed"}
	if err := db.SetPublicCatalogPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	access, _ := db.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	grant := model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration}
	if err := db.DeleteSourceData(ctx, "removed", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.PublicCatalogPage(ctx, grant, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatal("removed last allowed source widened export", err)
	}
	current, _ := db.PublicAccess(ctx)
	if current.CatalogEnabled || !current.Enabled || !current.ExposeSources {
		t.Fatal("source deletion did not revoke only catalog access")
	}
	if err := db.SetPublicAccess(ctx, access); !errors.Is(err, model.ErrConflict) {
		t.Fatal("an old settings form restored unrestricted catalog sharing", err)
	}
}

func TestPublicSharingByteBudgetKeepsAllContinuationRows(t *testing.T) {
	ctx, db := sharingDatabase(t)
	access, _ := db.PublicAccess(ctx)
	access.Enabled, access.ExposeSources, access.CatalogEnabled = true, true, true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two", "three", "four"} {
		if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('example',$1,jsonb_build_object('title',$2::text))`, id, strings.Repeat("A", 2<<20)); err != nil {
			t.Fatal(err)
		}
	}
	access, _ = db.PublicAccess(ctx)
	grant := model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration}
	request := CatalogPageRequest{Limit: 1000}
	seen := map[string]bool{}
	pages := 0
	for range 5 {
		page, _, err := db.PublicCatalogPage(ctx, grant, request)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		body, _ := json.Marshal(page)
		if len(body) > 7<<20 {
			t.Fatal("public page exceeded its byte budget")
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("byte boundary duplicated a row")
			}
			seen[item.ID] = true
		}
		if page.Checkpoint != "" {
			break
		}
		request.Cursor = page.NextCursor
	}
	if pages != 2 || len(seen) != 4 {
		t.Fatal("byte boundary lost rows", pages, len(seen))
	}
}

func TestPublicCatalogOriginsMatchNamedCatalogs(t *testing.T) {
	ctx, db := sharingDatabase(t)
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.torrents(provider_id,source_id,fields) VALUES('alpha','one','{"title":"Example release","info_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`); err != nil {
		t.Fatal(err)
	}
	access, _ := db.PublicAccess(ctx)
	access.Enabled = true
	access.ExposeSources = true
	access.CatalogEnabled = true
	if err := db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	access, _ = db.PublicAccess(ctx)
	public, _, err := db.PublicCatalogPage(ctx, model.PublicCatalogGrant{Generation: access.Generation, CatalogGeneration: access.CatalogGeneration}, CatalogPageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	named, err := db.CreateShare(ctx, model.ShareInput{Name: "Named catalog", Enabled: true, Scope: "all", Fields: CatalogFields()})
	if err != nil {
		t.Fatal(err)
	}
	private, err := db.CatalogPage(ctx, named.Share.ID, named.Password, CatalogPageRequest{})
	if err != nil || len(public.Items) != 1 || len(private.Items) != 1 || public.Items[0].ID != private.Items[0].ID || public.Items[0].Origin != private.Items[0].Origin {
		t.Fatal("public and named shares created different identities", err)
	}
	access, _ = db.PublicAccess(ctx)
	access.CatalogEnabled = false
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	private, err = db.CatalogPage(ctx, named.Share.ID, named.Password, CatalogPageRequest{})
	if err != nil || len(private.Items) != 1 {
		t.Fatal("disabling public sharing cleared a named catalog", err)
	}
}
