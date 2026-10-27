package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompactStoragePreservesEvidenceAndNeverMutatesSourceData(t *testing.T) {
	fields := map[string]any{
		"title": "Example.S02.2160p.MULTi", "size": json.Number("9007199254740993"),
		"external_ids": map[string]any{"imdb": []any{"tt1", "tt2"}, "poster": "unused"},
		"attributes":   map[string]any{"season": []any{"2"}, "audiolangs": []any{"fr", "en"}, "seeders": "50", "description": "unused"},
		"description":  "large unused data", "peers": 7,
	}
	before, _ := json.Marshal(fields)
	compact := StoredTorrentFields(fields, StorageCompact)
	if compact["description"] != nil || compact["peers"] != nil || compact["size"] != fields["size"] {
		t.Fatalf("compact storage lost exact values or retained unused fields: %#v", compact)
	}
	if !reflect.DeepEqual(compact["external_ids"], map[string]any{"imdb": []any{"tt1", "tt2"}}) ||
		!reflect.DeepEqual(compact["attributes"], map[string]any{"season": []any{"2"}, "audiolangs": []any{"fr", "en"}}) {
		t.Fatalf("identifier/language evidence was changed: %#v", compact)
	}
	after, _ := json.Marshal(fields)
	if string(before) != string(after) || StoredTorrentFields(fields, StorageFull)["description"] != fields["description"] {
		t.Fatal("source data was modified, or full mode discarded a selected field")
	}
	if StoredTorrentFields(nil, StorageCompact) != nil {
		t.Fatal("nil fields were fabricated")
	}
}

func TestCompactMetadataRequirementsDoNotRequestDiscardedFieldsForever(t *testing.T) {
	provider := Provider{Traversal: &JSONTraversal{EnrichFields: []string{"description", "external_ids", "attributes", "peers"}}}
	run := Run{Config: provider, StorageMode: StorageCompact}
	want := []string{"external_ids", "attributes"}
	if !reflect.DeepEqual(run.MetadataFields(), want) || !reflect.DeepEqual(run.ConnectorConfig().MetadataFields(), want) ||
		!reflect.DeepEqual(provider.Traversal.EnrichFields, []string{"description", "external_ids", "attributes", "peers"}) {
		t.Fatal("discarded fields remain required, or the immutable definition was changed")
	}
}

func TestCompactStorageProjectsTypedTorznabAttributesAndIdentifiers(t *testing.T) {
	attributes := map[string][]string{"imdbid": {"tt1", "tt2"}, "language": {"fr", "en"}, "description": {"unused"}}
	ids := map[string]string{"tmdb": "42", "poster": "unused"}
	compact := StoredTorrentFields(map[string]any{"attributes": attributes, "external_ids": ids}, StorageCompact)
	if !reflect.DeepEqual(compact["attributes"], map[string][]string{"imdbid": {"tt1", "tt2"}, "language": {"fr", "en"}}) ||
		!reflect.DeepEqual(compact["external_ids"], map[string]string{"tmdb": "42"}) || attributes["description"] == nil || ids["poster"] == "" {
		t.Fatal("typed attributes retained unused payloads or changed identifier evidence")
	}
	for _, missing := range []any{map[string]any(nil), map[string][]string(nil), map[string]string(nil)} {
		fields := StoredTorrentFields(map[string]any{"external_ids": missing}, StorageCompact)
		encoded, err := json.Marshal(fields)
		if err != nil || string(encoded) != `{"external_ids":null}` {
			t.Fatalf("unknown metadata became a known empty object: %s %v", encoded, err)
		}
	}
}

func TestCompactStorageAlsoProjectsNestedMetadataWithoutChangingEvidence(t *testing.T) {
	fields := map[string]any{"metadata": map[string]any{
		"description":  "unused outer detail",
		"attributes":   map[string]any{"language": []string{"fr", "en"}, "description": "unused attribute detail"},
		"external_ids": map[string]any{"imdb": []string{"tt1", "tt2"}, "poster": "unused identifier detail"},
		"metadata":     map[string]any{"season": 2, "description": "unused inner detail"},
	}}
	before, _ := json.Marshal(fields)
	compact := StoredTorrentFields(fields, StorageCompact)
	want := map[string]any{"metadata": map[string]any{
		"attributes":   map[string]any{"language": []string{"fr", "en"}},
		"external_ids": map[string]any{"imdb": []string{"tt1", "tt2"}},
		"metadata":     map[string]any{"season": 2},
	}}
	if !reflect.DeepEqual(compact, want) {
		t.Fatal("nested metadata retained unused payloads or changed useful evidence")
	}
	after, _ := json.Marshal(fields)
	if string(before) != string(after) || !reflect.DeepEqual(StoredTorrentFields(fields, StorageFull), fields) {
		t.Fatal("nested projection mutated the source or full storage")
	}
}

func TestCompactBooksRetainUsefulBibliographicEvidence(t *testing.T) {
	fields := map[string]any{"title": "Morgan Vale - The Amber Orchard EPUB", "author": "Morgan Vale", "isbn": "9780306406157", "openlibrary": "OL100W", "categories": []int{7020}, "description": "unneeded", "attributes": map[string][]string{"isbn13": {"9780306406157", "9780306406158"}, "booktitle": {"The Amber Orchard"}, "book_format": {"epub"}, "password": {"private"}}}
	stored := StoredTorrentFields(fields, StorageCompact)
	if stored["description"] != nil || stored["author"] != fields["author"] || stored["openlibrary"] != fields["openlibrary"] {
		t.Fatal("book evidence lost in compact storage")
	}
	hints := BookSourceHints(stored)
	if hints.Author != "Morgan Vale" || hints.Title != "The Amber Orchard" || hints.Format != "epub" || len(hints.ISBNs) != 3 || len(hints.Categories) != 1 || hints.Categories[0] != 7020 {
		t.Fatalf("typed bibliographic hints lost: %+v", hints)
	}
	if stored["attributes"].(map[string][]string)["password"] != nil {
		t.Fatal("unknown attribute retained")
	}
}

func TestCoverURLsUseOnlyCanonicalProviderIdentifiers(t *testing.T) {
	for _, unsafe := range []string{"https://private.invalid/a.jpg", "//private.invalid/a.jpg", "/../secret.jpg", "/a.jpg?key=secret", "/a.jpg#fragment"} {
		if CoverURL("movie", unsafe, 0) != "" {
			t.Fatal("untrusted image URL accepted")
		}
	}
	if CoverURL("movie", "/ExamplePoster.jpg", 0) != "https://image.tmdb.org/t/p/w342/ExamplePoster.jpg" || CoverURL("book", "", 123) != "https://covers.openlibrary.org/b/id/123-M.jpg?default=false" || CoverURL("book", "", -1) != "" {
		t.Fatal("canonical cover URL lost")
	}
}
