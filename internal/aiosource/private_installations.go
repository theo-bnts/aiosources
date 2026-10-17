package aiosource

import (
	"context"
	"errors"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/store"
)

// Named private installations deliberately have no URL settings. Their current
// database configuration is authoritative, including playback authorization.
func PrivateConnectionKey(value string) (id, key string, named bool, err error) {
	if !strings.HasPrefix(value, "s1.") {
		return "", "", false, nil
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 || len(parts[1]) != 36 || len(parts[2]) != 43 || strings.Contains(value, "~") {
		return "", "", true, ErrAuth
	}
	return parts[1], parts[2], true, nil
}
func (s *Service) privateConnection(ctx context.Context, token string) (model.Share, error) {
	id, key, named, err := PrivateConnectionKey(token)
	if err != nil || !named {
		return model.Share{}, ErrAuth
	}
	connection, err := s.DB.StremioConnection(ctx, id, key)
	if errors.Is(err, store.ErrCatalogAuthentication) {
		return model.Share{}, ErrAuth
	}
	return connection, err
}
func (s *Service) PrivatePreferences(ctx context.Context, token string) (model.StreamPreferences, error) {
	if strings.HasPrefix(token, "s1.") {
		c, err := s.privateConnection(ctx, token)
		if err != nil {
			return model.StreamPreferences{}, err
		}
		return connectionPreferences(c), nil
	}
	_, p, err := InstallationPreferences(token)
	return p, err
}

func connectionPreferences(connection model.Share) model.StreamPreferences {
	preferences := *connection.StreamPreferences
	if connection.Scope == "selected" {
		preferences.Sources = append([]string{}, connection.SourceIDs...)
	}
	return preferences
}

func (s *Service) PrivateManifest(ctx context.Context, token string) (map[string]any, error) {
	if strings.HasPrefix(token, "s1.") {
		c, err := s.privateConnection(ctx, token)
		if err != nil {
			return nil, err
		}
		// Clients may retain the manifest. Advertise the stable supported types;
		// the current saved media filter is enforced on every stream/play request.
		// Changing it therefore does not require reinstalling the addon.
		manifest := Manifest()
		manifest["id"] = "org.aiosource.v2.connection." + strings.ReplaceAll(c.ID, "-", "")
		manifest["name"] = "AIOSources · " + c.Name
		return manifest, nil
	}
	_, preferences, err := InstallationPreferences(token)
	if err != nil {
		return nil, err
	}
	return InstallationManifest(preferences), nil
}
