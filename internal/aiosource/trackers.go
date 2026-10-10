package aiosource

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Validate both when selecting a secret and when using it: vault values can be
// rotated independently of the configuration. Never include the value in errors.
func validateAnnounce(value string) error {
	invalid := func() error {
		return fmt.Errorf("%w: announce secret must contain a complete HTTP, HTTPS or UDP tracker URL", model.ErrInvalid)
	}
	if value == "" || len(value) > 8192 {
		return invalid()
	}
	for _, b := range []byte(value) {
		if b <= ' ' || b == 127 {
			return invalid()
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "udp") {
		return invalid()
	}
	return nil
}

func (s *Service) trackerAnnounce(ctx context.Context, t model.AIOTracker) (string, error) {
	if t.AnnounceSecret == "" {
		return "", nil
	}
	value, err := s.Vault.Resolve(ctx, t.AnnounceSecret)
	if err != nil {
		return "", err
	}
	if err = validateAnnounce(value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) migrateTrackerSecrets(ctx context.Context) error {
	c, err := s.DB.AIOConfig(ctx)
	if err != nil {
		return err
	}
	for _, t := range c.Trackers {
		if t.Announce != "" || t.PasskeySecret != "" {
			return s.SaveConfig(ctx, c)
		}
	}
	return nil
}

func (s *Service) saveTrackerSecrets(ctx context.Context, c model.AIOConfig) error {
	encrypted := map[string][]byte{}
	trackers := make(map[string]model.AIOTracker, len(c.Trackers))
	for id, t := range c.Trackers {
		if t.Announce != "" {
			if err := validateAnnounce(t.Announce); err != nil {
				return err
			}
			key, err := s.key(ctx, t.PasskeySecret)
			if err != nil {
				return err
			}
			// Preserve the former suffix convention only while importing old
			// settings. New settings always use the complete URL verbatim.
			value := strings.TrimRight(t.Announce, "/") + "/" + url.PathEscape(key)
			if err = validateAnnounce(value); err != nil {
				return err
			}
			var random [12]byte
			if _, err = rand.Read(random[:]); err != nil {
				return fmt.Errorf("cannot name migrated announce secret")
			}
			t.AnnounceSecret = "aiosource_announce_" + hex.EncodeToString(random[:])
			ciphertext, err := s.Vault.EncryptSecret(t.AnnounceSecret, value)
			if err != nil {
				return err
			}
			encrypted[t.AnnounceSecret] = ciphertext
			t.Announce, t.PasskeySecret = "", ""
		} else if _, err := s.trackerAnnounce(ctx, t); err != nil {
			return err
		}
		trackers[id] = t
	}
	c.Trackers = trackers
	return s.DB.SetAIOConfigAndSecrets(ctx, c, encrypted)
}
