package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestRuntimeSettingsPreserveSectionsRevisionsAndEffectiveValues(t *testing.T) {
	ctx, dsn, db := activityDatabase(t)
	peer, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	for _, section := range model.RuntimeSections() {
		initial, err := db.RuntimeSection(ctx, section)
		if err != nil || !initial.Valid(section) {
			t.Fatal(section, initial, err)
		}
		for key, field := range model.RuntimeFields(section) {
			if initial.Values[key] != field.Default {
				t.Fatal("changed default", section, key)
			}
			if value, err := peer.RuntimeValue(ctx, section, key); err != nil || value != field.Default {
				t.Fatal("effective default", value, err)
			}
		}
	}
	historyBefore, _ := db.RuntimeSection(ctx, "history")
	initial, _ := db.RuntimeSection(ctx, "connections")
	initial.Values["torznab_seconds"] = 45
	if err := db.UpdateRuntimeSection(ctx, "connections", initial); err != nil {
		t.Fatal(err)
	}
	if value, err := db.ConnectionTimeout(ctx, "torznab"); err != nil || value != 45*time.Second {
		t.Fatal("saved timeout not enforced", value, err)
	}
	if err := db.UpdateRuntimeSection(ctx, "connections", initial); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale update accepted", err)
	}
	initial.Values["extra"] = 1
	if err := db.UpdateRuntimeSection(ctx, "connections", initial); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("unknown field accepted", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		value, err := peer.ConnectionTimeout(ctx, "torznab")
		if err != nil {
			t.Fatal(err)
		}
		if value == 45*time.Second {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("peer retained obsolete policy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	current, _ := db.RuntimeSection(ctx, "history")
	if current.Revision != historyBefore.Revision || current.Values["days"] != 7 {
		t.Fatal("another section was overwritten", current)
	}
}

func TestRuntimeSectionsRejectIncompleteAndUnboundedPolicies(t *testing.T) {
	for _, section := range model.RuntimeSections() {
		for key, field := range model.RuntimeFields(section) {
			for _, invalid := range []int{field.Min - 1, field.Max + 1} {
				value := model.DefaultRuntimeSection(section)
				value.Values[key] = invalid
				if value.Valid(section) {
					t.Fatal(section, key, invalid)
				}
			}
			value := model.DefaultRuntimeSection(section)
			delete(value.Values, key)
			if value.Valid(section) {
				t.Fatal("missing field accepted", section, key)
			}
		}
	}
}

func TestHistoryPolicyActuallyChangesDiagnosticExpiry(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	provider := model.Provider{ID: "retention-policy-source", Adapter: "http_json"}
	run := mirrorClaim(t, ctx, db, provider, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Items: []model.Record{coverageRecord("retained")}, Done: true}, "complete")
	if err != nil {
		t.Fatal(err)
	}
	mirrorFinish(t, ctx, db, run, model.StatusSucceeded)
	ageCollectionHistory(t, ctx, db, run.ID)
	policy, _ := db.RuntimeSection(ctx, "history")
	policy.Values["days"] = 30
	if err := db.UpdateRuntimeSection(ctx, "history", policy); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	if _, err := db.GetRun(ctx, run.ID); err != nil {
		t.Fatal("history was removed before selected expiry", err)
	}
	policy, _ = db.RuntimeSection(ctx, "history")
	policy.Values["days"] = 1
	if err := db.UpdateRuntimeSection(ctx, "history", policy); err != nil {
		t.Fatal(err)
	}
	drainHistory(t, ctx, db)
	if _, err := db.GetRun(ctx, run.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("configured expiry was not enforced", err)
	}
	if len(mirrorRows(t, ctx, db, provider.ID)) != 1 {
		t.Fatal("retention changed published data")
	}
}

func TestAutomaticRecoveryOptOutPreservesManualResumeAndCheckpoint(t *testing.T) {
	ctx, dsn, db := activityDatabase(t)
	run := mirrorClaim(t, ctx, db, model.Provider{ID: "recovery-policy-source", Adapter: "http_json"}, model.ModeFull)
	run, err := db.SavePage(ctx, run, model.Page{Next: []byte(`{"page":2}`)}, "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := db.CollectionSettings(ctx)
	policy.AutoResumeInterrupted = false
	if err := db.UpdateCollectionSettings(ctx, policy); err != nil {
		t.Fatal(err)
	}
	db.Close()
	restarted, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.RecoverInterrupted(ctx, map[string]bool{run.ProviderID: true}); err != nil {
		t.Fatal(err)
	}
	paused, err := restarted.GetRun(ctx, run.ID)
	if err != nil || paused.Status != model.StatusPaused || string(paused.Cursor) != string(run.Cursor) {
		t.Fatal("checkpoint not held for manual recovery", err)
	}
	resumed, err := restarted.ResumeRun(ctx, run.ID)
	if err != nil || resumed.Status != model.StatusQueued || string(resumed.Cursor) != string(run.Cursor) {
		t.Fatal("manual recovery was blocked", err)
	}
}

func TestRuntimePolicyWaitingReadsRespectTheirOwnCancellation(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `LOCK TABLE ingest.runtime_settings IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := db.RuntimeValue(ctx, "debrid", "cache_jobs"); result <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		db.runtime.mu.Lock()
		started := db.runtime.loading != nil
		db.runtime.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("policy read did not start")
		}
		time.Sleep(time.Millisecond)
	}
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := db.RuntimeValue(short, "debrid", "cache_jobs"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting read ignored its deadline", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestConnectionBudgetsKeepTheirAdmissionLeaseUntilDeliveryEnds(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	policy, _ := db.RuntimeSection(ctx, "connections")
	for key := range policy.Values {
		policy.Values[key] = 120
	}
	if err := db.UpdateRuntimeSection(ctx, "connections", policy); err != nil {
		t.Fatal(err)
	}
	for format, begin := range map[string]func(context.Context, string, string) (*CatalogDownload, error){"catalog": db.BeginCatalogDownload, "torznab": db.BeginTorznabRequest, "stremio": db.BeginStremioRequest} {
		input := model.ShareInput{Name: "Configured budget", Format: format, Enabled: true, Scope: "all", Fields: CatalogFields()}
		if format == "stremio" {
			input.StreamPreferences = &model.StreamPreferences{Mode: "all"}
		}
		credential, err := db.CreateShare(ctx, input)
		if err != nil {
			t.Fatal(format, err)
		}
		active, err := begin(ctx, credential.Share.ID, credential.Password)
		if err != nil {
			t.Fatal(format, err)
		}
		remaining := time.Until(active.deadline)
		if remaining < 119*time.Second || remaining > 121*time.Second {
			t.Fatal("configured deadline was not used", format, remaining)
		}
		var lease time.Time
		if err := db.pool.QueryRow(ctx, `SELECT lease_until FROM ingest.share_requests WHERE id=$1`, active.id).Scan(&lease); err != nil || lease.Before(active.deadline.Add(4*time.Second)) {
			t.Fatal("lease ended before response authorization", format, err)
		}
		if err := active.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
