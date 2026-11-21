package model

import (
	"slices"
	"testing"
)

func TestSoftwareCoverURLStrictValidation(t *testing.T) {
	if got := CoverURL("software", "visual-studio-code", 0); got != "https://cdn.jsdelivr.net/gh/selfhst/icons@main/png/visual-studio-code.png" {
		t.Fatalf("unexpected valid software icon URL: %q", got)
	}
	if got := CoverURL("software", "7-zip", 0); got != "https://cdn.jsdelivr.net/gh/selfhst/icons@main/png/7-zip.png" {
		t.Fatalf("unexpected numeric-prefixed software icon URL: %q", got)
	}
	for _, bad := range []string{
		"",
		"https://evil.example/icon.png",
		"../etc/passwd",
		"vlc.png",
		"VLC",
		"-leading-dash",
		"bad/slash",
		"bad_underscore",
		"a" + string(make([]byte, 130)),
	} {
		if got := CoverURL("software", bad, 0); got != "" {
			t.Fatalf("expected empty cover URL for untrusted icon ref %q, got %q", bad, got)
		}
	}
}

func TestSoftwareSecretReferencesAndProviderCalls(t *testing.T) {
	cfg := AIOConfig{
		TMDBSecret:        "tmdb_ref",
		IGDBSecret:        "igdb_ref",
		SoftwareNVDSecret: "nvd_ref",
		MusicBrainzSecret: "mb_ref",
	}
	if !slices.Contains(cfg.SecretReferences(), "nvd_ref") {
		t.Fatalf("SoftwareNVDSecret missing from SecretReferences: %v", cfg.SecretReferences())
	}
	for _, phase := range []string{
		"software_nvd_meta",
		"software_nvd_dump",
		"software_nvd_incremental",
		"software_winget",
		"software_homebrew",
		"software_icons",
	} {
		if !(ProviderCall{Phase: phase, HTTPStatus: 200, DurationMS: 12}).Valid() {
			t.Fatalf("expected provider call phase %q to be valid", phase)
		}
	}
}

func TestSoftwareSourceHintsCategoriesAndExclusions(t *testing.T) {
	mac := SoftwareSourceHints(map[string]any{
		"categories": []any{4000, 4030},
		"winget_id":  "VideoLAN.VLC",
		"vendor":     "VideoLAN",
		"os":         "macOS, Windows",
	})
	if mac.Kind != "mac" || mac.WinGetID != "VideoLAN.VLC" || mac.Vendor != "VideoLAN" || len(mac.Platforms) != 2 {
		t.Fatalf("unexpected mac software hints: %+v", mac)
	}

	android := SoftwareSourceHints(map[string]any{
		"categories": []any{4070},
	})
	if android.Kind != "android" {
		t.Fatalf("expected android kind for 4070, got %q", android.Kind)
	}

	// Category 4050 (PC-Games) MUST NEVER be treated as software.
	pcGame := SoftwareSourceHints(map[string]any{
		"categories": []any{4000, 4050},
	})
	if pcGame.Kind != "" {
		t.Fatalf("expected 4050 PC-Games to be excluded from software hints, got %q", pcGame.Kind)
	}

	// Books / Music / Video categories must also be excluded.
	for _, cat := range []int{1000, 2000, 3000, 3030, 5000, 7000, 7020} {
		h := SoftwareSourceHints(map[string]any{"categories": []any{cat}})
		if h.Kind != "" {
			t.Fatalf("expected category %d to be excluded from software hints, got %q", cat, h.Kind)
		}
	}
}
