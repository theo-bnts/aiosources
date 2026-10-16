package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestInvalidCatalogCredentialsDoNotWaitForAnActiveDownloadLock(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	db, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	grant := sharingGrant(t, ctx, db)
	control, err := pgx.Connect(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close(context.Background()) })
	tx, err := control.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `SELECT id FROM ingest.shares WHERE id=$1 FOR UPDATE`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := db.BeginCatalogDownload(bounded, grant.Share.ID, strings.Repeat("A", 43)); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("invalid credentials occupied a connection waiting for the share lock: %v", err)
	}
	var charged int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.share_requests WHERE share_id=$1`, grant.Share.ID).Scan(&charged); err != nil || charged != 0 {
		t.Fatalf("unauthenticated requests consumed the recipient quota: %d %v", charged, err)
	}
}

func TestCatalogAdmissionRechecksRevocationAfterWaitingForItsLock(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := sharingGrant(t, ctx, db)
	control, err := pgx.ConnectConfig(ctx, db.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close(context.Background()) })
	tx, err := control.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `SELECT id FROM ingest.shares WHERE id=$1 FOR UPDATE`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		download, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
		if download != nil {
			_ = download.Close()
		}
		result <- err
	}()
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := control.QueryRow(deadline, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%FROM ingest.shares WHERE id=$1%FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal("admission did not reach its locked recheck:", err)
		}
		if waiting {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline.Done():
			t.Fatal("admission never waited for the recipient lock")
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE ingest.shares SET enabled=false,revision=revision+1 WHERE id=$1`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrCatalogAuthentication) {
			t.Fatalf("a credential verified before revocation was admitted afterwards: %v", err)
		}
	case <-deadline.Done():
		t.Fatal("revoked admission did not finish")
	}
}

func TestCatalogAdmissionIsSharedAndOnlyChargesAuthenticatedRequests(t *testing.T) {
	ctx, endpoint := testutil.NewDatabase(t)
	first, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err := first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	requests, concurrent := 3, 1
	grant, err := first.CreateShare(ctx, model.ShareInput{Name: "Limited", Enabled: true, Scope: "all", Fields: []string{"title"}, RequestsPerMinute: &requests, MaxConcurrentDownloads: &concurrent})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if _, err := second.BeginCatalogDownload(ctx, grant.Share.ID, strings.Repeat("A", 43)); !errors.Is(err, ErrCatalogAuthentication) {
			t.Fatalf("password guess was admitted: %v", err)
		}
	}
	type admission struct {
		download *CatalogDownload
		err      error
	}
	start := make(chan struct{})
	results := make(chan admission, 8)
	for index := range 8 {
		db := []*Store{first, second}[index%2]
		go func() {
			<-start
			download, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
			results <- admission{download: download, err: err}
		}()
	}
	close(start)
	var active *CatalogDownload
	for range 8 {
		result := <-results
		if result.err == nil {
			t.Cleanup(func() { _ = result.download.Close() })
			if active != nil {
				t.Error("simultaneous instances both acquired the one available download")
			}
			active = result.download
		} else {
			var quota *CatalogQuotaError
			if !errors.As(result.err, &quota) || !quota.Concurrent {
				t.Errorf("concurrent admission returned an unexpected error: %v", result.err)
			}
		}
	}
	if active == nil {
		t.Fatal("no authenticated request was admitted")
	}
	var quota *CatalogQuotaError
	if _, err := second.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{Head: true}); !errors.As(err, &quota) || !quota.Concurrent || quota.RetryAfter < 1 {
		t.Fatalf("another instance bypassed the active download: %v", err)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	// Rejection above must not spend a rate slot; both following requests fit.
	sharingPage(t, ctx, second, grant, CatalogPageRequest{Head: true})
	sharingPage(t, ctx, first, grant, CatalogPageRequest{})
	if _, err := second.CatalogPage(ctx, grant.Share.ID, grant.Password, CatalogPageRequest{}); !errors.As(err, &quota) || quota.Concurrent || quota.RetryAfter < 1 || quota.RetryAfter > 60 {
		t.Fatalf("authenticated sliding-minute quota was not enforced: %v", err)
	}
	// Advance stored history rather than sleeping through the minute boundary.
	if _, err := first.pool.Exec(ctx, `UPDATE ingest.share_requests SET requested_at=clock_timestamp()-interval '61 seconds' WHERE share_id=$1`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	sharingPage(t, ctx, second, grant, CatalogPageRequest{})
}

func TestCatalogCrashedLeaseExpiresAndCannotResume(t *testing.T) {
	ctx, db := sharingDatabase(t)
	concurrent := 1
	grant, err := db.CreateShare(ctx, model.ShareInput{Name: "Lease", Enabled: true, Scope: "all", Fields: []string{"title"}, MaxConcurrentDownloads: &concurrent})
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = abandoned.Close() })
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.share_requests SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, abandoned.id); err != nil {
		t.Fatal(err)
	}
	sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	if _, err := abandoned.Page(ctx, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("expired lease remained a capability: %v", err)
	}
}

func TestCatalogExpiryRejectsOldContinuationsAndAdmittedDownloads(t *testing.T) {
	ctx, db := sharingDatabase(t)
	publishSharingRow(t, ctx, db, "one", "a", `{"title":"first"}`, nil)
	publishSharingRow(t, ctx, db, "one", "b", `{"title":"second"}`, nil)
	grant := sharingGrant(t, ctx, db)
	page := sharingPage(t, ctx, db, grant, CatalogPageRequest{Limit: 1})
	snapshot := sharingPage(t, ctx, db, grant, CatalogPageRequest{})
	admitted, err := db.BeginCatalogDownload(ctx, grant.Share.ID, grant.Password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admitted.Close() })
	// Simulate time passing without changing the signed cursor's revision.
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.shares SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, grant.Share.ID); err != nil {
		t.Fatal(err)
	}
	for _, request := range []CatalogPageRequest{{Cursor: page.NextCursor}, {Checkpoint: snapshot.Checkpoint}, {Head: true}} {
		if _, err := db.CatalogPage(ctx, grant.Share.ID, grant.Password, request); !errors.Is(err, ErrCatalogAuthentication) {
			t.Fatalf("expired share exported via continuation: %v", err)
		}
	}
	if _, err := admitted.Page(ctx, CatalogPageRequest{}); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("admitted request bypassed expiry: %v", err)
	}
	if err := admitted.Validate(ctx); !errors.Is(err, ErrCatalogAuthentication) {
		t.Fatalf("encoded response bypassed expiry: %v", err)
	}
}

func TestShareMutationsRollBackWhenActivityCannotBeWritten(t *testing.T) {
	ctx, db := sharingDatabase(t)
	grant := sharingGrant(t, ctx, db)
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_share_activity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'unavailable activity'; END $$; CREATE TRIGGER reject_share_activity BEFORE INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.reject_share_activity();`); err != nil {
		t.Fatal(err)
	}
	input := model.ShareInput{Name: "Unlogged", Enabled: true, Scope: "all", Fields: []string{"title"}, RequestsPerMinute: &grant.Share.RequestsPerMinute, MaxConcurrentDownloads: &grant.Share.MaxConcurrentDownloads}
	if _, err := db.CreateShare(ctx, input); err == nil {
		t.Fatal("created an unlogged credential")
	}
	input.Revision = grant.Share.Revision
	input.Enabled = false
	if _, err := db.UpdateShare(ctx, grant.Share.ID, input); err == nil {
		t.Fatal("changed unlogged permissions")
	}
	if _, err := db.RotateShare(ctx, grant.Share.ID, grant.Share.Revision); err == nil {
		t.Fatal("rotated an unlogged password")
	}
	if err := db.DeleteShare(ctx, grant.Share.ID, grant.Share.Revision); err == nil {
		t.Fatal("revoked an unlogged credential")
	}
	shares, err := db.Shares(ctx)
	if err != nil || len(shares) != 1 || shares[0].Revision != grant.Share.Revision || !shares[0].Enabled || shares[0].Name != grant.Share.Name {
		t.Fatalf("failed activity left a partial share mutation: %+v %v", shares, err)
	}
	sharingPage(t, ctx, db, grant, CatalogPageRequest{})
}
