package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLegacyMigrationChecksumsRemainVerifiable(t *testing.T) {
	data, err := os.ReadFile("testdata/legacy-schema-checksums.json")
	if err != nil {
		t.Fatal(err)
	}
	var original map[int]string
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	if len(original) != legacySchemaVersion {
		t.Fatal("legacy migration coverage is incomplete")
	}
	for _, migration := range migrations {
		if migration.version > legacySchemaVersion {
			continue
		}
		if legacyMigrationChecksum(migration.sql) != original[migration.version] {
			t.Errorf("migration %d no longer reconstructs its released legacy checksum", migration.version)
		}
	}
}
