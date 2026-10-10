package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func waitForSecretLock(t *testing.T, ctx context.Context, db *Store, blocker int32) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := db.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("secret operation did not wait for the concurrent transaction")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestDeleteSecretSeesAdmissionCommittedWhileWaiting(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	if err := db.PutSecret(ctx, "credential", []byte("ciphertext")); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	var pid int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid() FROM ingest.secrets WHERE name='credential' FOR KEY SHARE`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan error, 1)
	go func() { deleted <- db.DeleteSecret(ctx, "credential") }()
	waitForSecretLock(t, ctx, db, pid)
	// Model the immutable snapshot commit at the end of an admitted request.
	if _, err := tx.Exec(ctx, `INSERT INTO ingest.runs(id,provider_id,provider_name,mode,status,revision,config)
VALUES('admitted','source','Source','full','queued','','{"id":"source","auth":{"secret_ref":"credential"}}')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; !errors.Is(err, model.ErrConflict) {
		t.Fatalf("deletion missed a newly committed immutable snapshot: %v", err)
	}
	if _, err := db.Secret(ctx, "credential"); err != nil {
		t.Fatalf("admitted collection lost its credential: %v", err)
	}
}

func TestCollectionAdmissionWaitsForSecretDeletion(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	if err := db.PutSecret(ctx, "credential", []byte("ciphertext")); err != nil {
		t.Fatal(err)
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	var pid int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM ingest.secrets WHERE name='credential'`); err != nil {
		t.Fatal(err)
	}
	admitted := make(chan error, 1)
	go func() {
		_, err := db.CreateRun(ctx, model.Run{ProviderID: "source", Mode: model.ModeFull, Config: model.Provider{ID: "source", Auth: model.Auth{SecretRef: "credential"}}})
		admitted <- err
	}()
	waitForSecretLock(t, ctx, db, pid)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-admitted; !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("collection was admitted with a deleted credential: %v", err)
	}
	runs, err := db.ListRuns(ctx, model.ListOptions{})
	if err != nil || runs.Total != 0 {
		t.Fatalf("failed admission left runnable work: total=%d error=%v", runs.Total, err)
	}
}

func TestSecretReferencesSurviveActiveRunTransitions(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	names := []string{"token", "username", "password", "header"}
	for _, name := range names {
		if err := db.PutSecret(ctx, name, []byte("ciphertext")); err != nil {
			t.Fatal(err)
		}
	}
	provider := model.Provider{ID: "source", Auth: model.Auth{SecretRef: "token", UsernameRef: "username", PasswordRef: "password"}, HTTP: model.HTTPConfig{SecretHeaders: map[string]string{"X-Key": "header"}}}
	run, err := db.CreateRun(ctx, model.Run{ProviderID: provider.ID, Config: provider, Mode: model.ModeFull})
	if err != nil {
		t.Fatal(err)
	}
	assertProtected := func() {
		t.Helper()
		for _, name := range names {
			if err := db.DeleteSecret(ctx, name); !errors.Is(err, model.ErrConflict) {
				t.Fatalf("active snapshot did not protect %s: %v", name, err)
			}
		}
	}
	assertProtected()
	claimed, err := db.ClaimNext(ctx)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim: run=%v error=%v", claimed, err)
	}
	assertProtected()
	mirrorFinish(t, ctx, db, *claimed, model.StatusPaused)
	assertProtected()
	if _, err := db.RequestCancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := db.DeleteSecret(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ResumeRun(ctx, run.ID); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("resume admitted deleted credentials: %v", err)
	}
	stored, err := db.GetRun(ctx, run.ID)
	if err != nil || stored.Status != model.StatusCancelled {
		t.Fatalf("failed resume changed durable run state: status=%s error=%v", stored.Status, err)
	}
}
