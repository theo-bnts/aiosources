package providers_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/providers"
)

func legacyDefinition(id string) string {
	return "# Keep this exact operator comment.\nversion: 1\nid: " + id + "\nname: Fictional archive\nadapter: http_json\nurl: https://archive.invalid/api\nenabled: false\n"
}

func TestMigrationRetainsOriginalsAndExactNumbers(t *testing.T) {
	dir := t.TempDir()
	original := legacyDefinition("precision") + "request_interval: 2s\nschedule:\n  enabled: false\n  every: 1h\n  known_pages: 4\nhttp:\n  query:\n    id: 9007199254740993\n    large: 184467440737095516170\n  incremental_query:\n    fraction: 0.123456789012345678901234567890\n  body:\n    ratio: 1.2300e+2\n    hex: 0x2a\noptions:\n  local_categories: [2000.0, 2.010e3]\n"
	writeDefinition(t, filepath.Join(dir, "operator source.YAML"), original)
	result, err := providers.MigrateSources(t.Context(), dir, nil)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("migration failed: %+v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "operator source.YAML")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active YAML was not removed: %v", err)
	}
	assertFile(t, filepath.Join(result.RecoveryDirectory, "operator source.YAML"), original)
	assertPrivate(t, filepath.Join(result.RecoveryDirectory, "operator source.YAML"))
	assertPrivate(t, filepath.Join(result.RecoveryDirectory, ".manifest.json"))
	assertPrivate(t, filepath.Join(dir, "operator source.json"))
	if info, err := os.Stat(result.RecoveryDirectory); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("recovery directory is not private: %v", err)
	}
	r := openRegistry(t, dir)
	document, err := r.Get("precision")
	if err != nil || len(document.Issues) != 0 {
		t.Fatalf("migrated source is not usable: %v, %v", err, document.Issues)
	}
	p := document.Provider
	if p.Enabled || p.RequestInterval != "2s" || p.Schedule.Enabled == nil || *p.Schedule.Enabled || p.Schedule.KnownPages != 4 || p.Schedule.Mode != model.ModeIncremental {
		t.Fatalf("migration changed source or schedule settings: %+v", p.Schedule)
	}
	body := p.HTTP.Body.(map[string]any)
	categories := p.Options["local_categories"].([]any)
	for name, test := range map[string]struct {
		got  any
		want string
	}{
		"integer":           {p.HTTP.Query["id"], "9007199254740993"},
		"beyond uint64":     {p.HTTP.Query["large"], "184467440737095516170"},
		"decimal":           {p.HTTP.IncrementalQuery["fraction"], "0.123456789012345678901234567890"},
		"exponent":          {body["ratio"], "1.2300e+2"},
		"hex":               {body["hex"], "42"},
		"integral category": {categories[0], "2000"},
		"exponent category": {categories[1], "2010"},
	} {
		if number, ok := test.got.(json.Number); !ok || string(number) != test.want {
			t.Fatalf("%s lost precision or numeric semantics: %#v", name, test.got)
		}
	}
	before := document.JSON
	rerun, err := providers.MigrateSources(t.Context(), dir, nil)
	if err != nil || rerun.Migrated != 0 {
		t.Fatalf("completed migration is not idempotent: %+v, %v", rerun, err)
	}
	assertFile(t, filepath.Join(dir, "operator source.json"), before)
	assertFile(t, filepath.Join(result.RecoveryDirectory, "operator source.YAML"), original)
}

func TestMigrationPreflightsEveryDefinitionBeforeWrites(t *testing.T) {
	for name, invalid := range map[string]string{
		"duplicate property": legacyDefinition("bad") + "options: {limit: 1, limit: 2}\n",
		"null scalar":        legacyDefinition("bad") + "schedule: {enabled: null}\n",
		"literal credential": legacyDefinition("bad") + "http: {query: {password: unique-sensitive-value}}\n",
		"nonfinite number":   legacyDefinition("bad") + "options: {limit: .inf}\n",
		"coerced timestamp":  strings.Replace(legacyDefinition("bad"), "name: Fictional archive", "name: 2020-01-01", 1),
		"alias":              legacyDefinition("bad") + "options: {one: &a [1], two: *a}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			valid := legacyDefinition("valid")
			writeDefinition(t, filepath.Join(dir, "a-valid.yml"), valid)
			writeDefinition(t, filepath.Join(dir, "z-invalid.yml"), invalid)
			if _, err := providers.MigrateSources(t.Context(), dir, nil); err == nil || strings.Contains(err.Error(), "unique-sensitive-value") {
				t.Fatalf("invalid legacy source accepted or secret exposed: %v", err)
			}
			assertFile(t, filepath.Join(dir, "a-valid.yml"), valid)
			assertFile(t, filepath.Join(dir, "z-invalid.yml"), invalid)
			for _, name := range []string{"a-valid.json", "z-invalid.json", ".source-migration"} {
				if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("preflight failure created migration output %q: %v", name, err)
				}
			}
		})
	}
}

func TestMigrationRejectsNameAndIDCollisionsWithoutOverwrite(t *testing.T) {
	for _, conflict := range []string{"target", "case-folded target", "duplicate target", "duplicate ID"} {
		t.Run(conflict, func(t *testing.T) {
			dir := t.TempDir()
			original := legacyDefinition("archive")
			writeDefinition(t, filepath.Join(dir, "source.yaml"), original)
			otherName, other := "source.json", definition("unrelated")
			switch conflict {
			case "case-folded target":
				otherName = "SOURCE.JSON"
			case "duplicate target":
				otherName, other = "source.yml", legacyDefinition("other")
			case "duplicate ID":
				otherName, other = "unrelated.json", definition("archive")
			}
			writeDefinition(t, filepath.Join(dir, otherName), other)
			if _, err := providers.MigrateSources(t.Context(), dir, nil); err == nil {
				t.Fatal("migration accepted a collision")
			}
			assertFile(t, filepath.Join(dir, "source.yaml"), original)
			assertFile(t, filepath.Join(dir, otherName), other)
		})
	}
}

func TestMigrationResumesJournaledPartialInstall(t *testing.T) {
	dir := t.TempDir()
	originals := map[string]string{"one.yaml": legacyDefinition("one"), "two.yml": legacyDefinition("two")}
	for name, original := range originals {
		writeDefinition(t, filepath.Join(dir, name), original)
	}
	completed, err := providers.MigrateSources(t.Context(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct an interruption after the first durable JSON installation:
	// the recovery journal and originals exist, all legacy sources still exist.
	for name, original := range originals {
		writeDefinition(t, filepath.Join(dir, name), original)
	}
	if err := os.Remove(filepath.Join(dir, "two.json")); err != nil {
		t.Fatal(err)
	}
	result, err := providers.MigrateSources(t.Context(), dir, nil)
	if err != nil || result.Migrated != 2 {
		t.Fatalf("journaled partial installation did not resume: %+v, %v", result, err)
	}
	r := openRegistry(t, dir)
	for name, original := range originals {
		assertFile(t, filepath.Join(completed.RecoveryDirectory, name), original)
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("resumed migration retained active YAML: %v", err)
		}
		if document, err := r.Get(strings.TrimSuffix(name, filepath.Ext(name))); err != nil || len(document.Issues) != 0 {
			t.Fatalf("resumed source is unavailable: %v, %v", err, document.Issues)
		}
	}
}

func TestMigrationDoesNotOverwriteChangedJournaledOutput(t *testing.T) {
	dir := t.TempDir()
	original := legacyDefinition("archive")
	writeDefinition(t, filepath.Join(dir, "source.yaml"), original)
	result, err := providers.MigrateSources(t.Context(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeDefinition(t, filepath.Join(dir, "source.yaml"), original)
	changed := definition("new-owner")
	writeDefinition(t, filepath.Join(dir, "source.json"), changed)
	if _, err := providers.MigrateSources(t.Context(), dir, nil); err == nil {
		t.Fatal("resume accepted changed output")
	}
	assertFile(t, filepath.Join(dir, "source.json"), changed)
	assertFile(t, filepath.Join(dir, "source.yaml"), original)
	assertFile(t, filepath.Join(result.RecoveryDirectory, "source.yaml"), original)
}

func TestMigrationRejectsLinkedOriginals(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "external.yaml")
			original := legacyDefinition("outside")
			writeDefinition(t, outside, original)
			link := os.Symlink
			if kind == "hardlink" {
				link = os.Link
			}
			if err := link(outside, filepath.Join(dir, "source.yaml")); err != nil {
				t.Fatal(err)
			}
			if _, err := providers.MigrateSources(t.Context(), dir, nil); err == nil {
				t.Fatal("linked original was migrated")
			}
			assertFile(t, outside, original)
			if _, err := os.Lstat(filepath.Join(dir, "source.yaml")); err != nil {
				t.Fatalf("unsafe original was removed: %v", err)
			}
		})
	}
}
