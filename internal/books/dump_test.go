package books

import (
	"strings"
	"testing"
)

func TestCompactOpenLibraryExport(t *testing.T) {
	prefix := func(typ, key, json string) string { return typ + "\t" + key + "\t1\t2026-01-01T00:00:00\t" + json }
	work, err := DecodeLine([]byte(prefix("/type/work", "/works/OL123W", `{"key":"/works/OL123W","title":"The Amber Orchard","authors":[{"author":{"key":"/authors/OL456A"}}],"covers":[-1,789],"description":"unneeded description"}`)))
	if err != nil || work.Work == nil || work.Work.OpenLibrary != "OL123W" || work.Work.Book.Cover != 789 || len(work.Work.Book.AuthorKeys) != 1 || work.Work.TMDB != "" {
		t.Fatalf("work: %+v %v", work, err)
	}
	edition, err := DecodeLine([]byte(prefix("/type/edition", "/books/OL321M", `{"key":"/books/OL321M","title":"The Amber Orchard","works":[{"key":"/works/OL123W"}],"authors":[{"key":"/authors/OL456A"}],"isbn_10":["0306406152"],"isbn_13":["9780306406157","1234567890123"],"languages":[{"key":"/languages/eng"}],"publish_date":"2024","physical_format":"Paperback"}`)))
	if err != nil || edition.Edition == nil || len(edition.Edition.ISBN) != 1 || edition.Edition.Year != 2024 || edition.Edition.Normalized != "the amber orchard" {
		t.Fatalf("edition: %+v %v", edition, err)
	}
	if r, err := DecodeLine([]byte(prefix("/type/user", "/people/private", `{"password":"never retained"}`))); err != nil || r.Work != nil || r.Author != nil || r.Edition != nil {
		t.Fatal("irrelevant data retained")
	}
	if _, err := DecodeLine([]byte(strings.Replace(prefix("/type/work", "/works/OL123W", `{"key":"/works/OL124W","title":"Wrong"}`), "x", "x", 1))); err == nil {
		t.Fatal("mismatched identity accepted")
	}
}

func TestOptionalEditionFieldsDoNotAbortTheWholeExport(t *testing.T) {
	line := `/type/edition` + "\t/books/OL123M\t1\t2026-01-01\t" + `{"key":"/books/OL123M","title":"The Amber Orchard","isbn_13":["9780306406157",{}],"covers":[null,"invalid",123],"works":[null,{"key":"/works/OL100W"}],"physical_format":{"value":"unknown"},"publish_date":"1750"}`
	r, e := DecodeLine([]byte(line))
	if e != nil || r.Edition == nil || r.Edition.Cover != 123 || r.Edition.Year != 1750 || len(r.Edition.ISBN) != 1 {
		t.Fatalf("valid edition core lost: %+v %v", r, e)
	}
	if r, e := DecodeLine([]byte("/type/delete\t/people/example\t1\t2026-01-01\t{}")); e != nil || r.Deleted != "" {
		t.Fatal("non-bibliographic deletion was retained")
	}
}
