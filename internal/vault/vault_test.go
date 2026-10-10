package vault_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
	"github.com/theo-bnts/aiosources/internal/testutil"
	"github.com/theo-bnts/aiosources/internal/vault"
)

func openDatabase(t *testing.T) (context.Context, *store.Store) {
	t.Helper()
	ctx, url := testutil.NewDatabase(t)
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, db
}

func openVault(t *testing.T, ctx context.Context, db *store.Store, key []byte) *vault.Vault {
	t.Helper()
	v, err := vault.New(ctx, db, key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func assertSecret(t *testing.T, ctx context.Context, v *vault.Vault, name, want string) {
	t.Helper()
	got, err := v.Resolve(ctx, name)
	if err != nil || got != want {
		t.Fatalf("secret %q did not roundtrip exactly: err=%v", name, err)
	}
}

func TestVaultRoundtripRotationAndMetadata(t *testing.T) {
	ctx, db := openDatabase(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	v := openVault(t, ctx, db, key)
	const name = "archive-key"
	const original = "  synthetic-value-42\nwith intentional whitespace\t"
	if err := v.Put(ctx, name, original); err != nil {
		t.Fatal(err)
	}
	assertSecret(t, ctx, v, name, original)
	first, err := db.Secret(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first, []byte(original)) {
		t.Fatal("plaintext secret was persisted")
	}
	if err := v.Put(ctx, name, original); err != nil {
		t.Fatal(err)
	}
	second, err := db.Secret(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("re-encrypting the same secret reused its ciphertext")
	}
	const rotated = "replacement-synthetic-value-79"
	if err := v.Put(ctx, name, rotated); err != nil {
		t.Fatal(err)
	}
	reopened := openVault(t, ctx, db, key)
	assertSecret(t, ctx, reopened, name, rotated)
	assertSecret(t, ctx, v, name, rotated)
	metadata, err := reopened.List(ctx)
	if err != nil || len(metadata) != 1 || metadata[0].Name != name || metadata[0].UpdatedAt.IsZero() || metadata[0].Revision < 1 {
		t.Fatalf("rotation must retain one discoverable metadata entry: items=%+v, err=%v", metadata, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	var public []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	for field := range public[0] {
		if field != "name" && field != "updated_at" && field != "revision" {
			t.Fatalf("vault metadata exposed a non-metadata field %q", field)
		}
	}
	if bytes.Contains(encoded, []byte(rotated)) || bytes.Contains(encoded, []byte(original)) {
		t.Fatal("vault metadata exposed a secret value")
	}
	if _, err := vault.New(ctx, db, bytes.Repeat([]byte{0x42}, 32)); err == nil {
		t.Fatal("a different master key was accepted")
	}
	assertSecret(t, ctx, reopened, name, rotated)
	if err := reopened.Delete(ctx, name); err != nil {
		t.Fatal(err)
	}
	if value, err := v.Resolve(ctx, name); !errors.Is(err, model.ErrNotFound) || value != "" {
		t.Fatalf("deleted secret remained resolvable: err=%v", err)
	}
	metadata, err = v.List(ctx)
	if err != nil || len(metadata) != 0 {
		t.Fatalf("deleted secret remained in metadata: items=%+v, err=%v", metadata, err)
	}
}

func TestSecretCreationRetryPreservesCiphertextAndRejectsDifferentValues(t *testing.T) {
	ctx, db := openDatabase(t)
	v := openVault(t, ctx, db, bytes.Repeat([]byte{0x41}, 32))
	const name = "owned-inline-reference"
	const value = "  synthetic-inline-value\n"
	if err := v.PutAtRevision(ctx, name, value, 0); err != nil {
		t.Fatal(err)
	}
	original, err := v.List(ctx)
	if err != nil || len(original) != 1 {
		t.Fatal("created secret metadata is missing")
	}
	ciphertext, err := db.Secret(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := v.PutAtRevision(ctx, name, value, 0); err != nil {
			t.Fatal("retry of an acknowledged-or-lost identical creation failed:", err)
		}
	}
	if err := v.PutAtRevision(ctx, name, "synthetic-different-value", 0); !errors.Is(err, model.ErrConflict) {
		t.Fatal("create-only retry replaced a different existing value")
	}
	current, err := v.List(ctx)
	if err != nil || len(current) != 1 || current[0].Revision != original[0].Revision || !current[0].UpdatedAt.Equal(original[0].UpdatedAt) {
		t.Fatal("creation retry changed metadata or created another reference")
	}
	retained, err := db.Secret(ctx, name)
	if err != nil || !bytes.Equal(retained, ciphertext) {
		t.Fatal("creation retry re-encrypted or replaced the stored value")
	}
	assertSecret(t, ctx, v, name, value)
	if err := v.PutAtRevision(ctx, name, "synthetic-rotation", original[0].Revision); err != nil {
		t.Fatal(err)
	}
	if err := v.PutAtRevision(ctx, name, value, 0); !errors.Is(err, model.ErrConflict) {
		t.Fatal("old creation replay reversed a later rotation")
	}
	if err := v.PutAtRevision(ctx, name, "synthetic-rotation", original[0].Revision); !errors.Is(err, model.ErrConflict) {
		t.Fatal("identical values bypassed the revision check for replacement")
	}
	assertSecret(t, ctx, v, name, "synthetic-rotation")
}

func TestVaultRejectsTamperingAndCiphertextNameSwaps(t *testing.T) {
	ctx, db := openDatabase(t)
	v := openVault(t, ctx, db, bytes.Repeat([]byte{0x43}, 32))
	for name, value := range map[string]string{"alpha": "alpha-synthetic-secret", "beta": "beta-synthetic-secret"} {
		if err := v.Put(ctx, name, value); err != nil {
			t.Fatal(err)
		}
	}
	alpha, err := db.Secret(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := db.Secret(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecret(ctx, "alpha", beta); err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecret(ctx, "beta", alpha); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if value, err := v.Resolve(ctx, name); err == nil || value != "" {
			t.Fatalf("swapped ciphertext for %q was not rejected", name)
		}
	}
	tampered := bytes.Clone(alpha)
	tampered[len(tampered)-1] ^= 1
	if err := db.PutSecret(ctx, "alpha", tampered); err != nil {
		t.Fatal(err)
	}
	if value, err := v.Resolve(ctx, "alpha"); err == nil || value != "" {
		t.Fatal("tampered authentication tag was not rejected")
	}
	if err := db.PutSecret(ctx, "alpha", alpha); err != nil {
		t.Fatal(err)
	}
	assertSecret(t, ctx, v, "alpha", "alpha-synthetic-secret")
	if err := db.PutSecret(ctx, "beta", []byte{1}); err != nil {
		t.Fatal(err)
	}
	if value, err := v.Resolve(ctx, "beta"); err == nil || value != "" {
		t.Fatal("truncated ciphertext was not rejected")
	}
}

func TestVaultMissingFingerprintDoesNotAuthorizeExistingCiphertext(t *testing.T) {
	ctx, source := openDatabase(t)
	key := bytes.Repeat([]byte{0x44}, 32)
	v := openVault(t, ctx, source, key)
	if err := v.Put(ctx, "archive-key", "synthetic-backup-value"); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := source.Secret(ctx, "archive-key")
	if err != nil {
		t.Fatal(err)
	}
	// Restore encrypted rows into a fresh database without their key fingerprint.
	ctx, restored := openDatabase(t)
	if err := restored.PutSecret(ctx, "archive-key", ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range [][]byte{bytes.Repeat([]byte{0x45}, 32), key} {
		if _, err := vault.New(ctx, restored, candidate); err == nil {
			t.Fatal("missing fingerprint silently authorized existing ciphertext")
		}
	}
	preserved, err := restored.Secret(ctx, "archive-key")
	if err != nil || !bytes.Equal(preserved, ciphertext) {
		t.Fatalf("failed initialization changed recoverable encrypted data: %v", err)
	}
	assertSecret(t, ctx, v, "archive-key", "synthetic-backup-value")
}

func TestVaultMissingFingerprintDoesNotAuthorizeMFAOnlyCiphertext(t *testing.T) {
	ctx, source := openDatabase(t)
	key := bytes.Repeat([]byte{0x46}, 32)
	v := openVault(t, ctx, source, key)
	const fingerprintSetting = "vault.master_key_fingerprint.v1"
	fingerprint, err := source.Setting(ctx, fingerprintSetting)
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"admin-mfa-active-v1", "admin-mfa-pending-v1"} {
		t.Run(purpose, func(t *testing.T) {
			plaintext := []byte("synthetic-restored-mfa-secret")
			ciphertext, err := v.Seal(purpose, plaintext)
			if err != nil {
				t.Fatal(err)
			}
			ctx, restored := openDatabase(t)
			err = restored.SecurityUpdate(ctx, "", nil, func(state *store.SecurityState) (store.SecurityChange, error) {
				if purpose == "admin-mfa-active-v1" {
					state.ActiveSecret = ciphertext
				} else {
					// Expired enrollment still contains key-bound ciphertext.
					expired, session := time.Unix(1, 0), "restored-session"
					state.PendingSecret = ciphertext
					state.PendingExpiresAt = &expired
					state.PendingSessionID = &session
				}
				return store.SecurityChange{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range [][]byte{bytes.Repeat([]byte{0x47}, 32), key} {
				if _, err := vault.New(ctx, restored, candidate); err == nil {
					t.Fatal("missing fingerprint authorized MFA-only ciphertext")
				}
			}
			if _, err := restored.Setting(ctx, fingerprintSetting); !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("failed initialization established a replacement fingerprint: %v", err)
			}
			if _, err := restored.SetSettingOnce(ctx, fingerprintSetting, fingerprint); err != nil {
				t.Fatal(err)
			}
			recovered := openVault(t, ctx, restored, key)
			err = restored.SecurityUpdate(ctx, "", nil, func(state *store.SecurityState) (store.SecurityChange, error) {
				preserved := state.ActiveSecret
				if purpose == "admin-mfa-pending-v1" {
					preserved = state.PendingSecret
				}
				opened, err := recovered.Open(purpose, preserved)
				defer clear(opened)
				if err != nil || !bytes.Equal(opened, plaintext) {
					t.Fatal("failed initialization damaged recoverable MFA material")
				}
				return store.SecurityChange{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
