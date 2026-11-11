package aiosource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

const trustedConnectionPurpose = "trusted-connection-v1"

func trustedVisitorID(visitor *PublicInstallation) string {
	if visitor != nil && visitor.Trusted != nil {
		return visitor.Trusted.UserID
	}
	return ""
}

// Trusted URLs carry account identity and choices, never instance credentials.
// Distinct authenticated encryption prevents use as a public visitor token.
type TrustedConnection struct {
	Version     int                        `json:"version"`
	UserID      string                     `json:"user"`
	Generation  int64                      `json:"generation"`
	Audience    string                     `json:"audience"`
	Feature     string                     `json:"feature"`
	Preferences model.StreamPreferences    `json:"preferences,omitempty"`
	Torznab     model.PublicTorznabOptions `json:"torznab,omitempty"`
	Catalog     model.PublicCatalogOptions `json:"catalog,omitempty"`
	Services    map[string]string          `json:"services,omitempty"`
	Announces   map[string]string          `json:"announces,omitempty"`
}

func (s *Service) CreateTrustedConnection(ctx context.Context, grant model.TrustedGrant, audience, feature string, prefs model.StreamPreferences, tz model.PublicTorznabOptions, catalog model.PublicCatalogOptions) (string, error) {
	return s.CreateTrustedConnectionWithCredentials(ctx, grant, audience, feature, prefs, tz, catalog, nil)
}

func (s *Service) CreateTrustedConnectionWithCredentials(ctx context.Context, grant model.TrustedGrant, audience, feature string, prefs model.StreamPreferences, tz model.PublicTorznabOptions, catalog model.PublicCatalogOptions, personal *PublicInstallation) (string, error) {
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil {
		return "", err
	}
	if feature != "credentials" && !user.Allows(feature) {
		return "", ErrAuth
	}
	if audience == "" || !ValidStreamPreferences(prefs) || !ValidPublicTorznabOptions(tz) || !store.ValidPublicCatalogOptions(catalog) {
		return "", model.ErrInvalid
	}
	if !user.ExposeSources && (len(prefs.Sources) > 0 || len(tz.Sources) > 0) {
		return "", model.ErrInvalid
	}
	if !user.ExposeSources {
		for _, rule := range append(append([]model.ResultSort{}, prefs.Sorting...), tz.Sorting...) {
			if rule.Field == "source" {
				return "", model.ErrInvalid
			}
		}
	}
	if feature == "catalog" {
		policy, e := s.DB.PublicCatalogPolicy(ctx)
		if e != nil {
			return "", e
		}
		if _, e = store.ResolvePublicCatalogShare(policy, catalog); e != nil {
			return "", e
		}
	}
	value := TrustedConnection{Version: 1, UserID: user.ID, Generation: user.Generation, Audience: audience, Feature: feature, Preferences: prefs, Torznab: tz, Catalog: catalog}
	if personal != nil {
		if personal.Trusted == nil || *personal.Trusted != grant || personal.Audience != audience {
			return "", ErrAuth
		}
		if err := s.personalCredentials(ctx, model.PublicAccess{CredentialPolicy: user.CredentialPolicy, ExposeSources: user.ExposeSources}, personal); err != nil {
			return "", err
		}
		if feature != "catalog" {
			value.Announces = personal.Announces
		}
		if feature != "torznab" && feature != "catalog" {
			value.Services = personal.Services
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	defer clear(data)
	if len(data) > 6000 {
		return "", model.ErrConfigurationLimit
	}
	sealed, err := s.Vault.Seal(trustedConnectionPurpose, data)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(sealed)
	if feature == "stremio" || feature == "torznab" {
		if err := s.CheckPublicConfigurationSize(ctx, feature, token); err != nil {
			return "", err
		}
	}
	return token, nil
}

func (s *Service) OpenTrustedConnection(ctx context.Context, audience, feature, token string) (TrustedConnection, error) {
	var value TrustedConnection
	if len(token) < 32 || len(token) > MaxPublicInstallationSize {
		return value, ErrAuth
	}
	var prefs model.StreamPreferences
	var tz model.PublicTorznabOptions
	var err error
	override := strings.Contains(token, "~")
	if feature == "stremio" {
		token, prefs, err = InstallationPreferences(token)
	} else if feature == "torznab" {
		token, tz, err = PublicTorznabOptions(token)
	}
	if err != nil {
		return value, ErrAuth
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return value, ErrAuth
	}
	data, err := s.Vault.Open(trustedConnectionPurpose, sealed)
	if err != nil {
		return value, ErrAuth
	}
	defer clear(data)
	if json.Unmarshal(data, &value) != nil || value.Version != 1 || value.Feature != feature || value.Audience != audience {
		return value, ErrAuth
	}
	user, err := s.DB.TrustedUser(ctx, model.TrustedGrant{UserID: value.UserID, Generation: value.Generation})
	if err != nil || feature != "credentials" && !user.Allows(feature) {
		return value, ErrAuth
	}
	if override {
		if feature == "stremio" {
			value.Preferences = prefs
		} else if feature == "torznab" {
			value.Torznab = tz
		}
	}
	if !user.ExposeSources {
		value.Preferences.Sources = nil
		value.Preferences.ResultPreferences = value.Preferences.ResultPreferences.WithHiddenSources()
		value.Torznab.Sources = nil
		value.Torznab.ResultPreferences = value.Torznab.ResultPreferences.WithHiddenSources()
	}
	return value, nil
}

func (s *Service) TrustedVisitor(ctx context.Context, grant model.TrustedGrant, audience, feature string, prefs model.StreamPreferences) (*PublicInstallation, error) {
	return s.TrustedVisitorWithCredentials(ctx, grant, audience, feature, prefs, nil, nil)
}
func (s *Service) TrustedVisitorWithCredentials(ctx context.Context, grant model.TrustedGrant, audience, feature string, prefs model.StreamPreferences, services, announces map[string]string) (*PublicInstallation, error) {
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil || !user.Allows(feature) {
		return nil, ErrAuth
	}
	if !ValidStreamPreferences(prefs) {
		return nil, model.ErrInvalid
	}
	if !user.ExposeSources {
		prefs.Sources = nil
		prefs.ResultPreferences = prefs.ResultPreferences.WithHiddenSources()
	}
	copy := grant
	visitor := &PublicInstallation{Trusted: &copy, Audience: audience, Generation: user.Generation, Preferences: prefs, Services: services, Announces: announces}
	if err = s.personalCredentials(ctx, model.PublicAccess{CredentialPolicy: user.CredentialPolicy, ExposeSources: user.ExposeSources}, visitor); err != nil {
		return nil, err
	}
	if err = s.resolveVisitorServices(ctx, visitor, user.CredentialPolicy); err != nil {
		clear(visitor.Services)
		return nil, err
	}
	return visitor, nil
}

func (s *Service) visitorAccess(ctx context.Context, visitor *PublicInstallation, feature string) (model.PublicAccess, error) {
	if visitor != nil && visitor.Trusted != nil {
		user, err := s.DB.TrustedUser(ctx, *visitor.Trusted)
		if err != nil || feature != "credentials" && !user.Allows(feature) {
			return model.PublicAccess{}, ErrAuth
		}
		return model.PublicAccess{CredentialPolicy: user.CredentialPolicy, Enabled: true, SearchEnabled: user.SearchEnabled, StremioEnabled: user.StremioEnabled, TorznabEnabled: user.TorznabEnabled, CatalogEnabled: user.CatalogEnabled, ExposeSources: user.ExposeSources, Generation: user.Generation}, nil
	}
	return s.DB.PublicAccess(ctx)
}

func (s *Service) TrustedSources(ctx context.Context, grant model.TrustedGrant) ([]PublicPrivateSource, error) {
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil {
		return nil, err
	}
	items := []PublicPrivateSource{}
	if !user.ExposeSources {
		return items, nil
	}
	for id, name := range s.sourceNames() {
		items = append(items, PublicPrivateSource{ID: id, Name: name})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	return items, nil
}

func (s *Service) TrustedPrivateSources(ctx context.Context, grant model.TrustedGrant) ([]PublicPrivateSource, error) {
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil {
		return nil, err
	}
	if !user.ExposeSources || user.UseInstanceAnnounces {
		return []PublicPrivateSource{}, nil
	}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.publicPrivateSources(config)
}
