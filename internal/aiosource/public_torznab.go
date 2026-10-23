package aiosource

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

const publicTorznabPurpose = "public-torznab-v1"
const MaxPublicTorznabSize = 6000 // Reserve space for standard client query parameters.

type PublicTorznabInstallation struct {
	Version            int                        `json:"version"`
	Generation         int64                      `json:"generation"`
	ProtocolGeneration int64                      `json:"protocol_generation"`
	Audience           string                     `json:"audience"`
	Announces          map[string]string          `json:"announces,omitempty"`
	Options            model.PublicTorznabOptions `json:"-"`
	Access             model.PublicAccess         `json:"-"`
}

func ValidPublicTorznabOptions(o model.PublicTorznabOptions) bool {
	cache := model.TorznabPreferences{Services: o.Services, CachedOnly: o.CachedOnly}
	if !ValidStreamPreferences(model.StreamPreferences{Sources: o.Sources}) || !cache.Valid() || len(o.MediaTypes) > 3 {
		return false
	}
	for i, m := range o.MediaTypes {
		if !slices.Contains([]string{"movie", "tv", "other"}, m) || slices.Contains(o.MediaTypes[:i], m) {
			return false
		}
	}
	b, err := json.Marshal(o)
	return err == nil && len(b) <= 4096
}

func (s *Service) CreatePublicTorznab(ctx context.Context, audience string, options model.PublicTorznabOptions, announces map[string]string) (string, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return "", err
	}
	if !access.Enabled || !access.TorznabEnabled {
		return "", model.ErrNotFound
	}
	if audience == "" || !ValidPublicTorznabOptions(options) || (!access.ExposeSources && len(options.Sources) > 0) {
		return "", model.ErrInvalid
	}
	if err = s.validatePublicAnnounces(ctx, access, announces); err != nil {
		return "", err
	}
	data, err := json.Marshal(PublicTorznabInstallation{Version: 1, Generation: access.Generation, ProtocolGeneration: access.TorznabGeneration, Audience: audience, Announces: announces, Options: options})
	if err != nil {
		return "", model.ErrInvalid
	}
	defer clear(data)
	sealed, err := s.Vault.Seal(publicTorznabPurpose, data)
	if err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(sealed)
	configuration, err := WithPublicTorznabOptions(key, options)
	if err != nil {
		return "", err
	}
	if err := s.CheckPublicConfigurationSize(ctx, "torznab", configuration); err != nil {
		return "", err
	}
	return configuration, nil
}

func (s *Service) OpenPublicTorznab(ctx context.Context, audience, value string) (*PublicTorznabInstallation, error) {
	if len(value) < 32 || len(value) > MaxPublicTorznabSize {
		return nil, ErrAuth
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled || !access.TorznabEnabled {
		return nil, ErrAuth
	}
	credential, options, err := PublicTorznabOptions(value)
	if err != nil {
		return nil, ErrAuth
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(credential)
	if err != nil {
		return nil, ErrAuth
	}
	data, err := s.Vault.Open(publicTorznabPurpose, sealed)
	if err != nil {
		return nil, ErrAuth
	}
	defer clear(data)
	var v PublicTorznabInstallation
	if json.Unmarshal(data, &v) != nil || v.Version != 1 || v.Generation != access.Generation || v.ProtocolGeneration != access.TorznabGeneration || v.Audience != audience || !validPublicAnnounces(v.Announces) {
		clear(v.Announces)
		return nil, ErrAuth
	}
	v.Options = options
	v.Access = access
	if !access.ExposeSources {
		clear(v.Announces)
		v.Options.Sources = nil
	}
	return &v, nil
}

func (s *Service) PublicTorznabPolicy(ctx context.Context, v *PublicTorznabInstallation) (store.TorznabVisibility, map[string]string, error) {
	if v == nil {
		return store.TorznabVisibility{}, nil, ErrAuth
	}
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return store.TorznabVisibility{}, nil, err
	}
	announces, err := s.visitorAnnounces(ctx, c, &PublicInstallation{Generation: v.Generation, Announces: v.Announces})
	if err != nil {
		return store.TorznabVisibility{}, nil, err
	}
	policy := store.TorznabVisibility{Public: true, CacheAfter: time.Now().Add(-time.Duration(c.CacheMinutes) * time.Minute), UncachedSources: []string{}}
	for id, t := range c.Trackers {
		if !t.Private {
			policy.UncachedSources = append(policy.UncachedSources, id)
		}
	}
	for id := range announces {
		if !slices.Contains(policy.UncachedSources, id) {
			policy.UncachedSources = append(policy.UncachedSources, id)
		}
	}
	return policy, announces, nil
}

func (s *Service) ValidatePublicTorznab(ctx context.Context, v *PublicTorznabInstallation) error {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return err
	}
	if !access.Enabled || !access.TorznabEnabled || v == nil || access.Generation != v.Generation || access.TorznabGeneration != v.ProtocolGeneration || access.ExposeSources != v.Access.ExposeSources {
		return ErrAuth
	}
	return nil
}

// Filters share the Stremio URL convention. Only personal credentials are
// sealed; changing preferences never requires resubmitting an announce URL.
func WithPublicTorznabOptions(credential string, options model.PublicTorznabOptions) (string, error) {
	if !ValidPublicTorznabOptions(options) {
		return "", model.ErrInvalid
	}
	if len(options.Sources) == 0 && len(options.MediaTypes) == 0 && len(options.Services) == 0 && !options.CachedOnly {
		return credential, nil
	}
	data, err := json.Marshal(options)
	if err != nil {
		return "", err
	}
	value := credential + "~v1~" + base64.RawURLEncoding.EncodeToString(data)
	if len(value) > MaxPublicTorznabSize {
		return "", model.ErrInvalid
	}
	return value, nil
}
func PublicTorznabOptions(value string) (string, model.PublicTorznabOptions, error) {
	var options model.PublicTorznabOptions
	parts := strings.Split(value, "~")
	if len(parts) == 1 {
		return value, options, nil
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] != "v1" {
		return "", options, model.ErrInvalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(data) > 4096 {
		return "", options, model.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&options) != nil || decoder.Decode(new(any)) != io.EOF || !ValidPublicTorznabOptions(options) {
		return "", options, model.ErrInvalid
	}
	return parts[0], options, nil
}
