package aiosource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

const publicCatalogPurpose = "public-catalog-v1"
const MaxPublicCatalogSize = 6000

type publicCatalogInstallation struct {
	Version           int    `json:"version"`
	Generation        int64  `json:"generation"`
	CatalogGeneration int64  `json:"catalog_generation"`
	Audience          string `json:"audience"`
	Options           string `json:"options"`
}

func publicCatalogAvailable(access model.PublicAccess) bool {
	return access.Enabled && access.ExposeSources && access.CatalogEnabled
}
func EncodePublicCatalogOptions(options model.PublicCatalogOptions) (string, error) {
	if !store.ValidPublicCatalogOptions(options) {
		return "", model.ErrInvalid
	}
	data, err := json.Marshal(store.NormalizePublicCatalogOptions(options))
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(data)
	if len(value) > MaxPublicCatalogSize {
		return "", model.ErrInvalid
	}
	return value, nil
}
func DecodePublicCatalogOptions(value string) (model.PublicCatalogOptions, error) {
	var options model.PublicCatalogOptions
	if len(value) == 0 || len(value) > MaxPublicCatalogSize {
		return options, model.ErrInvalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(data) > 4096 {
		return options, model.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&options) != nil || decoder.Decode(new(any)) != io.EOF || !store.ValidPublicCatalogOptions(options) {
		return options, model.ErrInvalid
	}
	canonical, err := EncodePublicCatalogOptions(options)
	if err != nil || canonical != value {
		return options, model.ErrInvalid
	}
	return options, nil
}
func publicCatalogOptionsHash(options string) string {
	value := sha256.Sum256([]byte(options))
	return hex.EncodeToString(value[:])
}

// The endpoint carries non-secret data choices; its separately sealed password
// contains only audience, revocation counters and their integrity binding.
// No account key, announce URL or saved visitor row is involved.
func (s *Service) CreatePublicCatalog(ctx context.Context, audience string, options model.PublicCatalogOptions) (configuration, password string, err error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return "", "", err
	}
	if !publicCatalogAvailable(access) {
		return "", "", model.ErrNotFound
	}
	if audience == "" {
		return "", "", model.ErrInvalid
	}
	configuration, err = EncodePublicCatalogOptions(options)
	if err != nil {
		return "", "", err
	}
	policy, err := s.DB.PublicCatalogPolicy(ctx)
	if err != nil {
		return "", "", err
	}
	if _, err = store.ResolvePublicCatalogShare(policy, options); err != nil {
		return "", "", err
	}
	data, err := json.Marshal(publicCatalogInstallation{Version: 1, Generation: access.Generation, CatalogGeneration: access.CatalogGeneration, Audience: audience, Options: publicCatalogOptionsHash(configuration)})
	if err != nil {
		return "", "", err
	}
	defer clear(data)
	sealed, err := s.Vault.Seal(publicCatalogPurpose, data)
	if err != nil {
		return "", "", err
	}
	return configuration, base64.RawURLEncoding.EncodeToString(sealed), nil
}
func (s *Service) OpenPublicCatalog(ctx context.Context, audience, configuration, password string) (model.PublicCatalogGrant, error) {
	var grant model.PublicCatalogGrant
	if len(password) < 32 || len(password) > MaxPublicCatalogSize {
		return grant, ErrAuth
	}
	options, err := DecodePublicCatalogOptions(configuration)
	if err != nil {
		return grant, ErrAuth
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return grant, err
	}
	if !publicCatalogAvailable(access) {
		return grant, ErrAuth
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(password)
	if err != nil {
		return grant, ErrAuth
	}
	data, err := s.Vault.Open(publicCatalogPurpose, sealed)
	if err != nil {
		return grant, ErrAuth
	}
	defer clear(data)
	var value publicCatalogInstallation
	if json.Unmarshal(data, &value) != nil || value.Version != 1 || value.Audience != audience || value.Generation != access.Generation || value.CatalogGeneration != access.CatalogGeneration || value.Options != publicCatalogOptionsHash(configuration) {
		return grant, ErrAuth
	}
	grant = model.PublicCatalogGrant{PublicCatalogOptions: options, Generation: value.Generation, CatalogGeneration: value.CatalogGeneration}
	return grant, nil
}
