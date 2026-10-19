package providers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONAdapterUpgradePreservesFilesPrecisionAndUnrelatedValues(t *testing.T) {
	dir := t.TempDir()
	raw := withFields(definition("precision"), `"http":{"query":{"number":9007199254740993,"ratio":1.2300e+2,"label":"http_json"}}`)
	file := filepath.Join(dir, "Custom Filename.JSON")
	writeDefinition(t, file, raw)
	invalid := strings.Replace(definition("invalid"), `"enabled": false`, `"enabled": "false"`, 1)
	writeDefinition(t, filepath.Join(dir, "invalid.json"), invalid)
	writeDefinition(t, filepath.Join(dir, "ignored.example.json"), definition("ignored"))
	r := openRegistry(t, dir)
	before, err := r.Get("precision")
	if err != nil {
		t.Fatal(err)
	}
	count, err := r.UpgradeDefinitions()
	if err != nil || count != 1 {
		t.Fatal("valid legacy definition was not upgraded", count, err)
	}
	want := strings.Replace(raw, `"adapter": "http_json"`, `"adapter": "json"`, 1)
	assertFile(t, file, want)
	assertFile(t, filepath.Join(dir, "invalid.json"), invalid)
	assertFile(t, filepath.Join(dir, "ignored.example.json"), definition("ignored"))
	after, err := r.Get("precision")
	if err != nil || len(after.Issues) > 0 || after.Provider.ID != before.Provider.ID || after.Provider.Adapter != "json" || after.Revision == before.Revision {
		t.Fatal("source identity or validated state changed during upgrade", err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	count, err = r.UpgradeDefinitions()
	current, statErr := os.Stat(file)
	if err != nil || statErr != nil || count != 0 || !current.ModTime().Equal(info.ModTime()) {
		t.Fatal("repeated startup rewrote an upgraded definition", err, statErr)
	}
	if _, err = r.Save("precision", raw, before.Revision); err == nil {
		t.Fatal("an old draft silently overwrote the upgraded definition")
	}
}

func TestJSONAdapterUpgradeRecognizesEscapedTokenWithoutTouchingNestedKeys(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Replace(definition("escaped"), `"http_json"`, `"\u0068ttp_json"`, 1)
	raw = withFields(raw, `"http":{"query":{"adapter":"http_json","value":184467440737095516170}}`)
	file := filepath.Join(dir, "escaped.json")
	writeDefinition(t, file, raw)
	r := openRegistry(t, dir)
	if count, err := r.UpgradeDefinitions(); err != nil || count != 1 {
		t.Fatal("escaped legacy adapter was not upgraded", count, err)
	}
	assertFile(t, file, strings.Replace(raw, `"\u0068ttp_json"`, `"json"`, 1))
}
