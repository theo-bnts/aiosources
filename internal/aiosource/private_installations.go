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
func PrivateConnectionKey(value string) (id, key string, err error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != "s1" || len(parts[1]) != 36 || len(parts[2]) != 43 || strings.Contains(value, "~") {
		return "", "", ErrAuth
	}
	return parts[1], parts[2], nil
}
func (s *Service) privateConnection(ctx context.Context, token string) (model.Share, error) {
	id, key, err := PrivateConnectionKey(token)
	if err != nil {
		return model.Share{}, ErrAuth
	}
	connection, err := s.DB.StremioConnection(ctx, id, key)
	if errors.Is(err, store.ErrCatalogAuthentication) {
		return model.Share{}, ErrAuth
	}
	return connection, err
}
func (s *Service) PrivatePreferences(ctx context.Context, token string) (model.StreamPreferences, error) {
	c, err := s.privateConnection(ctx, token)
	if err != nil {
		return model.StreamPreferences{}, err
	}
	return connectionPreferences(c), nil
}

func connectionPreferences(connection model.Share) model.StreamPreferences {
	preferences := *connection.StreamPreferences
	if connection.Scope == "selected" {
		preferences.Sources = append([]string{}, connection.SourceIDs...)
	}
	return preferences
}

func (s *Service) PrivateManifest(ctx context.Context, token string) (map[string]any, error) {
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
