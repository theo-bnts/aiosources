// Package vault stores only authenticated ciphertext in PostgreSQL. Secret
// values are available to connectors through Resolve, never through metadata.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

// MaxSecretBytes prevents a secret mutation from becoming an unbounded blob.
// Values are otherwise preserved exactly, including intentional whitespace.
const MaxSecretBytes = 64 * 1024
const fingerprintSetting = "vault.master_key_fingerprint.v1"
const envelopeVersion byte = 1

var secretName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type Vault struct {
	db   *store.Store
	aead cipher.AEAD
}

func New(ctx context.Context, db *store.Store, key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("vault master key must contain exactly 32 bytes")
	}
	if db == nil {
		return nil, fmt.Errorf("vault database is required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize vault encryption")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize vault authentication")
	}
	hash := sha256.New()
	hash.Write([]byte("scraper/vault/master-key-fingerprint/v1\x00"))
	hash.Write(key)
	fingerprint := hash.Sum(nil)
	_, err = db.Setting(ctx, fingerprintSetting)
	if errors.Is(err, model.ErrNotFound) {
		// A lost fingerprint must not silently authorize a new key against an
		// existing encrypted vault or MFA state. Recovery requires its setting.
		existing, inspectErr := db.HasEncryptedMaterial(ctx)
		if inspectErr != nil {
			return nil, fmt.Errorf("cannot inspect vault initialization")
		}
		if existing {
			// Another process may have initialized and stored encrypted material
			// since our first read. Only its fingerprint can authorize us.
			if _, readErr := db.Setting(ctx, fingerprintSetting); readErr != nil {
				return nil, fmt.Errorf("vault fingerprint is missing for existing encrypted secrets")
			}
		}
	} else if err != nil {
		return nil, fmt.Errorf("cannot read vault initialization")
	}
	stored, err := db.SetSettingOnce(ctx, fingerprintSetting, hex.EncodeToString(fingerprint))
	if err != nil {
		return nil, fmt.Errorf("cannot verify vault master key")
	}
	expected, err := hex.DecodeString(stored)
	if err != nil || len(expected) != sha256.Size || subtle.ConstantTimeCompare(expected, fingerprint) != 1 {
		return nil, fmt.Errorf("vault master key does not match the established key")
	}
	return &Vault{db: db, aead: aead}, nil
}

func checkName(name string) error {
	if !secretName.MatchString(name) {
		return fmt.Errorf("%w: secret name must be 1–128 ASCII letters, digits, underscores or hyphens, starting with a letter or digit", model.ErrInvalid)
	}
	return nil
}

// Put encrypts before crossing the storage boundary. The envelope contains a
// version byte, a fresh random GCM nonce and authenticated ciphertext. Binding
// the name as AAD makes swapping two database secret values fail closed.
func (v *Vault) Put(ctx context.Context, name, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	envelope, err := v.EncryptSecret(name, value)
	if err != nil {
		return err
	}
	if err := v.db.PutSecret(ctx, name, envelope); err != nil {
		return storageError(ctx, err, "cannot store encrypted secret")
	}
	return nil
}

func (v *Vault) PutAtRevision(ctx context.Context, name, value string, revision int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if revision < 0 {
		return model.ErrInvalid
	}
	envelope, err := v.EncryptSecret(name, value)
	if err != nil {
		return err
	}
	if err := v.db.PutSecretAtRevision(ctx, name, envelope, revision); err != nil {
		if revision == 0 && errors.Is(err, model.ErrConflict) {
			// A create acknowledgement can be lost. Retrying the same reference
			// and exact value is safe, but must never rotate an existing secret.
			existing, readErr := v.Resolve(ctx, name)
			if readErr != nil && !errors.Is(readErr, model.ErrNotFound) {
				return storageError(ctx, readErr, "cannot verify encrypted secret creation")
			}
			if readErr == nil && subtle.ConstantTimeCompare([]byte(existing), []byte(value)) == 1 {
				return nil
			}
		}
		return storageError(ctx, err, "cannot store encrypted secret")
	}
	return nil
}

func (v *Vault) DeleteAtRevision(ctx context.Context, name string, revision int64) error {
	if err := checkName(name); err != nil {
		return err
	}
	if revision < 1 {
		return model.ErrInvalid
	}
	if err := v.db.DeleteSecretAtRevision(ctx, name, revision); err != nil {
		return storageError(ctx, err, "cannot delete encrypted secret")
	}
	return nil
}

// EncryptSecret prepares authenticated ciphertext without writing it. Callers
// commit a new secret and its configuration reference in one transaction.
func (v *Vault) EncryptSecret(name, value string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if len(value) == 0 || len(value) > MaxSecretBytes {
		return nil, fmt.Errorf("%w: secret value must contain 1–65536 bytes", model.ErrInvalid)
	}
	nonceSize := v.aead.NonceSize()
	envelope := make([]byte, 1+nonceSize, 1+nonceSize+len(value)+v.aead.Overhead())
	envelope[0] = envelopeVersion
	if _, err := rand.Read(envelope[1:]); err != nil {
		return nil, fmt.Errorf("cannot generate secret encryption nonce")
	}
	plaintext := []byte(value)
	envelope = v.aead.Seal(envelope, envelope[1:1+nonceSize], plaintext, []byte(name))
	clear(plaintext)
	return envelope, nil
}

// Resolve is deliberately separate from List: callers must explicitly request
// a named value, and no environment or plaintext fallback exists.
func (v *Vault) Resolve(ctx context.Context, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	envelope, err := v.db.Secret(ctx, name)
	if err != nil {
		return "", storageError(ctx, err, "cannot read encrypted secret")
	}
	nonceSize := v.aead.NonceSize()
	if len(envelope) < 1+nonceSize+v.aead.Overhead() || len(envelope) > 1+nonceSize+MaxSecretBytes+v.aead.Overhead() || envelope[0] != envelopeVersion {
		return "", fmt.Errorf("encrypted secret is invalid")
	}
	plaintext, err := v.aead.Open(nil, envelope[1:1+nonceSize], envelope[1+nonceSize:], []byte(name))
	if err != nil {
		return "", fmt.Errorf("encrypted secret authentication failed")
	}
	if len(plaintext) == 0 {
		clear(plaintext)
		return "", fmt.Errorf("encrypted secret is empty")
	}
	value := string(plaintext)
	clear(plaintext)
	return value, nil
}

func (v *Vault) Delete(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if err := v.db.DeleteSecret(ctx, name); err != nil {
		return storageError(ctx, err, "cannot delete encrypted secret")
	}
	return nil
}

func (v *Vault) List(ctx context.Context) ([]model.SecretInfo, error) {
	items, err := v.db.ListSecrets(ctx)
	if err != nil {
		return nil, storageError(ctx, err, "cannot list secret metadata")
	}
	if items == nil {
		items = []model.SecretInfo{}
	}
	return items, nil
}

func storageError(ctx context.Context, err error, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("%w: secret is unavailable", model.ErrNotFound)
	}
	if errors.Is(err, model.ErrConflict) {
		return model.ErrConflict
	}
	return errors.New(message)
}
