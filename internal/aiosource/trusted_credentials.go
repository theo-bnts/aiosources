package aiosource

import (
	"context"
	"sort"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Remembered personal credentials are scoped to one live account generation.
// They cannot be replayed as a public credential or by another trusted account.
func (s *Service) ResolveTrustedCredential(ctx context.Context, grant model.TrustedGrant, audience string, changes PublicCredentialChanges) (*PublicInstallation, error) {
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil {
		return nil, trustedAccessError(err)
	}
	copy := grant
	visitor := &PublicInstallation{Trusted: &copy, Version: 1, Audience: audience, Generation: grant.Generation, Services: map[string]string{}, Announces: map[string]string{}}
	if changes.Credential != "" {
		value, e := s.OpenTrustedConnection(ctx, audience, "credentials", changes.Credential)
		if e != nil {
			return nil, e
		}
		if value.UserID != grant.UserID || value.Generation != grant.Generation {
			return nil, ErrAuth
		}
		visitor.Services = value.Services
		visitor.Announces, err = s.retainedPersonalAnnounces(ctx, model.PublicAccess{CredentialPolicy: user.CredentialPolicy}, value.Announces)
		clear(value.Announces)
		if err != nil {
			clear(visitor.Services)
			return nil, err
		}
	}
	if err = applyCredentialChanges(visitor, changes); err == nil {
		err = s.personalCredentials(ctx, model.PublicAccess{CredentialPolicy: user.CredentialPolicy}, visitor)
	}
	if err != nil {
		clear(visitor.Services)
		clear(visitor.Announces)
		return nil, err
	}
	return visitor, nil
}

func (s *Service) TrustedBrowserCredential(ctx context.Context, grant model.TrustedGrant, audience string, visitor *PublicInstallation) (PublicBrowserCredential, error) {
	result := PublicBrowserCredential{Services: []string{}, Announces: []string{}}
	user, err := s.DB.TrustedUser(ctx, grant)
	if err != nil {
		return result, trustedAccessError(err)
	}
	result.InstanceServices = user.UseInstanceDebrid
	for _, p := range PublicProviders {
		if visitor.Services[p.ID] != "" {
			result.Services = append(result.Services, p.ID)
		}
	}
	for id := range visitor.Announces {
		result.Announces = append(result.Announces, id)
	}
	sort.Strings(result.Announces)
	// A browser credential may also authorize Search with instance services.
	result.Credential, err = s.CreateTrustedConnectionWithCredentials(ctx, grant, audience, "credentials", model.StreamPreferences{}, model.PublicTorznabOptions{}, model.PublicCatalogOptions{}, visitor)
	return result, err
}

func (s *Service) ValidatePersonalServices(ctx context.Context, visitor *PublicInstallation, required bool) error {
	access, err := s.visitorCredentialAccess(ctx, visitor)
	if err != nil {
		return err
	}
	if access.UseInstanceDebrid {
		return nil
	}
	if required && !validPublicKeys(visitor.Services) {
		return model.ErrInvalid
	}
	for _, p := range PublicProviders {
		if key := visitor.Services[p.ID]; key != "" {
			if err = s.Providers.ValidateKey(ctx, p.ID, key); err != nil {
				return err
			}
		}
	}
	return nil
}
