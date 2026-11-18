package books

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

var publicationYearRE = regexp.MustCompile(`(?i)\b(?:ca?\.?\s*)?((?:1[0-9]{3}|20[0-9]{2}|21[0-9]{2}))\b`)

func publicationYear(s string) int {
	if m := publicationYearRE.FindStringSubmatch(s); len(m) > 1 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

var keyRE = regexp.MustCompile(`^OL[1-9][0-9]{0,14}[WMA]$`)

func Key(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ToUpper(s)
	if keyRE.MatchString(s) {
		return s
	}
	return ""
}
func WorkKey(s string) bool {
	return keyRE.MatchString(s) && (strings.HasSuffix(s, "W") || strings.HasSuffix(s, "M"))
}

type Edition struct {
	Digital    bool     `json:"-"`
	ID         string   `json:"id"`
	Works      []string `json:"works"`
	Title      string   `json:"title"`
	Normalized string   `json:"normalized"`
	ISBN       []string `json:"isbn"`
	Authors    []string `json:"authors"`
	Languages  []string `json:"languages"`
	Year       int      `json:"year"`
	Format     string   `json:"format"`
	Cover      int64    `json:"cover"`
}
type Author struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}
type Record struct {
	PreserveKey string
	Revision    int64
	Modified    *time.Time
	Skipped     bool
	Redirect    *Redirect
	Work        *model.AIOWork
	Edition     *Edition
	Author      *Author
	Deleted     string
}
type Redirect struct{ Source, Target string }
type reference struct {
	Key string `json:"key"`
}

// DecodeLine consumes the official TSV export, not HTML or a per-book API.
// Irrelevant record types (including user data) are discarded immediately.
func DecodeLine(line []byte) (Record, error) {
	cols := bytes.SplitN(line, []byte{'\t'}, 5)
	if len(cols) != 5 {
		return Record{}, errors.New("invalid Open Library export record")
	}
	revision, err := strconv.ParseInt(string(cols[2]), 10, 64)
	if err != nil || revision < 1 {
		return Record{}, errors.New("invalid Open Library export revision")
	}
	record, err := decodeRecord(string(cols[0]), string(cols[1]), cols[4])
	record.Revision = revision
	if modified, e := ParseTimestamp(string(cols[3])); e == nil {
		record.Modified = &modified
	}
	return record, err
}

func (r Record) Key() string {
	switch {
	case r.Work != nil:
		return r.Work.OpenLibrary
	case r.Edition != nil:
		return r.Edition.ID
	case r.Author != nil:
		return r.Author.ID
	case r.Redirect != nil:
		return r.Redirect.Source
	default:
		return r.Deleted
	}
}

func decodeRecord(typ, key string, body []byte) (Record, error) {
	if typ != "/type/work" && typ != "/type/edition" && typ != "/type/author" && typ != "/type/delete" && typ != "/type/redirect" {
		return Record{}, nil
	}
	id := Key(key)
	if id == "" {
		if typ == "/type/delete" || typ == "/type/redirect" {
			return Record{}, nil
		}
		return Record{}, errors.New("invalid Open Library identifier")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return Record{}, errors.New("invalid Open Library export JSON")
	}
	// Optional historical fields are not uniformly typed across all editions.
	// Keep the valid bibliographic core and ignore malformed optional values.
	text := func(key string) string { var out string; _ = json.Unmarshal(fields[key], &out); return out }
	stringsFor := func(key string) []string { return decodeJSONStrings(fields[key]) }
	var covers []json.RawMessage
	_ = json.Unmarshal(fields["covers"], &covers)
	if typ == "/type/delete" {
		return Record{Deleted: id}, nil
	}
	if Key(text("key")) != id {
		return Record{}, errors.New("Open Library export identity mismatch")
	}
	if typ == "/type/redirect" {
		target := Key(text("location"))
		if target == "" || target == id {
			return Record{}, errors.New("invalid Open Library redirect")
		}
		return Record{Redirect: &Redirect{Source: id, Target: target}}, nil
	}
	clean := func(s string) string {
		s = strings.Join(strings.Fields(s), " ")
		if len(s) > 1024 {
			return ""
		}
		return s
	}
	authors := decodeAuthorKeys(fields["authors"])
	cover := int64(0)
	for _, c := range covers {
		var number int64
		if json.Unmarshal(c, &number) == nil && number > 0 {
			cover = number
			break
		}
	}
	title := clean(text("title"))
	switch typ {
	case "/type/author":
		if !strings.HasSuffix(id, "A") || clean(text("name")) == "" {
			return Record{Skipped: true}, nil
		}
		aliases := []string{}
		for _, a := range stringsFor("alternate_names") {
			if a = clean(a); a != "" && len(aliases) < 50 {
				aliases = append(aliases, a)
			}
		}
		return Record{Author: &Author{ID: id, Name: clean(text("name")), Aliases: aliases}}, nil
	case "/type/work":
		if !strings.HasSuffix(id, "W") || title == "" {
			return Record{Skipped: true}, nil
		}
		years := []int{}
		if year := publicationYear(text("first_publish_date")); year > 0 {
			years = append(years, year)
		}
		return Record{Work: &model.AIOWork{Type: "book", OpenLibrary: id, DisplayTitle: title, Titles: []string{title}, Years: years, Book: model.BookInformation{AuthorKeys: authors, Cover: cover}, LocalizedTitles: map[string]string{}}}, nil
	case "/type/edition":
		if !strings.HasSuffix(id, "M") || title == "" {
			return Record{Skipped: true}, nil
		}
		e := Edition{
			ID:         id,
			Title:      title,
			Normalized: Normalize(title),
			Authors:    authors,
			Cover:      cover,
			Works:      decodeEditionWorks(fields["works"], id),
			ISBN:       decodeEditionISBNs(fields["isbn_10"], fields["isbn_13"]),
			Languages:  decodeEditionLanguages(fields["languages"]),
			Year:       publicationYear(text("publish_date")),
			Format:     clean(text("physical_format")),
		}
		e.Digital = digitalEvidence(e.Format, text("ocaid"), stringsFor("source_records"))
		return Record{Edition: &e}, nil
	}
	return Record{}, nil
}

func decodeAuthorKeys(raw json.RawMessage) []string {
	var rows []json.RawMessage
	_ = json.Unmarshal(raw, &rows)
	authors := []string{}
	for _, v := range rows {
		var a struct {
			Key    string    `json:"key"`
			Author reference `json:"author"`
		}
		if json.Unmarshal(v, &a) != nil {
			continue
		}
		k := Key(a.Key)
		if k == "" {
			k = Key(a.Author.Key)
		}
		if k != "" && strings.HasSuffix(k, "A") && !slices.Contains(authors, k) {
			authors = append(authors, k)
		}
		if len(authors) == 100 {
			break
		}
	}
	return authors
}

func decodeEditionWorks(raw json.RawMessage, editionID string) []string {
	works := []string{}
	for _, ref := range decodeJSONReferences(raw) {
		if key := Key(ref.Key); strings.HasSuffix(key, "W") && !slices.Contains(works, key) {
			works = append(works, key)
			if len(works) == 20 {
				break
			}
		}
	}
	if len(works) == 0 {
		works = []string{editionID}
	}
	return works
}

func decodeEditionISBNs(isbn10, isbn13 json.RawMessage) []string {
	out := []string{}
	for _, v := range append(decodeJSONStrings(isbn10), decodeJSONStrings(isbn13)...) {
		if isbn := ISBN(v); isbn != "" && !slices.Contains(out, isbn) && len(out) < 50 {
			out = append(out, isbn)
		}
	}
	return out
}

func decodeEditionLanguages(raw json.RawMessage) []string {
	out := []string{}
	for _, ref := range decodeJSONReferences(raw) {
		if value := editionLanguage(ref.Key); value != "" && !slices.Contains(out, value) && len(out) < 30 {
			out = append(out, value)
		}
	}
	return out
}

func decodeJSONStrings(raw json.RawMessage) []string {
	var rows []json.RawMessage
	_ = json.Unmarshal(raw, &rows)
	out := []string{}
	for _, row := range rows {
		var value string
		if json.Unmarshal(row, &value) == nil {
			out = append(out, value)
		}
	}
	return out
}

func decodeJSONReferences(raw json.RawMessage) []reference {
	var rows []json.RawMessage
	_ = json.Unmarshal(raw, &rows)
	out := []reference{}
	for _, row := range rows {
		var ref reference
		if json.Unmarshal(row, &ref) == nil {
			out = append(out, ref)
		}
	}
	return out
}
