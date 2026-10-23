package aiosource

import (
	"context"
	"sort"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Only authenticated ciphertext and non-secret field identifiers are returned.
// The application stores no visitor profile; the browser carries this value.
type PublicBrowserCredential struct {
	Credential string   `json:"credential"`
	Services   []string `json:"services"`
	Announces  []string `json:"announces"`
}

type PublicCredentialChanges struct {
	Credential      string            `json:"credential"`
	Services        map[string]string `json:"services"`
	Announces       map[string]string `json:"announces"`
	RemoveServices  []string          `json:"remove_services"`
	RemoveAnnounces []string          `json:"remove_announces"`
}

// Merge only the visitor's encrypted configuration. Administrator credentials
// are never read. Withdrawn source permissions also retire remembered announces.
func (s *Service) ResolvePublicCredential(ctx context.Context, audience string, changes PublicCredentialChanges) (*PublicInstallation, error) {
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return nil, err
	}
	if !access.Enabled {
		return nil, model.ErrNotFound
	}
	saved := &PublicInstallation{Version: 1, Generation: access.Generation, Audience: audience, Services: map[string]string{}, Announces: map[string]string{}}
	if changes.Credential != "" {
		saved, err = s.openPublicCredential(ctx, audience, changes.Credential, false)
		if err != nil {
			return nil, err
		}
		c, readErr := s.DB.AIOConfig(ctx)
		if readErr != nil {
			clear(saved.Services)
			clear(saved.Announces)
			return nil, readErr
		}
		permitted, readErr := s.visitorAnnounces(ctx, c, saved)
		clear(saved.Announces)
		if readErr != nil {
			clear(saved.Services)
			return nil, readErr
		}
		saved.Announces = permitted
	}
	valid := false
	defer func() {
		if !valid {
			clear(saved.Services)
			clear(saved.Announces)
		}
	}()
	if len(changes.RemoveServices) > len(PublicProviders) || len(changes.RemoveAnnounces) > 128 {
		return nil, model.ErrInvalid
	}
	for _, service := range changes.RemoveServices {
		known := false
		for _, provider := range PublicProviders {
			known = known || service == provider.ID
		}
		if !known {
			return nil, model.ErrInvalid
		}
		delete(saved.Services, service)
	}
	for _, source := range changes.RemoveAnnounces {
		if !model.ValidSourceID(source) {
			return nil, model.ErrInvalid
		}
		delete(saved.Announces, source)
	}
	if saved.Services == nil {
		saved.Services = map[string]string{}
	}
	if saved.Announces == nil {
		saved.Announces = map[string]string{}
	}
	for service, key := range changes.Services {
		saved.Services[service] = key
	}
	for source, value := range changes.Announces {
		saved.Announces[source] = value
	}
	valid = true
	return saved, nil
}

// Empty-service credentials can remember Torznab announces, but cannot authorize
// Stremio or Search: those entry points still require a validated service key.
func (s *Service) PublicBrowserCredential(ctx context.Context, audience string, installation *PublicInstallation) (PublicBrowserCredential, error) {
	result := PublicBrowserCredential{Services: []string{}, Announces: []string{}}
	access, err := s.DB.PublicAccess(ctx)
	if err != nil {
		return result, err
	}
	if installation == nil || !access.Enabled || access.Generation != installation.Generation || audience == "" || installation.Audience != audience {
		return result, ErrAuth
	}
	if len(installation.Services) > 0 && !validPublicKeys(installation.Services) {
		return result, model.ErrInvalid
	}
	if err := s.validatePublicAnnounces(ctx, access, installation.Announces); err != nil {
		return result, err
	}
	for _, provider := range PublicProviders {
		if installation.Services[provider.ID] != "" {
			result.Services = append(result.Services, provider.ID)
		}
	}
	for source := range installation.Announces {
		result.Announces = append(result.Announces, source)
	}
	sort.Strings(result.Announces)
	if len(result.Services) == 0 && len(result.Announces) == 0 {
		return result, nil
	}
	profile := *installation
	profile.StremioGeneration = 0
	result.Credential, err = s.sealPublicInstallation(ctx, profile)
	return result, err
}
