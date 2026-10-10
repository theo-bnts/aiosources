package vault

import (
	"crypto/rand"
	"errors"
)

// Seal encrypts internal security material outside the ordinary named-secret API.
// Its domain-separated AAD cannot be supplied through vault CRUD names.
func (v *Vault) Seal(purpose string, plaintext []byte) ([]byte, error) {
	if v == nil || v.aead == nil || !internalPurpose(purpose) || len(plaintext) == 0 || len(plaintext) > MaxSecretBytes {
		return nil, errors.New("invalid internal secret")
	}
	n := v.aead.NonceSize()
	envelope := make([]byte, 1+n, 1+n+len(plaintext)+v.aead.Overhead())
	envelope[0] = envelopeVersion
	if _, err := rand.Read(envelope[1:]); err != nil {
		return nil, errors.New("cannot generate internal secret nonce")
	}
	return v.aead.Seal(envelope, envelope[1:1+n], plaintext, []byte("scraper/internal-seal/v1\x00"+purpose)), nil
}

// Open authenticates both ciphertext and its internal purpose. The caller clears
// the returned plaintext after use; callers never expose this through vault CRUD.
func (v *Vault) Open(purpose string, envelope []byte) ([]byte, error) {
	if v == nil || v.aead == nil || !internalPurpose(purpose) {
		return nil, errors.New("invalid internal secret")
	}
	n := v.aead.NonceSize()
	if len(envelope) < 1+n+v.aead.Overhead() || len(envelope) > 1+n+MaxSecretBytes+v.aead.Overhead() || envelope[0] != envelopeVersion {
		return nil, errors.New("invalid internal secret envelope")
	}
	plaintext, err := v.aead.Open(nil, envelope[1:1+n], envelope[1+n:], []byte("scraper/internal-seal/v1\x00"+purpose))
	if err != nil || len(plaintext) == 0 {
		clear(plaintext)
		return nil, errors.New("internal secret authentication failed")
	}
	return plaintext, nil
}

func internalPurpose(purpose string) bool {
	return purpose == "admin-mfa-pending-v1" || purpose == "admin-mfa-active-v1"
}
