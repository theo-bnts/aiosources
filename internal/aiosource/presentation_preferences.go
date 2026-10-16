package aiosource

import (
	"slices"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
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
	return (p.Mode == "" || p.Mode == "all" || p.Mode == "preferred" || p.Mode == "multi" || p.Mode == "unknown") &&
		(p.Language == "" || NormalizeLanguage(p.Language) == p.Language) && (p.Mode == "" || p.Mode == "all" || p.Language != "")
}

// Preferences travel with the installed URL: an intermediary's browser or
// server locale cannot change the user's selected playback languages.
func InstallationPreferences(token string) (string, model.StreamPreferences, error) {
	parts := strings.Split(token, "~")
	if len(parts) == 1 {
		return token, model.StreamPreferences{}, nil
	}
	if len(parts) != 3 {
		return "", model.StreamPreferences{}, model.ErrInvalid
	}
	p := model.StreamPreferences{Language: parts[1], Mode: parts[2]}
	if parts[0] == "" || !ValidStreamPreferences(p) || p.Mode == "" {
		return "", p, model.ErrInvalid
	}
	return parts[0], p, nil
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
