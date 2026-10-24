package model

import (
	"encoding/json"
	"strings"
	"testing"
)

var allCatalogDetails = CatalogEntities{Identifiers: true, Languages: true, Technical: true, Files: true}

func TestCatalogDetailsPreserveEvidenceAndExcludePrivatePayloads(t *testing.T) {
	var source map[string]any
	decoder := json.NewDecoder(strings.NewReader(`{
"imdb_id":"null","tmdb_id":1234567890123456,"season":["1","2"],"year":"2025",
"attributes":{"imdbid":["tt0000101","tt0000102"],"audiochannels":"5.1","infohash":"do-not-send","apikey":"private-key"},
"external_ids":{"tvdb_id":"TT-invalid-private-value","mal":null},
"language":["fr","en"],"hdr":"HDR10","filename":"Folder/Example.mkv","file_size":9007199254740993,
"subtitles":[{"language":"fr","url":"https://private.invalid/subtitle","id":"private-id"}],
"metadata":{"resolution":"1080p","announce":"https://private.invalid/announce"},
"download_url":"https://private.invalid/download","private":true,"raw":{"auth":"private"}}`))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		t.Fatal(err)
	}
	projected := ProjectCatalogDetails(source, allCatalogDetails)
	if !ValidCatalogDetails(projected, allCatalogDetails) {
		t.Fatal("projected source information is not a valid bounded payload")
	}
	if projected["tmdb_id"] != json.Number("1234567890123456") || projected["file_size"] != json.Number("9007199254740993") || projected["imdb_id"] != "null" {
		t.Fatal("exact numeric evidence or absent ID semantics changed")
	}
	data, _ := json.Marshal(projected)
	for _, private := range []string{"private", "do-not-send", "apikey", "url", "announce", "raw"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("private payload survived: %s", private)
		}
	}
	if string(source["file_size"].(json.Number)) != "9007199254740993" {
		t.Fatal("projection mutated the original source facts")
	}
	if projected["external_ids"].(map[string]any)["tvdb_id"] != catalogConflicting {
		t.Fatal("malformed ID evidence was silently dropped")
	}
}

func TestCatalogDetailsConsentAndStrictValidation(t *testing.T) {
	for _, invalid := range []string{"files,files", "keys", "languages,,technical", strings.Repeat("a", 65)} {
		if _, ok := ParseCatalogDetails(invalid); ok {
			t.Fatalf("accepted invalid permission header %q", invalid)
		}
	}
	requested, ok := ParseCatalogDetails(allCatalogDetails.DetailsHeader())
	if !ok || requested != allCatalogDetails || requested.Header() != "" {
		t.Fatal("new permissions would break old peers")
	}
	fields := map[string]any{"language": "fr", "duration": json.Number("7200"), "tmdb_id": "101"}
	if ValidCatalogDetails(fields, CatalogEntities{Languages: true}) || ValidCatalogDetails(map[string]any{"attributes": map[string]any{"password": "hidden"}}, allCatalogDetails) || ValidCatalogDetails(map[string]any{"filename": "../private/file"}, allCatalogDetails) {
		t.Fatal("unrequested or unsafe details accepted")
	}
	if got := ProjectCatalogDetails(fields, CatalogEntities{Languages: true}); len(got) != 1 || got["language"] != "fr" {
		t.Fatal("one permission granted unrelated fields")
	}
	for _, malicious := range []string{"https://private.invalid/file", "magnet:?xt=private", "file?apikey=private", "Bearer private", "file\nsecret", "/private/file", "Folder/../file"} {
		if ValidCatalogDetails(map[string]any{"filename": malicious}, allCatalogDetails) {
			t.Fatalf("unsafe file information accepted: %q", malicious)
		}
	}
}

func TestCatalogDetailsRemainBoundedWithLargeSourceArrays(t *testing.T) {
	fields := map[string]any{}
	for key, group := range catalogDetailGroups {
		values := make([]any, 16)
		for i := range values {
			if group == "identifiers" {
				values[i] = json.Number(strings.Repeat("9", 64))
			} else {
				values[i] = strings.Repeat("x", 128)
			}
		}
		fields[key] = values
	}
	source := map[string]any{"attributes": fields, "external_ids": fields, "metadata": fields}
	for key, value := range fields {
		source[key] = value
	}
	projected := ProjectCatalogDetails(source, allCatalogDetails)
	if !ValidCatalogDetails(projected, allCatalogDetails) {
		t.Fatal("pathological arrays generated an invalid or oversized export")
	}
}
