package vault_test

import (
	"bytes"
	"testing"

	"github.com/theo-bnts/aiosources/internal/vault"
)

func TestInternalSecretsCannotCrossPurposesOrVaultCRUD(t *testing.T) {
	ctx, db := openDatabase(t)
	v := openVault(t, ctx, db, bytes.Repeat([]byte{0x42}, 32))
	plaintext := []byte("synthetic-connection-secret")
	envelope, err := v.Seal("trusted-connection-v1", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(envelope, plaintext) {
		t.Fatal("internal secret was not encrypted")
	}
	if _, err = v.Open("public-installation-v1", envelope); err == nil {
		t.Fatal("trusted secret authenticated as public")
	}
	if err = db.PutSecret(ctx, "trusted-connection-v1", envelope); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Resolve(ctx, "trusted-connection-v1"); err == nil {
		t.Fatal("internal secret resolved through ordinary vault CRUD")
	}
	if err = v.Put(ctx, "public-installation-v1", string(plaintext)); err != nil {
		t.Fatal(err)
	}
	ordinary, err := db.Secret(ctx, "public-installation-v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Open("public-installation-v1", ordinary); err == nil {
		t.Fatal("ordinary vault secret authenticated as connection secret")
	}
	other, err := vault.New(ctx, db, bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := other.Open("trusted-connection-v1", envelope)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatal("internal secret did not survive vault reopening")
	}
	clear(opened)
	envelope[len(envelope)-1] ^= 1
	if _, err = v.Open("trusted-connection-v1", envelope); err == nil {
		t.Fatal("tampered internal secret authenticated")
	}
}

func TestPublicSearchCapabilitiesHaveSeparatePurposes(t *testing.T) {
	ctx, db := openDatabase(t)
	v := openVault(t, ctx, db, bytes.Repeat([]byte{0x24}, 32))
	purposes := []string{"public-catalog-v1", "provider-preparation-v1", "public-installation-v1", "public-playback-v1", "public-torznab-v1", "public-catalog-source-v1", "public-catalog-preparation-v1", "public-catalog-file-v1", "catalog-browser-download-v1"}
	for _, purpose := range purposes {
		sealed, err := v.Seal(purpose, []byte("synthetic-capability-payload"))
		if err != nil {
			t.Fatal(err)
		}
		for _, other := range purposes {
			opened, err := v.Open(other, sealed)
			if (err == nil) != (purpose == other) {
				t.Fatalf("Capability %s accepted as %s", purpose, other)
			}
			clear(opened)
		}
	}
}
