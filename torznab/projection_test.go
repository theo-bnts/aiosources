package torznab

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestProjectionRestrictsJSONWithoutLosingZeroOrMultipleValues(t *testing.T) {
	fields := []string{"title", "seeders", "peers", "attributes.imdbid", "attributes.tag", "attributes.absent", "title"}
	projection, err := NewProjection(fields)
	if err != nil {
		t.Fatal(err)
	}
	// Reusing the caller's configuration slice must not widen an already
	// compiled selection to expose a private download link.
	fields[0] = "link"
	zero := int64(0)
	item := Item{
		GUID: "unselected-identity", Title: "Example release", Seeders: &zero,
		Link: "https://example.invalid/download?token=private-secret",
		Attributes: map[string][]string{
			"imdbid": {"tt001"}, "tag": {"one", "two"},
			"apikey": {"private-secret"}, "vendor-private": {"not-selected"},
		},
	}
	encoded, err := json.Marshal(projection.Project(&item))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-secret") || strings.Contains(string(encoded), "unselected-identity") {
		t.Fatal("projection exposed unselected data")
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"title": "Example release", "seeders": float64(0),
		"attributes": map[string]any{"imdbid": []any{"tt001"}, "tag": []any{"one", "two"}},
	}
	if !reflect.DeepEqual(output, want) {
		t.Fatalf("selection lost missing/zero/list distinctions: %#v", output)
	}
}

func TestProjectionPartialUpdatesDoNotEraseKnownValues(t *testing.T) {
	projection, err := NewProjection([]string{"title", "seeders", "published_at", "attributes.imdbid"})
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]any{"title": "Earlier title", "seeders": int64(9), "attributes": map[string][]string{"imdbid": {"tt001"}}}
	partial := Item{Title: "Updated title"}
	for name, value := range projection.Project(&partial) {
		stored[name] = value
	}
	if stored["title"] != "Updated title" || stored["seeders"] != int64(9) {
		t.Fatalf("partial data overwrote a known count: %#v", stored)
	}
	if _, present := stored["published_at"]; present {
		t.Fatal("missing date was fabricated")
	}
	if !reflect.DeepEqual(stored["attributes"], map[string][]string{"imdbid": {"tt001"}}) {
		t.Fatal("missing external ID erased known metadata")
	}
	zero := int64(0)
	partial.Seeders = &zero
	for name, value := range projection.Project(&partial) {
		stored[name] = value
	}
	if stored["seeders"] != int64(0) {
		t.Fatal("explicit zero could not update the previous count")
	}
}

func TestProjectionInvalidSelectionNeverFallsBackToAllFields(t *testing.T) {
	for _, fields := range [][]string{nil, {}, {"private-secret"}, {"attributes."}, {"attributes. imdbid"}} {
		projection, err := NewProjection(fields)
		if err == nil || projection != nil {
			t.Fatal("invalid selection could expose complete records")
		}
		if strings.Contains(err.Error(), "private-secret") {
			t.Fatal("selection error echoed raw configuration")
		}
	}
}

func TestProjectionWholeAttributeSelectionKeepsLaterVendorMetadata(t *testing.T) {
	projection, err := NewProjection([]string{"attributes.imdbid", "attributes", "attributes.tag"})
	if err != nil {
		t.Fatal(err)
	}
	item := Item{GUID: "not-selected"}
	if output := projection.Project(&item); len(output) != 0 {
		t.Fatalf("missing attributes fabricated output: %#v", output)
	}
	item.Attributes = map[string][]string{"vendor-field": {"available", "verified"}}
	output := projection.Project(&item)
	if !reflect.DeepEqual(output, map[string]any{"attributes": item.Attributes}) {
		t.Fatalf("explicit complete-attribute selection discarded later metadata: %#v", output)
	}
}
