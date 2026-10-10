package store

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestSecretRevisionConcurrentCreationRotationAndRecreation(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	compete := func(revision int64) {
		t.Helper()
		results := make(chan error, 16)
		var workers sync.WaitGroup
		for index := range 16 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				results <- db.PutSecretAtRevision(ctx, "credential", []byte{byte(index + 1)}, revision)
			}()
		}
		workers.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, model.ErrConflict) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("%d competing secret writes committed", successes)
		}
	}
	readRevision := func() int64 {
		t.Helper()
		items, err := db.ListSecrets(ctx)
		if err != nil || len(items) != 1 {
			t.Fatalf("secret metadata: %+v %v", items, err)
		}
		return items[0].Revision
	}
	compete(0)
	first := readRevision()
	compete(first)
	second := readRevision()
	if second <= first {
		t.Fatal("rotation did not replace the secret revision")
	}
	before, err := db.Secret(ctx, "credential")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSecretAtRevision(ctx, "credential", first); !errors.Is(err, model.ErrConflict) {
		t.Fatal("a stale deletion was accepted")
	}
	after, _ := db.Secret(ctx, "credential")
	if !bytes.Equal(before, after) {
		t.Fatal("rejected deletion changed ciphertext")
	}
	if err := db.DeleteSecretAtRevision(ctx, "credential", second); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecretAtRevision(ctx, "credential", []byte("recreated"), 0); err != nil {
		t.Fatal(err)
	}
	if readRevision() <= second {
		t.Fatal("deleting and recreating a name reused its revision")
	}
	if err := db.PutSecretAtRevision(ctx, "credential", []byte("stale"), first); !errors.Is(err, model.ErrConflict) {
		t.Fatal("an old form replaced a recreated secret")
	}
	if err := db.PutSecret(ctx, "credential", []byte("explicit CLI rotation")); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecretAtRevision(ctx, "credential", []byte("stale"), second); !errors.Is(err, model.ErrConflict) {
		t.Fatal("an explicit internal rotation did not invalidate open forms")
	}
}

func TestSettingsRevisionMigrationsPreserveExistingValues(t *testing.T) {
	ctx, _, db := activityDatabase(t)
	if err := db.PutSecret(ctx, "retained", []byte("retained ciphertext")); err != nil {
		t.Fatal(err)
	}
	before, _ := db.ListSecrets(ctx)
	if _, err := db.pool.Exec(ctx, `ALTER TABLE ingest.secrets DROP COLUMN revision;
DROP SEQUENCE ingest.secret_revisions;
DELETE FROM ingest.schema_migrations WHERE version=30`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		items, err := db.ListSecrets(ctx)
		if err != nil || len(items) != 1 || items[0].Name != before[0].Name || !items[0].UpdatedAt.Equal(before[0].UpdatedAt) || items[0].Revision < 1 {
			t.Fatal("migration changed existing secret metadata")
		}
		ciphertext, err := db.Secret(ctx, "retained")
		if err != nil || string(ciphertext) != "retained ciphertext" {
			t.Fatal("migration changed encrypted data")
		}
	}
}
