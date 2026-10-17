package store

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestTorznabCachedRequestsDoNotNeedAnExtraPoolConnection(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	endpoint, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	options := endpoint.Query()
	options.Set("pool_max_conns", "4")
	endpoint.RawQuery = options.Encode()
	db, err := Open(ctx, endpoint.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := db.CreateShare(ctx, model.ShareInput{Format: "torznab", Name: "Cached client", Scope: "all", Enabled: true, Fields: CatalogFields(), TorznabPreferences: &model.TorznabPreferences{CachedOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]*CatalogDownload, 4)
	for index := range requests {
		requests[index], err = db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password)
		if err != nil {
			t.Fatal(err)
		}
		defer requests[index].Close()
	}
	blocker, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close(ctx)
	blockTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blockTx)
	if _, err = blockTx.Exec(ctx, `SELECT id FROM ingest.shares WHERE id=$1 FOR UPDATE`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	readContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	finished := make(chan error, len(requests))
	for _, request := range requests {
		go func() {
			_, err := request.Torznab(readContext, model.TorznabQuery{Mode: "search", Limit: 100}, nil)
			finished <- err
		}()
	}
	for db.pool.Stat().AcquiredConns() < 4 && readContext.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if err = blockTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range requests {
		if err = <-finished; err != nil {
			t.Fatal("concurrent cached queries exhausted their own pool", err)
		}
	}
}

func TestTorznabSavedCachePolicyAppliesToExistingKeyAndCannotBeRelaxed(t *testing.T) {
	ctx, db := sharingDatabase(t)
	for _, hash := range []string{"a", "b", "c"} {
		publishSharingRow(t, ctx, db, "example", hash, `{"title":"Example release","info_hash":"`+strings.Repeat(hash, 40)+`"}`, nil)
	}
	generation, err := db.AIOCacheGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ service, hash string }{{"torbox", "a"}, {"alldebrid", "b"}} {
		if err := db.SaveSharedAvailability(ctx, pair.service, model.AIOAvailability{Generation: generation, Hash: strings.Repeat(pair.hash, 40), Cached: true, CheckedAt: time.Now().Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	grant := torznabGrant(t, ctx, db)
	read := func(q model.TorznabQuery) int64 {
		t.Helper()
		request, err := db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password)
		if err != nil {
			t.Fatal(err)
		}
		defer request.Close()
		q.Mode, q.Limit = "search", 100
		rows, err := request.Torznab(ctx, q, nil)
		if err != nil {
			t.Fatal(err)
		}
		return rows.Total
	}
	if got := read(model.TorznabQuery{}); got != 3 {
		t.Fatal("default access excluded uncached results", got)
	}
	input := model.ShareInput{Format: "torznab", Name: grant.Share.Name, Enabled: true, Scope: "all", Fields: CatalogFields(), Revision: grant.Share.Revision, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads, TorznabPreferences: &model.TorznabPreferences{CachedOnly: true, Services: []string{"torbox"}}}
	updated, err := db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil || updated.TorznabPreferences == nil || !updated.TorznabPreferences.CachedOnly {
		t.Fatal("cache settings were not saved", err)
	}
	for _, query := range []model.TorznabQuery{{}, {CachedOnly: false}, {Services: []string{"alldebrid", "torbox"}}, {Services: []string{"torbox"}}} {
		if got := read(query); got != 1 {
			t.Fatal("saved policy was bypassed or required a replacement key", got)
		}
	}
	if got := read(model.TorznabQuery{Services: []string{"alldebrid"}}); got != 0 {
		t.Fatal("disjoint cache services expanded back to all services", got)
	}
	// Omitted settings on an older management client must not silently widen an
	// existing grant. An explicit empty object is the intentional reset to All.
	input.Revision, input.TorznabPreferences = updated.Revision, nil
	updated, err = db.UpdateShare(ctx, grant.Share.ID, input)
	if err != nil || read(model.TorznabQuery{}) != 1 {
		t.Fatal("omitted preferences discarded a saved cache policy", err)
	}
	input.Revision, input.TorznabPreferences = updated.Revision, &model.TorznabPreferences{}
	if _, err = db.UpdateShare(ctx, grant.Share.ID, input); err != nil || read(model.TorznabQuery{}) != 3 {
		t.Fatal("explicit reset to All did not restore uncached records", err)
	}
	for _, format := range []string{"catalog", "stremio"} {
		input.Format = format
		if format == "stremio" {
			input.StreamPreferences = &model.StreamPreferences{Mode: "all"}
		}
		if ValidateShare(input) == nil {
			t.Fatal("Torznab settings crossed protocol boundaries", format)
		}
	}
}

func TestTorznabPreferencesUpgradePreservesExistingKeysAndDefaultVisibility(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err = db.migrateThrough(ctx, 59); err != nil {
		t.Fatal(err)
	}
	grant := legacyShareFixture(t, ctx, db, model.ShareInput{Name: "Existing Torznab", Scope: "all", Enabled: true, Fields: CatalogFields()})
	if _, err = db.pool.Exec(ctx, `UPDATE ingest.shares SET format='torznab',media_types=ARRAY['movie','tv'],requests_per_minute=19,max_concurrent_downloads=3 WHERE id=$1`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := db.Share(ctx, grant.Share.ID)
	if err != nil || current.TorznabPreferences == nil || current.TorznabPreferences.CachedOnly || len(current.TorznabPreferences.Services) != 0 || len(current.MediaTypes) != 2 || current.RequestsPerMinute != 19 || current.MaxConcurrentDownloads != 3 {
		t.Fatal("upgrade changed existing permissions or defaults", err)
	}
	request, err := db.BeginTorznabRequest(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		t.Fatal("upgrade invalidated an existing key", err)
	}
	defer request.Close()
}
