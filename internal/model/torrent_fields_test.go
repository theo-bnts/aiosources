package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWithoutSwarmCountsPreservesExactDataAndSourceObjects(t *testing.T) {
	nested := map[string]any{"seeders": 7, "peers": 9, "size": json.Number("9007199254740993123456789"), "title": "Seeders"}
	attributes := map[string][]string{"seeders": {"9"}, "peers": {"4"}, "imdbid": {"tt123"}}
	fields := map[string]any{"seeders": func() {}, "peers": func() {}, "metadata": []any{nested, nil, "seeders"}, "attributes": attributes}
	got := WithoutSwarmCounts(fields)
	want := map[string]any{"metadata": []any{map[string]any{"size": json.Number("9007199254740993123456789"), "title": "Seeders"}, nil, "seeders"}, "attributes": map[string][]string{"imdbid": {"tt123"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed durable data: %#v", got)
	}
	if _, exists := fields["seeders"]; !exists || nested["seeders"] != 7 || nested["peers"] != 9 || attributes["seeders"][0] != "9" {
		t.Fatal("mutated the source objects")
	}
	if WithoutSwarmCounts(nil) != nil {
		t.Fatal("nil fields changed")
	}
}

func TestMetadataFieldsKeepLegacySnapshotsAndSkipRetiredCounters(t *testing.T) {
	provider := Provider{Traversal: &JSONTraversal{EnrichFields: []string{"external_ids", "seeders", "peers", "metadata"}}}
	if !reflect.DeepEqual(provider.MetadataFields(), []string{"external_ids", "metadata"}) ||
		!reflect.DeepEqual(provider.Traversal.EnrichFields, []string{"external_ids", "seeders", "peers", "metadata"}) {
		t.Fatal("retired counters changed the source snapshot or effective enrichment")
	}
	provider.Traversal.EnrichFields = []string{"seeders", "peers"}
	if len(provider.MetadataFields()) != 0 || (Provider{}).MetadataFields() != nil {
		t.Fatal("counter-only or unconfigured enrichment retained candidates")
	}
}
