package books

import (
	"fmt"
	"reflect"
	"testing"
)

func TestEditionImportSelectionUsesDigitalEvidenceAndExplicitLocalReferences(t *testing.T) {
	selection := ImportSelection{RelevantOnly: true}
	selection.Include(Parse("The.Glass.Garden.EPUB", Hints{}))
	for _, test := range []struct {
		name, fields string
		want         bool
	}{
		{"paper only", `"physical_format":"Paperback"`, false},
		{"electronic edition", `"physical_format":"Electronic resource"`, true},
		{"audio cassette", `"physical_format":"Audio cassette"`, true},
		{"audiobook", `"physical_format":"Audiobook"`, true},
		{"sound recording", `"physical_format":"Sound recording"`, true},
		{"ebooks", `"physical_format":"eBooks"`, true},
		{"scanned print", `"physical_format":"Hardcover","ocaid":"synthetic_scan"`, true},
		{"archive provenance", `"source_records":["ia:synthetic_scan"]`, true},
		{"whitespace archive", `"ocaid":"  ","source_records":["ia:  "]`, false},
		{"retail provenance", `"source_records":["amazon:synthetic"]`, false},
		{"local title", `"title":"The Glass Garden","physical_format":"Hardcover"`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := []byte(fmt.Sprintf("/type/edition\t/books/OL100M\t1\t2026-01-01\t{\"key\":\"/books/OL100M\",\"title\":\"Unrelated Synthetic Book\",%s}", test.fields))
			probe, err := ProbeEdition(line)
			if err != nil {
				t.Fatal(err)
			}
			probe.Normalized = Normalize(probe.Title)
			full, err := DecodeLine(line)
			if err != nil || full.Edition == nil {
				t.Fatal(full, err)
			}
			if !reflect.DeepEqual(probe.Languages, full.Edition.Languages) || probe.Digital != full.Edition.Digital {
				t.Fatal("probe and import disagree", probe, full.Edition)
			}
			if got := selection.Allows(probe); got != test.want {
				t.Fatal(got, test.want)
			}
		})
	}
	selection.Identifiers = map[string]bool{"9780306406157": true, "OL500W": true}
	if !selection.Allows(Edition{ISBN: []string{"9780306406157"}}) || !selection.Allows(Edition{Works: []string{"OL500W"}}) {
		t.Fatal("local bibliographic references were excluded")
	}
	for _, title := range []string{"42", "A"} {
		selection.Include(Parse(title+".EPUB", Hints{}))
		if !selection.Allows(Edition{Normalized: Normalize(title)}) {
			t.Fatal("short or numeric local book title was excluded", title)
		}
	}
}

func TestBookLanguageSelectionIncludesTranslationsAndNormalizesCodes(t *testing.T) {
	values, ok := ImportLanguages([]string{"fre", "en", "fr", "eng"})
	if !ok || !reflect.DeepEqual(values, []string{"en", "fr"}) {
		t.Fatal(values, ok)
	}
	for _, bad := range [][]string{{""}, {"fr-CA"}, {"und"}, {"not-a-language"}} {
		if _, ok := ImportLanguages(bad); ok {
			t.Fatal("invalid language selection accepted", bad)
		}
	}
	selection := ImportSelection{RelevantOnly: true, Languages: []string{"fr"}}
	for _, code := range []string{"fre", "fra", "fr-CA"} {
		line := []byte(fmt.Sprintf("/type/edition\t/books/OL100M\t1\t2026-01-01\t{\"key\":\"/books/OL100M\",\"title\":\"Synthetic Translation\",\"physical_format\":\"EPUB\",\"languages\":[{\"key\":\"/languages/%s\"}]}", code))
		probe, err := ProbeEdition(line)
		if err != nil || !selection.Allows(probe) {
			t.Fatal("translation language was not normalized", code, probe, err)
		}
	}
	if selection.Allows(Edition{Digital: true, Languages: []string{"en"}}) || selection.Allows(Edition{Digital: true}) {
		t.Fatal("language filter admitted an unrelated or unknown language")
	}
	selection.Languages = nil
	if !selection.Allows(Edition{Digital: true}) {
		t.Fatal("language filtering was active without a selection")
	}
}

func TestEditionProbeToleratesHistoricalOptionalTypesLikeTheFullDecoder(t *testing.T) {
	line := []byte("/type/edition\t/books/OL100M\t1\t2026-01-01\t" + `{"key":"/books/OL100M","title":"Synthetic Edition","physical_format":"EPUB","languages":[7,{"key":"/languages/fre"}],"isbn_10":"invalid optional shape","source_records":{},"authors":[null,{"key":"/authors/OL200A"}]}`)
	probe, err := ProbeEdition(line)
	if err != nil {
		t.Fatal(err)
	}
	full, err := DecodeLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Digital != full.Edition.Digital || !reflect.DeepEqual(probe.Languages, full.Edition.Languages) || !reflect.DeepEqual(probe.Authors, full.Edition.Authors) {
		t.Fatal("historical optional fields changed selection", probe, full.Edition)
	}
}
