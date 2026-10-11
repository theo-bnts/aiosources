package store

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSharedAvailabilityRetainsOnlyAccountIndependentDataAndFreshEvidence(t *testing.T) {
	ctx, db := sharingDatabase(t)
	generation, _ := db.AIOCacheGeneration(ctx)
	hash := strings.Repeat("a", 40)
	positive := model.AIOAvailability{Hash: hash, Cached: true, CheckedAt: time.Now(), Generation: generation,
		Files: []model.AIOFile{{ID: 42, Path: "Example.mp4", Size: 100, Link: "https://private.example/account-link"}}}
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	older := positive
	older.Cached, older.CheckedAt = false, positive.CheckedAt.Add(-time.Minute)
	if err := db.SaveSharedAvailability(ctx, "torbox", older); err != nil {
		t.Fatal(err)
	}
	other := older
	other.CheckedAt = time.Now().Add(-20 * time.Minute)
	if err := db.SaveSharedAvailability(ctx, "alldebrid", other); err != nil {
		t.Fatal(err)
	}
	read := func(service string) []model.AIOAvailability {
		t.Helper()
		items, err := db.SharedAvailability(ctx, service, []string{hash}, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	items := read("torbox")
	if len(items) != 1 || !items[0].Cached || len(items[0].Files) != 1 || items[0].Files[0].ID != 0 || items[0].Files[0].Link != "" || positive.Files[0].ID != 42 {
		t.Fatal("shared projection is account-bound, mutated its input or accepted older evidence")
	}
	if len(read("alldebrid")) != 0 {
		t.Fatal("old negative suppressed a later cache check")
	}
	if err := db.ForgetSharedAvailability(ctx, "torbox", hash, older.CheckedAt); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 1 {
		t.Fatal("old invalidation removed a newer result")
	}
	positive.CheckedAt = time.Now().Add(time.Hour)
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 0 {
		t.Fatal("future evidence was served")
	}
	if err := db.ResetAIOCache(ctx); err != nil {
		t.Fatal(err)
	}
	positive.CheckedAt = time.Now()
	if err := db.SaveSharedAvailability(ctx, "torbox", positive); err != nil {
		t.Fatal(err)
	}
	if len(read("torbox")) != 0 {
		t.Fatal("reset was repopulated by an old writer")
	}
	access, err := db.PublicAccess(ctx)
	if err != nil || access.Enabled {
		t.Fatal("public access enabled by default", err)
	}
	access.Enabled = true
	if err = db.SetPublicAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	if err = db.SetPublicAccess(ctx, access); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale access change accepted")
	}
}

func TestProviderDiagnosticsUseAllowlistedValuesAndConcurrentFixedCaps(t *testing.T) {
	ctx, db := sharingDatabase(t)
	run, err := db.BeginOperation(ctx, model.OperationRun{Kind: "cache", Service: "torbox", Trigger: "playback"}, "synthetic-cache-operation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.pool.Exec(ctx, `INSERT INTO ingest.operation_events(run_id,phase,processed,errors) SELECT $1,'availability',0,0 FROM generate_series(1,998)`, run.ID); err != nil {
		t.Fatal(err)
	}
	var checks sync.WaitGroup
	for range 8 {
		checks.Add(1)
		go func() {
			defer checks.Done()
			if e := db.RecordProviderCall(ctx, run.ID, "provider_availability", 200, 5, ""); e != nil {
				t.Error(e)
			}
		}()
	}
	checks.Wait()
	events, err := db.OperationEvents(ctx, run.ID, model.ListOptions{})
	if err != nil || events.Total != 1000 {
		t.Fatal("concurrent diagnostics bypassed fixed cap", events.Total, err)
	}
	if err = db.RecordProviderCall(ctx, run.ID, "https://private.example/key", 200, 5, ""); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("arbitrary phase persisted")
	}
	request := model.StremioRequest{Origin: "public", Kind: "playback", Service: "torbox", MediaType: "movie", Resource: "imdb:tt1234567", Outcome: "results"}
	for range 40 {
		request.Calls = append(request.Calls, model.ProviderCall{Phase: "provider_playback", HTTPStatus: 200, DurationMS: 10})
	}
	if err = db.RecordStremioRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListStremioRequests(ctx, model.ListOptions{})
	if len(list.Items) != 1 || list.Items[0].Calls != nil {
		t.Fatal("summary loads detailed diagnostics")
	}
	detail, err := db.StremioRequest(ctx, list.Items[0].ID)
	if err != nil || len(detail.Calls) != 32 {
		t.Fatal("playback diagnostics not bounded", err)
	}
	request.Calls = []model.ProviderCall{{Phase: "provider_playback", ErrorCode: "private-value"}}
	if err = db.RecordStremioRequest(ctx, request); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("arbitrary provider error persisted")
	}
}

func TestPublicAccessChangesAreLoggedAtomicallyWithoutVisitorData(t *testing.T) {
	ctx, db := sharingDatabase(t)
	initial, err := db.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	logCount := func() int64 {
		t.Helper()
		var count int64
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM ingest.activity_logs WHERE kind IN ('security.public_access_enabled','security.public_access_disabled')`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	enable := initial
	enable.Enabled = true
	if err = db.SetPublicAccess(ctx, enable); err != nil {
		t.Fatal(err)
	}
	if logCount() != 1 {
		t.Fatal("enabling public access did not record its security change")
	}
	if err = db.SetPublicAccess(ctx, enable); !errors.Is(err, model.ErrConflict) || logCount() != 1 {
		t.Fatal("a conflicting save changed access or created an audit event")
	}
	current, _ := db.PublicAccess(ctx)
	if err = db.SetPublicAccess(ctx, current); err != nil || logCount() != 1 {
		t.Fatal("an unchanged setting fabricated a security change", err)
	}
	unchanged, _ := db.PublicAccess(ctx)
	if unchanged.Generation != current.Generation {
		t.Fatal("an unchanged setting revoked visitor installations")
	}
	// A storage failure must not expose the instance or revoke installations
	// without committing the matching log in the same transaction.
	if _, err = db.pool.Exec(ctx, `CREATE FUNCTION ingest.reject_public_access_log() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN IF NEW.kind='security.public_access_disabled' THEN RAISE EXCEPTION 'synthetic log failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER reject_public_access_log BEFORE INSERT ON ingest.activity_logs FOR EACH ROW EXECUTE FUNCTION ingest.reject_public_access_log()`); err != nil {
		t.Fatal(err)
	}
	disable := unchanged
	disable.Enabled = false
	if err = db.SetPublicAccess(ctx, disable); err == nil {
		t.Fatal("public access changed without its security log")
	}
	afterFailure, _ := db.PublicAccess(ctx)
	if afterFailure != unchanged || logCount() != 1 {
		t.Fatal("failed log did not roll back the access setting and generation")
	}
	if _, err = db.pool.Exec(ctx, `DROP TRIGGER reject_public_access_log ON ingest.activity_logs; DROP FUNCTION ingest.reject_public_access_log()`); err != nil {
		t.Fatal(err)
	}
	if err = db.SetPublicAccess(ctx, disable); err != nil || logCount() != 2 {
		t.Fatal("disabling public access did not record its security change", err)
	}
	logs, err := db.Logs(ctx, model.LogOptions{ListOptions: model.ListOptions{Limit: 50}})
	if err != nil || len(logs.Items) != 2 || logs.Items[0].Message != "Public access disabled" || logs.Items[1].Message != "Public access enabled" {
		t.Fatal("security changes do not use the standard Logs presentation", err)
	}
	for _, item := range logs.Items {
		if len(item.Data) != 0 || item.ProviderID != "" || item.RunID != "" {
			t.Fatal("public access log saved unrelated or visitor data")
		}
	}
}

func TestPublicAccessLogUpgradePreservesExistingAccessMessagesAndSettings(t *testing.T) {
	ctx, db := sharingDatabase(t)
	before, err := db.PublicAccess(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the function and migration stamp from the prior release. Other
	// migrations and persistent public settings retain their original state.
	oldFunction, _, ok := strings.Cut(consoleSimplificationSchema, "ALTER FUNCTION ingest.activity_message")
	if !ok {
		t.Fatal("could not identify the prior access-message definition")
	}
	oldFunction = strings.Replace(oldFunction, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
	if _, err = db.pool.Exec(ctx, oldFunction+`DELETE FROM ingest.schema_migrations WHERE version=50`); err != nil {
		t.Fatal(err)
	}
	var previousMessages string
	query := `SELECT jsonb_agg(jsonb_build_array(kind,ingest.activity_message(kind)) ORDER BY kind)::text
FROM unnest(ARRAY['security.mfa_enabled','security.mfa_disabled','security.recovery_rotated','security.session_revoked','security.sessions_revoked','share.created','share.permissions_changed','share.rotated','share.revoked']) kind`
	if err = db.pool.QueryRow(ctx, query).Scan(&previousMessages); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var migratedMessages string
	if err = db.pool.QueryRow(ctx, query).Scan(&migratedMessages); err != nil || migratedMessages != previousMessages {
		t.Fatal("public-access logging changed previous access messages", err)
	}
	after, err := db.PublicAccess(ctx)
	if err != nil || after != before {
		t.Fatal("logging migration altered public settings or revoked installations", err)
	}
	before.Enabled = true
	if err = db.SetPublicAccess(ctx, before); err != nil {
		t.Fatal("upgraded instance cannot change and log public access", err)
	}
}
