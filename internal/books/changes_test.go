package books

import (
	"strings"
	"testing"
	"time"
)

func TestBookChangeFeedKeepsOnlyBibliographicEvidence(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	rows, err := DecodeChanges([]byte(`[{"id":"123","timestamp":"2026-01-02T11:00:00.123456","kind":"future-action","author":{"key":"/people/example"},"ip":"192.0.2.1","comment":"unused","changes":[{"key":"/works/OL100W","revision":7},{"key":"/people/example","revision":2}]}]`), now)
	if err != nil || len(rows) != 1 || len(rows[0].Keys) != 1 || rows[0].Keys[0].Revision != 7 || rows[0].Keys[0].ID != "OL100W" {
		t.Fatalf("bibliographic change lost: %+v %v", rows, err)
	}
	for _, body := range []string{`null`, `{}`, `[{"id":"1","timestamp":"2026-01-02T11:00:00"}]`, `[{"id":"1","timestamp":"2026-01-02T11:00:00","changes":[{"key":"/works/OL100W","revision":0}]}]`, `[{"id":"1","timestamp":"2026-01-03T11:00:00","changes":[]}]`, `[{"id":"1","timestamp":"2026-01-02T11:00:00","changes":[{"key":"/authors/OL100W","revision":1}]}]`} {
		if _, err := DecodeChanges([]byte(body), now); err == nil {
			t.Fatalf("unverifiable response accepted: %s", body)
		}
	}
}

func TestBookDocumentsRequireTheObservedRevisionAndIdentity(t *testing.T) {
	body := `{"key":"/works/OL100W","type":{"key":"/type/work"},"revision":3,"title":"The Amber Orchard","covers":[123],"last_modified":{"value":"2026-01-02T10:00:00"}}`
	record, err := DecodeDocument("OL100W", 2, []byte(body))
	if err != nil || record.Work == nil || record.Revision != 3 || record.Work.Book.Cover != 123 || record.Modified == nil {
		t.Fatal("latest valid document rejected", err)
	}
	for _, candidate := range []string{strings.Replace(body, `"revision":3`, `"revision":1`, 1), strings.Replace(body, "OL100W", "OL200W", 1), strings.Replace(body, `"/type/work"`, `"/type/list"`, 1)} {
		if _, err := DecodeDocument("OL100W", 2, []byte(candidate)); err == nil {
			t.Fatal("stale or mismatched document accepted")
		}
	}
	if at, ok := ExportDate("https://archive.org/download/ol_dump_2026-01-01/ol_dump_all.txt.gz"); !ok || at.Format("2006-01-02") != "2026-01-01" {
		t.Fatal("snapshot boundary lost")
	}
	if _, ok := ExportDate("https://openlibrary.org/data/ol_dump_latest.txt.gz"); ok {
		t.Fatal("download time was invented as snapshot boundary")
	}
}

func TestBookGroupedReadsNeverTreatMissingOrStaleDocumentsAsDeleted(t *testing.T) {
	body := `{"status":"ok","result":{
 "/works/OL100W":{"key":"/works/OL100W","type":{"key":"/type/work"},"revision":2,"title":"The Amber Orchard"},
 "/works/OL200W":{"key":"/works/OL200W","type":{"key":"/type/work"},"revision":1,"title":"The Azure Almanac"},
 "/people/example":{"key":"/people/example","email":"unused@example.invalid"}}}`
	records, err := DecodeDocuments([]byte(body), map[string]int64{"OL100W": 2, "OL200W": 2, "OL300W": 1})
	if err != nil || len(records) != 1 || records["OL100W"].Work == nil {
		t.Fatalf("unexpected grouped evidence: %+v %v", records, err)
	}
	for _, value := range []string{`null`, `[]`, `{"status":"fail","result":{}}`, `{"status":"ok"}`} {
		if _, err := DecodeDocuments([]byte(value), map[string]int64{}); err == nil {
			t.Fatal("invalid grouped response accepted", value)
		}
	}
}

func TestBookFeedClocksUseTheSamePrecisionAsDurableCheckpoints(t *testing.T) {
	at, err := ParseTimestamp("2026-01-02T11:00:00.123456789Z")
	if err != nil || at.Nanosecond() != 123456000 {
		t.Fatal("finer API clocks would make a saved feed boundary unrecognizable", at, err)
	}
}
