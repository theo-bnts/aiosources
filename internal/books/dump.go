package books

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
	"golang.org/x/text/language"
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
	p := strings.Split(s, "/")
	s = strings.ToUpper(p[len(p)-1])
	if keyRE.MatchString(s) {
		return s
	}
	return ""
}
func WorkKey(s string) bool {
	return keyRE.MatchString(s) && (strings.HasSuffix(s, "W") || strings.HasSuffix(s, "M"))
}

type Edition struct {
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
	Skipped  bool
	Redirect *Redirect
	Work     *model.AIOWork
	Edition  *Edition
	Author   *Author
	Deleted  string
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
	typ := string(cols[0])
	if typ != "/type/work" && typ != "/type/edition" && typ != "/type/author" && typ != "/type/delete" && typ != "/type/redirect" {
		return Record{}, nil
	}
	id := Key(string(cols[1]))
	if id == "" {
		if typ == "/type/delete" || typ == "/type/redirect" {
			return Record{}, nil
		}
		return Record{}, errors.New("invalid Open Library identifier")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(cols[4], &fields); err != nil {
		return Record{}, errors.New("invalid Open Library export JSON")
	}
	// Optional historical fields are not uniformly typed across all editions.
	// Keep the valid bibliographic core and ignore malformed optional values.
	text := func(key string) string { var out string; _ = json.Unmarshal(fields[key], &out); return out }
	stringsFor := func(key string) []string {
		var rows []json.RawMessage
		_ = json.Unmarshal(fields[key], &rows)
		out := []string{}
		for _, row := range rows {
			var value string
			if json.Unmarshal(row, &value) == nil {
				out = append(out, value)
			}
		}
		return out
	}
	references := func(key string) []reference {
		var rows []json.RawMessage
		_ = json.Unmarshal(fields[key], &rows)
		out := []reference{}
		for _, row := range rows {
			var ref reference
			if json.Unmarshal(row, &ref) == nil {
				out = append(out, ref)
			}
		}
		return out
	}
	var authorsRaw []json.RawMessage
	_ = json.Unmarshal(fields["authors"], &authorsRaw)
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
	authors := []string{}
	for _, v := range authorsRaw {
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
		e := Edition{ID: id, Title: title, Normalized: Normalize(title), Authors: authors, Cover: cover, Works: []string{}, ISBN: []string{}, Languages: []string{}}
		for _, w := range references("works") {
			if k := Key(w.Key); strings.HasSuffix(k, "W") && !slices.Contains(e.Works, k) {
				e.Works = append(e.Works, k)
			}
			if len(e.Works) == 20 {
				break
			}
		}
		for _, v := range append(stringsFor("isbn_10"), stringsFor("isbn_13")...) {
			if isbn := ISBN(v); isbn != "" && !slices.Contains(e.ISBN, isbn) && len(e.ISBN) < 50 {
				e.ISBN = append(e.ISBN, isbn)
			}
		}
		for _, l := range references("languages") {
			lang := strings.TrimPrefix(l.Key, "/languages/")
			if tag, e := language.Parse(lang); e == nil {
				base, _ := tag.Base()
				lang = base.String()
			}
			if (len(lang) == 2 || len(lang) == 3) && !slices.Contains(e.Languages, lang) && len(e.Languages) < 30 {
				e.Languages = append(e.Languages, lang)
			}
		}
		e.Year = publicationYear(text("publish_date"))
		e.Format = clean(text("physical_format"))
		if len(e.Works) == 0 {
			e.Works = []string{id}
		}
		return Record{Edition: &e}, nil
	}
	return Record{}, nil
}
