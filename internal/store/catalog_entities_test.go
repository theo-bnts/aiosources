package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

var allCatalogEntities = model.CatalogEntities{Metadata: true, Availability: true}

const catalogTestHash = "0123456789abcdef0123456789abcdef01234567"

func catalogEntityFixture(t *testing.T, ctx context.Context, db *Store) model.ShareCredential {
	t.Helper()
	if err := db.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", IMDb: "tt0000101", Titles: []string{"Example Film"}, DisplayTitle: "Example Film", Years: []int{2020}, LocalizedTitles: map[string]string{"fr": "Film exemple"}}, []string{"example film"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		publishSharingRow(t, ctx, db, "allowed", id, `{"title":"Example.Film.2020","info_hash":"`+catalogTestHash+`","size":123}`, nil)
	}
	if _, err := db.pool.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','verified' FROM ingest.torrents t CROSS JOIN ingest.aio_works w WHERE t.provider_id='allowed';DELETE FROM ingest.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: 1, Hash: catalogTestHash, Cached: true, CheckedAt: time.Now().Add(-time.Minute), Files: []model.AIOFile{{Path: "Example.mkv", Size: 123, ID: 555, Link: "https://private.invalid/download?key=must-not-travel"}}}); err != nil {
		t.Fatal(err)
	}
	grant, err := db.CreateShare(ctx, model.ShareInput{Name: "Example recipient", Enabled: true, Scope: "selected", SourceIDs: []string{"allowed"}, Fields: CatalogFields(), CatalogEntities: allCatalogEntities})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func catalogEntityPages(t *testing.T, ctx context.Context, db *Store, grant model.ShareCredential, checkpoint string) []model.CatalogEnvelope {
	t.Helper()
	pages := []model.CatalogEnvelope{}
	q := CatalogPageRequest{Limit: 1, Checkpoint: checkpoint, Entities: allCatalogEntities}
	for range 30 {
		page, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, q)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
		if page.Checkpoint != "" {
			return pages
		}
		q.Cursor, q.Checkpoint = page.NextCursor, ""
	}
	t.Fatal("shared data traversal did not finish")
	return nil
}

func applyCatalogEntityPage(t *testing.T, ctx context.Context, db *Store, run model.Run, page model.CatalogEnvelope) model.Run {
	t.Helper()
	var state model.CatalogCursor
	if err := json.Unmarshal(run.Cursor, &state); err != nil {
		t.Fatal(err)
	}
	state.Protocol, state.Entities, state.InstanceID, state.Mode = page.Version, page.Entities, page.InstanceID, page.Mode
	state.Cursor, state.Checkpoint, state.Done = page.NextCursor, page.Checkpoint, page.Checkpoint != ""
	items := []model.Record{}
	for _, item := range page.Items {
		origin := item.Origin
		items = append(items, model.Record{SourceID: item.ID, Fields: item.Fields, Origin: &origin, Deleted: item.Deleted})
	}
	cursor, _ := json.Marshal(state)
	updated, err := db.SavePage(ctx, run, model.Page{ContentType: "application/json", Body: []byte("validated envelope"), Items: items, CatalogSupplement: page.Supplement, Next: cursor, Done: state.Done, ResetStaging: run.Pages == 0 && page.Mode == "full", RequireUniqueIDs: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func catalogEntityCounts(t *testing.T, ctx context.Context, db *Store) (works, cache int) {
	t.Helper()
	if err := db.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM ingest.aio_works),(SELECT count(*) FROM ingest.aio_shared_availability)`).Scan(&works, &cache); err != nil {
		t.Fatal(err)
	}
	return
}

func TestCatalogEntitiesScopedVersionedAndIndependentlyUpdated(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, db)
	legacy := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if legacy.Version != 1 || !legacy.Entities.Empty() || len(legacy.Supplement.Metadata)+len(legacy.Supplement.Availability) != 0 {
		t.Fatal("old clients received new entities")
	}
	for _, p := range catalogEntityPages(t, ctx, db, grant, legacy.Checkpoint) {
		if p.Mode != "incremental" || len(p.Items) > 0 {
			t.Fatal("opting into additional data redownloaded torrents")
		}
	}
	pages := catalogEntityPages(t, ctx, db, grant, "")
	works, cache := 0, 0
	for _, p := range pages {
		works += len(p.Supplement.Metadata)
		cache += len(p.Supplement.Availability)
		b, _ := json.Marshal(p)
		for _, secret := range []string{"must-not-travel", "private.invalid", `"id":555`, "secret_ref", "announce"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("private provider data exported: %s", secret)
			}
		}
	}
	if works != 1 || cache != 1 {
		t.Fatalf("deduplication or scope: works=%d cache=%d", works, cache)
	}
	checkpoint := pages[len(pages)-1].Checkpoint
	// Cache changes arrive even when the torrent journal did not change.
	if err := db.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: 1, Hash: catalogTestHash, Cached: true, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Updated.mkv", Size: 123}}}); err != nil {
		t.Fatal(err)
	}
	delta := catalogEntityPages(t, ctx, db, grant, checkpoint)
	records, cache := 0, 0
	for _, p := range delta {
		records += len(p.Items)
		cache += len(p.Supplement.Availability)
	}
	if records != 0 || cache != 1 {
		t.Fatalf("independent cache delta: records=%d cache=%d", records, cache)
	}
	// Neither an unrelated hash nor a work outside the authorized source leaks.
	if err := db.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: 1, Hash: strings.Repeat("f", 40), Cached: true, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "Other.mkv", Size: 1}}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range catalogEntityPages(t, ctx, db, grant, delta[len(delta)-1].Checkpoint) {
		for _, a := range p.Supplement.Availability {
			if a.Hash != catalogTestHash {
				t.Fatal("unrelated cache data leaked")
			}
		}
	}
}

func TestCatalogEntitiesNewTorrentCanReusePreviouslyKnownCache(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, db)
	otherHash := strings.Repeat("b", 40)
	if err := db.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: 1, Hash: otherHash, Cached: true, CheckedAt: time.Now().Add(-time.Minute), Files: []model.AIOFile{{Path: "Another.mkv", Size: 456}}}); err != nil {
		t.Fatal(err)
	}
	first := catalogEntityPages(t, ctx, db, grant, "")
	publishSharingRow(t, ctx, db, "allowed", "new-record", `{"title":"Another.Example","info_hash":"`+otherHash+`"}`, nil)
	found := false
	for _, p := range catalogEntityPages(t, ctx, db, grant, first[len(first)-1].Checkpoint) {
		for _, a := range p.Supplement.Availability {
			if a.Hash == otherHash {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("new torrent membership lost an existing cache observation")
	}
}

func TestCatalogEntitiesPublishAtomicallyDeduplicateAndPreserveLocalWork(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, sender)
	pages := catalogEntityPages(t, ctx, sender, grant, "")
	ctx, receiver := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = allCatalogEntities
	run := mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range pages {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
		if w, c := catalogEntityCounts(t, ctx, receiver); w+c != 0 {
			t.Fatal("uncommitted shared data became visible")
		}
	}
	run = mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	if w, c := catalogEntityCounts(t, ctx, receiver); w != 1 || c != 1 {
		t.Fatalf("published data missing: %d %d", w, c)
	}
	// Two imports of the same TMDB identity use one metadata record.
	provider.ID = "another_remote"
	run = mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range pages {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	if w, c := catalogEntityCounts(t, ctx, receiver); w != 1 || c != 1 {
		t.Fatal("duplicate metadata or cache observations")
	}
	if err := receiver.SaveAIOWork(ctx, model.AIOWork{Type: "movie", TMDB: "101", Titles: []string{"Local authoritative title"}}, []string{"local authoritative title"}); err != nil {
		t.Fatal(err)
	}
	provider.ID = "remote"
	run = mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range pages {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	var title, owner string
	if err := receiver.pool.QueryRow(ctx, `SELECT display_title,catalog_import_source FROM ingest.aio_works`).Scan(&title, &owner); err != nil || title != "Local authoritative title" || owner != "" {
		t.Fatalf("local metadata replaced: %q %q %v", title, owner, err)
	}
	// Decisions are never imported: each receiver must identify its own records.
	var matches int
	if err := receiver.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.aio_matches`).Scan(&matches); err != nil || matches != 0 {
		t.Fatal("remote identification decisions were trusted")
	}
}

func TestCatalogEntitiesCancellationAndCacheResetCannotPublishStaleData(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, sender)
	pages := catalogEntityPages(t, ctx, sender, grant, "")
	for _, reset := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "cache reset"}[reset], func(t *testing.T) {
			ctx, receiver := sharingDatabase(t)
			provider := mirrorSource("remote")
			provider.CatalogEntities = allCatalogEntities
			run := mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
			for _, p := range pages {
				run = applyCatalogEntityPage(t, ctx, receiver, run, p)
			}
			status := model.StatusCancelled
			if reset {
				if err := receiver.ResetAIOCache(ctx); err != nil {
					t.Fatal(err)
				}
				status = model.StatusSucceeded
			}
			mirrorFinish(t, ctx, receiver, run, status)
			w, c := catalogEntityCounts(t, ctx, receiver)
			if c != 0 || (!reset && w != 0) || (reset && w != 1) {
				t.Fatalf("stale data published: %d %d", w, c)
			}
		})
	}
}

func TestCatalogEntitiesPrivateAndMalformedDataCannotEnterStaging(t *testing.T) {
	ctx, db := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = allCatalogEntities
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	invalid := model.CatalogSupplement{Availability: []model.CatalogCacheObservation{{Service: "realdebrid", Hash: catalogTestHash, CheckedAt: time.Now(), Files: []model.AIOFile{{Path: "file.mkv", Size: 1}}}}}
	_, err := db.SavePage(ctx, run, model.Page{Next: json.RawMessage(`{"catalog_version":1}`), CatalogSupplement: invalid}, "")
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("account-only proof accepted: %v", err)
	}
	var pages int
	if err = db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.pages WHERE run_id=$1`, run.ID).Scan(&pages); err != nil || pages != 0 {
		t.Fatal("invalid page advanced")
	}
}

func TestCatalogEntitiesLateCommitCannotFallBehindCheckpoint(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, db)
	first := catalogEntityPages(t, ctx, db, grant, "")
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_shared_availability SET checked_at=now(),files='[{"path":"Later.mkv","size":123}]' WHERE service='torbox' AND hash=$1`, catalogTestHash); err != nil {
		t.Fatal(err)
	}
	// An unrelated, newer committed transaction must not acknowledge the older
	// writer's uncommitted cache update.
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.settings(key,value) VALUES('catalog.fixture','value')`); err != nil {
		t.Fatal(err)
	}
	before := catalogEntityPages(t, ctx, db, grant, first[len(first)-1].Checkpoint)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	after := catalogEntityPages(t, ctx, db, grant, before[len(before)-1].Checkpoint)
	count := 0
	for _, p := range after {
		for _, a := range p.Supplement.Availability {
			count++
			if a.Files[0].Path != "Later.mkv" {
				t.Fatal("old observation exported")
			}
		}
	}
	if count != 1 {
		t.Fatal("a late committed cache observation was skipped")
	}
}

func TestCatalogEntitiesMetadataUpdatesAndRemoteDataAreNotForwarded(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, sender)
	first := catalogEntityPages(t, ctx, sender, grant, "")
	ctx, receiver := sharingDatabase(t)
	provider := mirrorSource("remote")
	provider.CatalogEntities = allCatalogEntities
	run := mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range first {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	// Enrich a translation without changing the torrent or its match revision.
	if _, err := sender.pool.Exec(ctx, `UPDATE ingest.aio_works SET localized_titles='{"fr":"Titre amélioré"}',updated_at=now()`); err != nil {
		t.Fatal(err)
	}
	delta := catalogEntityPages(t, ctx, sender, grant, first[len(first)-1].Checkpoint)
	count := 0
	for _, p := range delta {
		count += len(p.Supplement.Metadata)
		if len(p.Items) > 0 {
			t.Fatal("metadata update redownloaded torrents")
		}
	}
	if count != 1 {
		t.Fatal("translation update was skipped")
	}
	run = mirrorClaim(t, ctx, receiver, provider, model.ModeIncremental)
	for _, p := range delta {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	var title string
	if err := receiver.pool.QueryRow(ctx, `SELECT localized_titles->>'fr' FROM ingest.aio_works`).Scan(&title); err != nil || title != "Titre amélioré" {
		t.Fatalf("remote metadata did not update: %q %v", title, err)
	}
	// Even after local identification, foreign assertions are not amplified.
	if _, err := receiver.pool.Exec(ctx, `INSERT INTO ingest.aio_matches(provider_id,source_id,fingerprint,work_id,revision,status,reason)
SELECT t.provider_id,t.source_id,t.fields_fingerprint,w.id,w.revision,'matched','verified' FROM ingest.torrents t CROSS JOIN ingest.aio_works w;DELETE FROM ingest.aio_match_queue`); err != nil {
		t.Fatal(err)
	}
	outgoing, err := receiver.CreateShare(ctx, model.ShareInput{Name: "Another recipient", Scope: "all", Enabled: true, Fields: CatalogFields(), CatalogEntities: allCatalogEntities})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range catalogEntityPages(t, ctx, receiver, outgoing, "") {
		if len(p.Supplement.Metadata)+len(p.Supplement.Availability) > 0 {
			t.Fatal("remote assertions were re-exported")
		}
	}
}

func TestCatalogEntitiesFreshLocalNegativeCacheWins(t *testing.T) {
	ctx, sender := sharingDatabase(t)
	grant := catalogEntityFixture(t, ctx, sender)
	pages := catalogEntityPages(t, ctx, sender, grant, "")
	ctx, receiver := sharingDatabase(t)
	if err := receiver.SaveSharedAvailability(ctx, "torbox", model.AIOAvailability{Generation: 1, Hash: catalogTestHash, CheckedAt: time.Now().Add(-2 * time.Minute), Cached: false}); err != nil {
		t.Fatal(err)
	}
	provider := mirrorSource("remote")
	provider.CatalogEntities = allCatalogEntities
	run := mirrorClaim(t, ctx, receiver, provider, model.ModeFull)
	for _, p := range pages {
		run = applyCatalogEntityPage(t, ctx, receiver, run, p)
	}
	mirrorFinish(t, ctx, receiver, run, model.StatusSucceeded)
	var cached bool
	var owner string
	if err := receiver.pool.QueryRow(ctx, `SELECT cached,catalog_import_source FROM ingest.aio_shared_availability`).Scan(&cached, &owner); err != nil || cached || owner != "" {
		t.Fatal("remote observation replaced a fresh local negative check")
	}
}

func TestCatalogEntitiesEnablingShareIncludesEarlierInFlightMetadata(t *testing.T) {
	ctx, db := sharingDatabase(t)
	old := catalogEntityFixture(t, ctx, db)
	if err := db.DeleteShare(ctx, old.Share.ID, old.Share.Revision); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if err = lockCatalogEntityWrites(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE ingest.aio_works SET localized_titles='{"fr":"Titre en cours"}',updated_at=now()`); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type outcome struct {
		credential model.ShareCredential
		err        error
	}
	done := make(chan outcome, 1)
	go func() {
		grant, err := db.CreateShare(deadline, model.ShareInput{Name: "New recipient", Scope: "all", Enabled: true, Fields: CatalogFields(), CatalogEntities: allCatalogEntities})
		done <- outcome{grant, err}
	}()
	select {
	case <-done:
		t.Fatal("opt-in snapshot overtook an in-flight data writer")
	case <-time.After(50 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	count := 0
	for _, p := range catalogEntityPages(t, ctx, db, result.credential, "") {
		for _, a := range p.Supplement.Metadata {
			count++
			if a.LocalizedTitles["fr"] != "Titre en cours" {
				t.Fatal("activation missed the committed data")
			}
		}
	}
	if count != 1 {
		t.Fatal("activation missed previously in-flight metadata")
	}
}
