package providers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
)

const migrationRecoveryDirectory = ".source-migration"
const migrationManifestName = ".manifest.json"

// MigrationResult never contains source documents or credential values.
type MigrationResult struct {
	Migrated          int    `json:"migrated"`
	RecoveryDirectory string `json:"recovery_directory,omitempty"`
}

type migrationEntry struct {
	Source           string `json:"source"`
	Target           string `json:"target"`
	OriginalRevision string `json:"original_revision"`
	JSONRevision     string `json:"json_revision"`
}

type migrationManifest struct {
	Version int              `json:"version"`
	Entries []migrationEntry `json:"entries"`
}

type migrationSource struct {
	entry     migrationEntry
	original  []byte
	json      []byte
	present   bool
	installed bool
}

// MigrateSources is an explicit offline operation. Stop the application and all
// external source writers first. It never opens a database, calls the network or
// resolves secrets. Cooperating registries are locked throughout the operation.
// Exact originals and a recovery journal are durable before any JSON is created;
// all JSON is durable before any YAML is removed. An interrupted invocation may
// resume only when both originals and installed output match its journal.
func MigrateSources(ctx context.Context, directory string, validate func(model.Provider) error) (MigrationResult, error) {
	result := MigrationResult{}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, fmt.Errorf("migration requires an existing real provider directory")
	}
	registry, err := New(directory, validate)
	if err != nil {
		return result, err
	}
	defer registry.Close()
	if err := registry.acquireMigrationLock(ctx); err != nil {
		return result, err
	}
	defer registry.release()
	names, err := migrationNames(registry.root)
	if err != nil {
		return result, err
	}
	var legacy []string
	for _, name := range names {
		if legacyDefinitionFilename(name) {
			legacy = append(legacy, name)
		}
	}
	if len(legacy) == 0 {
		return result, nil
	}
	if len(legacy) > 10000 {
		return result, fmt.Errorf("migration exceeds the 10000-source safety limit")
	}

	var archive *Registry
	var manifest migrationManifest
	if _, err := registry.root.Lstat(migrationRecoveryDirectory); err == nil {
		archive, err = openMigrationArchive(registry.root)
		if err != nil {
			return result, err
		}
		defer archive.root.Close()
		manifest, err = readMigrationManifest(archive)
		if err != nil {
			return result, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("cannot inspect migration recovery directory")
	} else {
		manifest.Version = 1
		for _, name := range legacy {
			manifest.Entries = append(manifest.Entries, migrationEntry{Source: name, Target: strings.TrimSuffix(name, filepath.Ext(name)) + ".json"})
		}
	}

	// Complete preflight, including old definitions, saved recovery state,
	// semantic IDs, case-insensitive target names, and every existing JSON file.
	sources, err := registry.preflightMigration(ctx, names, legacy, manifest, archive)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if archive == nil {
		if err := registry.root.Mkdir(migrationRecoveryDirectory, 0700); err != nil {
			return result, fmt.Errorf("cannot create private migration recovery directory")
		}
		archive, err = openMigrationArchive(registry.root)
		if err != nil {
			return result, err
		}
		defer archive.root.Close()
		manifest.Entries = make([]migrationEntry, len(sources))
		for index := range sources {
			manifest.Entries[index] = sources[index].entry
		}
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return result, fmt.Errorf("cannot prepare migration recovery journal")
		}
		if err := archive.atomicWrite(migrationManifestName, append(data, '\n'), ""); err != nil {
			return result, fmt.Errorf("cannot persist migration recovery journal; original definitions remain unchanged")
		}
		if err := registry.syncDirectory(); err != nil {
			return result, err
		}
	}
	result.RecoveryDirectory = filepath.Join(directory, migrationRecoveryDirectory)
	incomplete := func() (MigrationResult, error) {
		return result, fmt.Errorf("migration incomplete; exact originals are retained in %s or their original locations; stop all writers, resolve changed files or collisions, then rerun migrate-sources", migrationRecoveryDirectory)
	}
	for _, source := range sources {
		if ctx.Err() != nil {
			return incomplete()
		}
		if existing, err := archive.readDefinition(source.entry.Source); err == nil {
			if !bytes.Equal(existing, source.original) {
				return incomplete()
			}
		} else {
			if _, err := archive.root.Lstat(source.entry.Source); !errors.Is(err, os.ErrNotExist) {
				return incomplete()
			}
			if err := archive.atomicWrite(source.entry.Source, source.original, ""); err != nil {
				return incomplete()
			}
		}
	}
	if err := archive.syncDirectory(); err != nil {
		return incomplete()
	}
	// No-clobber installs: a file that appeared after preflight is never replaced.
	for _, source := range sources {
		if ctx.Err() != nil {
			return incomplete()
		}
		if !source.installed {
			if err := registry.atomicWrite(source.entry.Target, source.json, ""); err != nil {
				return incomplete()
			}
		}
	}
	for _, source := range sources {
		if ctx.Err() != nil {
			return incomplete()
		}
		output, outputErr := registry.readDefinition(source.entry.Target)
		original, originalErr := archive.readDefinition(source.entry.Source)
		if outputErr != nil || originalErr != nil || revision(output) != source.entry.JSONRevision || revision(original) != source.entry.OriginalRevision {
			return incomplete()
		}
		if source.present {
			current, err := registry.readDefinition(source.entry.Source)
			if err != nil || revision(current) != source.entry.OriginalRevision {
				return incomplete()
			}
			if err := registry.root.Remove(source.entry.Source); err != nil {
				return incomplete()
			}
			if err := registry.syncDirectory(); err != nil {
				return incomplete()
			}
		}
		result.Migrated++
	}
	return result, nil
}

func (r *Registry) preflightMigration(ctx context.Context, names, legacy []string, manifest migrationManifest, archive *Registry) ([]migrationSource, error) {
	fail := func() ([]migrationSource, error) {
		return nil, fmt.Errorf("migration preflight failed: invalid or changed definitions, duplicate source IDs, unsafe files, or target/recovery collisions; no source files were changed")
	}
	claimedSources := make(map[string]bool, len(manifest.Entries))
	claimedTargets := make(map[string]bool, len(manifest.Entries))
	claimedIDs := make(map[string]bool)
	actualNames := make(map[string]string, len(names))
	for _, name := range names {
		lower := strings.ToLower(name)
		if previous, exists := actualNames[lower]; exists && previous != name && (definitionFilename(name) || legacyDefinitionFilename(name)) {
			return fail()
		}
		actualNames[lower] = name
	}
	sources := make([]migrationSource, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sourceName, targetName := strings.ToLower(entry.Source), strings.ToLower(entry.Target)
		if claimedSources[sourceName] || claimedTargets[targetName] || !utf8.ValidString(entry.Source) || !legacyDefinitionFilename(entry.Source) || filepath.Base(entry.Source) != entry.Source || strings.ContainsAny(entry.Source, "\\\x00") || !definitionFilename(entry.Target) || entry.Target != strings.TrimSuffix(entry.Source, filepath.Ext(entry.Source))+".json" {
			return fail()
		}
		claimedSources[sourceName], claimedTargets[targetName] = true, true
		source := migrationSource{entry: entry}
		if _, err := r.root.Lstat(entry.Source); err == nil {
			source.original, err = r.readDefinition(entry.Source)
			if err != nil {
				return fail()
			}
			source.present = true
		} else if !errors.Is(err, os.ErrNotExist) || archive == nil {
			return fail()
		} else {
			source.original, err = archive.readDefinition(entry.Source)
			if err != nil {
				return fail()
			}
		}
		if archive != nil && revision(source.original) != entry.OriginalRevision {
			return fail()
		}
		converted, err := convertLegacySource(source.original)
		if err != nil {
			return fail()
		}
		validation := r.Validate(string(converted))
		if !validation.Valid || claimedIDs[validation.ID] {
			return fail()
		}
		claimedIDs[validation.ID] = true
		source.json = converted
		source.entry.OriginalRevision, source.entry.JSONRevision = revision(source.original), revision(converted)
		if archive != nil && source.entry.JSONRevision != entry.JSONRevision {
			return fail()
		}
		if actual, exists := actualNames[targetName]; exists {
			if archive == nil || actual != entry.Target {
				return fail()
			}
			data, err := r.readDefinition(entry.Target)
			if err != nil || revision(data) != entry.JSONRevision {
				return fail()
			}
			source.installed = true
		}
		if !source.present && !source.installed {
			return fail()
		}
		if archive != nil {
			if _, err := archive.root.Lstat(entry.Source); err == nil {
				data, err := archive.readDefinition(entry.Source)
				if err != nil || revision(data) != entry.OriginalRevision {
					return fail()
				}
			} else if !errors.Is(err, os.ErrNotExist) || !source.present {
				return fail()
			}
		}
		sources = append(sources, source)
	}
	for _, name := range legacy {
		if !claimedSources[strings.ToLower(name)] {
			return fail()
		}
	}
	for _, name := range names {
		if !definitionFilename(name) || claimedTargets[strings.ToLower(name)] {
			continue
		}
		data, err := r.readDefinition(name)
		if err != nil {
			return fail()
		}
		validation := r.Validate(string(data))
		if !validation.Valid || claimedIDs[validation.ID] {
			return fail()
		}
		claimedIDs[validation.ID] = true
	}
	return sources, nil
}

func legacyDefinitionFilename(name string) bool {
	lower := strings.ToLower(name)
	return (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")) && !strings.HasSuffix(lower, ".example.yaml") && !strings.HasSuffix(lower, ".example.yml")
}

func migrationNames(root *os.Root) ([]string, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("cannot open source migration directory")
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("cannot list source migration directory")
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	sort.Strings(names)
	return names, nil
}

func openMigrationArchive(parent *os.Root) (*Registry, error) {
	info, err := parent.Lstat(migrationRecoveryDirectory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("migration recovery must be a private real directory (0700)")
	}
	root, err := parent.OpenRoot(migrationRecoveryDirectory)
	if err != nil {
		return nil, fmt.Errorf("cannot open migration recovery directory")
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("migration recovery directory changed while opening")
	}
	return &Registry{root: root}, nil
}

func readMigrationManifest(archive *Registry) (migrationManifest, error) {
	var manifest migrationManifest
	invalid := func() (migrationManifest, error) {
		return manifest, fmt.Errorf("migration recovery journal is missing or invalid; preserve the recovery directory and inspect it offline before retrying")
	}
	info, err := archive.root.Lstat(migrationManifestName)
	if err != nil || info.Size() > 8<<20 {
		return invalid()
	}
	data, err := archive.readDefinition(migrationManifestName)
	if err != nil {
		return invalid()
	}
	if _, err := parseDocument(string(data)); err != nil {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.Version != 1 || len(manifest.Entries) == 0 || len(manifest.Entries) > 10000 {
		return invalid()
	}
	for _, entry := range manifest.Entries {
		for _, digest := range []string{entry.OriginalRevision, entry.JSONRevision} {
			if decoded, err := hex.DecodeString(digest); err != nil || len(decoded) != 32 {
				return invalid()
			}
		}
	}
	return manifest, nil
}
