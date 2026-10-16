package aiosource

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
	"golang.org/x/text/language"
)

func NormalizeLanguage(value string) string {
	if len(value) > 64 {
		return ""
	}
	tag, err := language.Parse(value)
	if err != nil {
		return ""
	}
	base, _ := tag.Base()
	if base.String() == "und" {
		return ""
	}
	return base.String()
}

func ValidStreamPreferences(p model.StreamPreferences) bool {
	if len(p.Sources) > 100 || len(p.Services) > 3 || len(p.Types) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, source := range p.Sources {
		if !providers.ValidID(source) || seen[source] {
			return false
		}
		seen[source] = true
	}
	for _, pair := range []struct{ values, allowed []string }{{p.Services, []string{"alldebrid", "torbox", "direct"}}, {p.Types, []string{"movie", "series", "anime"}}} {
		clear(seen)
		for _, value := range pair.values {
			if !slices.Contains(pair.allowed, value) || seen[value] {
				return false
			}
			seen[value] = true
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > 4096 {
		return false
	}
	return (p.Mode == "" || p.Mode == "all" || p.Mode == "preferred" || p.Mode == "multi" || p.Mode == "unknown") &&
		(p.Language == "" || NormalizeLanguage(p.Language) == p.Language) && (p.Mode == "" || p.Mode == "all" || p.Language != "")
}

// Preferences travel with the installed URL: an intermediary's browser or
// server locale cannot change the user's selected playback languages.
func InstallationPreferences(token string) (string, model.StreamPreferences, error) {
	if len(token) > MaxPublicInstallationSize {
		return "", model.StreamPreferences{}, model.ErrInvalid
	}
	parts := strings.Split(token, "~")
	if len(parts) == 1 {
		return token, model.StreamPreferences{}, nil
	}
	if len(parts) != 3 {
		return "", model.StreamPreferences{}, model.ErrInvalid
	}
	p := model.StreamPreferences{Language: parts[1], Mode: parts[2]}
	if parts[1] == "v1" {
		data, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
		if err != nil || len(data) > 4096 {
			return "", p, model.ErrInvalid
		}
		p = model.StreamPreferences{}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
			return "", p, model.ErrInvalid
		}
	}
	if parts[0] == "" || !ValidStreamPreferences(p) || p.Mode == "" {
		return "", p, model.ErrInvalid
	}
	return parts[0], p, nil
}

func WithInstallationPreferences(token string, p model.StreamPreferences) (string, error) {
	if !ValidStreamPreferences(p) {
		return "", model.ErrInvalid
	}
	advanced := len(p.Sources) > 0 || len(p.Services) > 0 || len(p.Types) > 0 || p.CachedOnly
	if !advanced {
		if p.Mode == "" {
			return token, nil
		}
		return token + "~" + p.Language + "~" + p.Mode, nil
	}
	if p.Mode == "" {
		p.Mode = "all"
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 4096 {
		return "", model.ErrInvalid
	}
	value := token + "~v1~" + base64.RawURLEncoding.EncodeToString(data)
	if len(value) > MaxPublicInstallationSize {
		return "", model.ErrInvalid
	}
	return value, nil
}

// URL options narrow a connection; they never create or extend a credential.
func AllowsInstallation(p model.StreamPreferences, provider, service, mediaType string) bool {
	return (provider == "" || len(p.Sources) == 0 || slices.Contains(p.Sources, provider)) && (service == "" || len(p.Services) == 0 || slices.Contains(p.Services, service)) && (mediaType == "" || len(p.Types) == 0 || slices.Contains(p.Types, mediaType))
}

func InstallationManifest(p model.StreamPreferences) map[string]any {
	manifest := Manifest()
	if len(p.Types) > 0 {
		manifest["types"] = p.Types
		manifest["resources"] = []any{map[string]any{"name": "stream", "types": p.Types, "idPrefixes": []string{"tt", "imdb:", "tmdb:", "tvdb:", "mal:"}}}
	}
	if len(p.Sources) > 0 || len(p.Services) > 0 || len(p.Types) > 0 || p.CachedOnly {
		data, _ := json.Marshal(p)
		hash := sha256.Sum256(data)
		manifest["id"] = "org.aiosource.v2." + hex.EncodeToString(hash[:8])
	}
	return manifest
}

func AllowsLanguage(rec model.AIORecord, p model.StreamPreferences) bool {
	if p.Mode == "" || p.Mode == "all" {
		return true
	}
	languages := streamLanguages(rec)
	if len(languages) == 0 {
		return p.Mode == "unknown"
	}
	return slices.ContainsFunc(languages, func(l streamLanguage) bool {
		return l.code == p.Language || (l.code == "multi" && (p.Mode == "multi" || p.Mode == "unknown"))
	})
}
