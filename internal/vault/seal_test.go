package vault_test

import (
	"bytes"
	"testing"

	"github.com/theo-bnts/aiosources/internal/vault"
)

func TestInternalSecretsCannotCrossPurposesOrVaultCRUD(t *testing.T) {
	ctx, db := openDatabase(t)
	v := openVault(t, ctx, db, bytes.Repeat([]byte{0x42}, 32))
	plaintext := []byte("synthetic-authenticator-secret")
	envelope, err := v.Seal("admin-mfa-pending-v1", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope, plaintext) {
		t.Fatal("internal secret was not encrypted")
	}
	if _, err = v.Open("admin-mfa-active-v1", envelope); err == nil {
		t.Fatal("pending secret authenticated as active")
	}
	if err = db.PutSecret(ctx, "admin-mfa-pending-v1", envelope); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Resolve(ctx, "admin-mfa-pending-v1"); err == nil {
		t.Fatal("internal secret resolved through ordinary vault CRUD")
	}
	if err = v.Put(ctx, "admin-mfa-active-v1", string(plaintext)); err != nil {
		t.Fatal(err)
	}
	ordinary, err := db.Secret(ctx, "admin-mfa-active-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Open("admin-mfa-active-v1", ordinary); err == nil {
		t.Fatal("ordinary vault secret authenticated as MFA secret")
	}
	other, err := vault.New(ctx, db, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := other.Open("admin-mfa-pending-v1", envelope)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatal("internal secret did not survive vault reopening")
	}
	clear(opened)
	envelope[len(envelope)-1] ^= 1
	if _, err = v.Open("admin-mfa-pending-v1", envelope); err == nil {
		t.Fatal("tampered internal secret authenticated")
	}
}
