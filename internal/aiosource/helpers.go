package aiosource

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func itoa64(n int64) string       { return strconv.FormatInt(n, 10) }
func strconvPair(a, b int) string { return strconv.Itoa(a) + ":" + strconv.Itoa(b) }
func fingerprint(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

var hashRE = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

var regexpIMDb = regexp.MustCompile(`^tt[0-9]+$`)

func (s *Service) sealVaultJSON(purpose string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	defer clear(data)
	sealed, err := s.Vault.Seal(purpose, data)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) openVaultJSON(purpose, token string, out any) error {
	if len(token) < 32 || strings.Contains(token, "~") {
		return model.ErrInvalid
	}
	sealed, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return model.ErrInvalid
	}
	data, err := s.Vault.Open(purpose, sealed)
	if err != nil {
		return model.ErrInvalid
	}
	defer clear(data)
	if json.Unmarshal(data, out) != nil {
		return model.ErrInvalid
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
