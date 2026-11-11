package aiosource

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Resolve authorization separately from credentials: an authenticated user is
// not implicitly entitled to instance keys, and a public visitor may be.
func (s *Service) visitorCredentialAccess(ctx context.Context, visitor *PublicInstallation) (model.PublicAccess, error) {
	if visitor == nil {
		return model.PublicAccess{}, ErrAuth
	}
	if visitor.Trusted != nil {
		user, err := s.DB.TrustedUser(ctx, *visitor.Trusted)
		if err != nil {
			return model.PublicAccess{}, ErrAuth
		}
		return model.PublicAccess{CredentialPolicy: user.CredentialPolicy, Enabled: true, ExposeSources: user.ExposeSources, Generation: user.Generation}, nil
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
	if !access.ExposeSources || access.UseInstanceAnnounces || len(announces) == 0 {
		return map[string]string{}, nil
	}
	config, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return nil, err
	}
	sources, err := s.publicPrivateSources(config)
	if err != nil {
		return map[string]string{}, nil
	}
	retained := map[string]string{}
	for _, source := range sources {
		if value := announces[source.ID]; value != "" {
			retained[source.ID] = value
		}
	}
	return retained, nil
}
