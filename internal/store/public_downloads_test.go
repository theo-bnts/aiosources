package store

import (
	"errors"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/testutil"
)

func TestVisitorAnnounceUpgradeIsOptInAndAccessChangesAreAtomic(t *testing.T) {
	ctx, dsn := testutil.NewDatabase(t)
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.migrateThrough(ctx, 51); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(ctx, `UPDATE ingest.aio_public_access SET revision=7,enabled=true,generation=9`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := db.PublicAccess(ctx)
	if err != nil || before.AllowPrivateDownloads || !before.Enabled || before.Revision != 7 || before.Generation != 9 {
		t.Fatal("upgrade exposed sources or changed existing installation access", before, err)
	}
	before.AllowPrivateDownloads = true
	if err := db.SetPublicAccess(ctx, before); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPublicAccess(ctx, before); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale permission update accepted")
	}
	current, _ := db.PublicAccess(ctx)
	if !current.AllowPrivateDownloads || current.Generation != 9 || current.Revision != 8 {
		t.Fatal("optional permission revoked ordinary cached installations")
	}
	if _, err := db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_download_permission_log() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN IF NEW.kind='security.public_source_downloads_disabled' THEN RAISE EXCEPTION 'synthetic log failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER reject_download_permission_log BEFORE INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.reject_download_permission_log()`); err != nil {
		t.Fatal(err)
	}
	disabled := current
	disabled.AllowPrivateDownloads = false
	if err := db.SetPublicAccess(ctx, disabled); err == nil {
		t.Fatal("access permission committed without its log")
	}
	afterFailure, _ := db.PublicAccess(ctx)
	if afterFailure != current {
		t.Fatal("failed log did not roll back permission, revision and generation")
	}
	if _, err := db.pool.Exec(ctx, `DROP TRIGGER reject_download_permission_log ON ingest.activity_logs; DROP FUNCTION ingest.reject_download_permission_log()`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPublicAccess(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}})
	if err != nil || len(logs.Items) != 2 || logs.Items[0].Message != "Visitor announce URLs disabled" || logs.Items[1].Message != "Visitor announce URLs enabled" {
		t.Fatal("permission changes do not use the standard safe log presentation", err)
	}
	for _, item := range logs.Items {
		if len(item.Data) != 0 {
			t.Fatal("access log retained visitor or source data")
		}
	}
}
