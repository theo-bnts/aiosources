package aiosource

import (
	"context"
	"sort"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (v *PublicInstallation) withAllowedServices(prefs model.StreamPreferences, mediaType string) *PublicInstallation {
	if v == nil {
		return nil
	}
	copy := *v
	copy.Services = map[string]string{}
	for service, key := range v.Services {
		if AllowsInstallation(prefs, "", service, mediaType) {
			copy.Services[service] = key
		}
	}
	return &copy
}

func browserCredentialSummary(visitor *PublicInstallation, instanceServices bool) PublicBrowserCredential {
	result := PublicBrowserCredential{InstanceServices: instanceServices, Services: []string{}, Announces: []string{}}
	if visitor == nil {
		return result
	}
	for _, provider := range PublicProviders {
		if visitor.Services[provider.ID] != "" {
			result.Services = append(result.Services, provider.ID)
		}
	}
	for source := range visitor.Announces {
		result.Announces = append(result.Announces, source)
	}
	sort.Strings(result.Announces)
	return result
}

func (s *Service) validateServiceKeys(ctx context.Context, services map[string]string) error {
	for _, provider := range PublicProviders {
		if key := services[provider.ID]; key != "" {
			if err := s.Providers.ValidateKey(ctx, provider.ID, key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) filterPersonalAnnounces(c model.AIOConfig, announces map[string]string) map[string]string {
	retained := map[string]string{}
	if len(announces) == 0 {
		return retained
	}
	sources, err := s.publicPrivateSources(c)
	if err != nil {
		// An unreadable definition cannot authorize an uncached download, but
		// must not prevent existing cached playback through an encrypted link.
		return retained
	}
	for _, source := range sources {
		if value := announces[source.ID]; value != "" {
			retained[source.ID] = value
		}
	}
	return retained
}

// Resolve authorization separately from credentials: an authenticated user is
// not implicitly entitled to instance keys, and a public visitor may be.
func (s *Service) visitorCredentialAccess(ctx context.Context, visitor *PublicInstallation) (model.PublicAccess, error) {
	if visitor == nil {
		return model.PublicAccess{}, ErrAuth
	}
	if visitor.Trusted != nil {
		user, err := s.DB.TrustedUser(ctx, *visitor.Trusted)
		if err != nil {
			return model.PublicAccess{}, trustedAccessError(err)
		}
		return model.PublicAccess{CredentialPolicy: user.CredentialPolicy, Enabled: true, Generation: user.Generation}, nil
	}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return access, err
	}
	if !access.Enabled || access.Generation != visitor.Generation {
		return access, ErrAuth
	}
	return access, nil
}

// Instance values exist only in request memory. Never call this before sealing
// a visitor configuration, which must contain personal credentials only.
func (s *Service) resolveVisitorServices(ctx context.Context, visitor *PublicInstallation, policy model.CredentialPolicy) error {
	if !policy.UseInstanceDebrid {
		return nil
	}
	clear(visitor.Services)
	visitor.Services = map[string]string{}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	for service, ref := range config.DebridSecrets() {
		if ref == "" || !AllowsInstallation(visitor.Preferences, "", service, "") {
			continue
		}
		key, e := s.key(ctx, ref)
		if e != nil {
			clear(visitor.Services)
			return e
		}
		visitor.Services[service] = key
	}
	return nil
}

func (s *Service) personalCredentials(ctx context.Context, access model.PublicAccess, visitor *PublicInstallation) error {
	if access.UseInstanceDebrid {
		clear(visitor.Services)
	}
	if access.UseInstanceAnnounces {
		clear(visitor.Announces)
	}
	if len(visitor.Services) > 0 && !validPublicKeys(visitor.Services) {
		return model.ErrInvalid
	}
	return s.validatePublicAnnounces(ctx, access, visitor.Announces)
}

// Retain only personal announces still permitted by live source definitions.
// This never resolves an instance secret, even when instance credentials are enabled.
func (s *Service) retainedPersonalAnnounces(ctx context.Context, access model.PublicAccess, announces map[string]string) (map[string]string, error) {
	if access.UseInstanceAnnounces || len(announces) == 0 {
		return map[string]string{}, nil
	}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.filterPersonalAnnounces(config, announces), nil
}

func (s *Service) PublicInstanceSearch(ctx context.Context, audience string) (*PublicInstallation, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled || !access.SearchEnabled || !access.UseInstanceDebrid {
		return nil, ErrAuth
	}
	visitor := &PublicInstallation{Generation: access.Generation, Audience: audience, Services: map[string]string{}}
	if err = s.resolveVisitorServices(ctx, visitor, access.CredentialPolicy); err != nil {
		return nil, err
	}
	return visitor, nil
}
