package books

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/language"
)

// Probe only the fields needed by the selection. Large descriptions, subject
// lists and source-specific metadata are skipped by the JSON decoder rather
// than copied into a map and decoded again for an excluded edition.
func ProbeEdition(line []byte) (Edition, error) {
	columns := bytes.SplitN(line, []byte{'\t'}, 5)
	if len(columns) != 5 || string(columns[0]) != "/type/edition" {
		return Edition{}, errors.New("invalid edition export record")
	}
	if revision, err := strconv.ParseInt(string(columns[2]), 10, 64); err != nil || revision < 1 {
		return Edition{}, errors.New("invalid edition revision")
	}
	var fields struct {
		Key       string          `json:"key"`
		Title     string          `json:"title"`
		Format    json.RawMessage `json:"physical_format"`
		Archive   json.RawMessage `json:"ocaid"`
		Sources   json.RawMessage `json:"source_records"`
		Languages json.RawMessage `json:"languages"`
		ISBN10    json.RawMessage `json:"isbn_10"`
		ISBN13    json.RawMessage `json:"isbn_13"`
		Works     json.RawMessage `json:"works"`
		Authors   json.RawMessage `json:"authors"`
	}
	if err := json.Unmarshal(columns[4], &fields); err != nil {
		return Edition{}, err
	}
	e := Edition{ID: Key(fields.Key), Title: fields.Title, Works: []string{}, ISBN: []string{}, Authors: []string{}, Languages: []string{}}
	if e.ID == "" || Key(string(columns[1])) != e.ID || !strings.HasSuffix(e.ID, "M") || strings.TrimSpace(e.Title) == "" {
		return Edition{}, errors.New("invalid edition identity")
	}
	var archive string
	_ = json.Unmarshal(fields.Archive, &archive)
	_ = json.Unmarshal(fields.Format, &e.Format)
	rows := func(raw json.RawMessage) []json.RawMessage {
		var out []json.RawMessage
		_ = json.Unmarshal(raw, &out)
		return out
	}
	sources := []string{}
	for _, raw := range rows(fields.Sources) {
		var v string
		if json.Unmarshal(raw, &v) == nil {
			sources = append(sources, v)
		}
	}
	e.Digital = digitalEvidence(e.Format, archive, sources)
	for _, raw := range rows(fields.Languages) {
		var ref reference
		if json.Unmarshal(raw, &ref) != nil {
			continue
		}
		if value := editionLanguage(ref.Key); value != "" && !slices.Contains(e.Languages, value) && len(e.Languages) < 30 {
			e.Languages = append(e.Languages, value)
		}
	}
	for _, raw := range append(rows(fields.ISBN10), rows(fields.ISBN13)...) {
		var v string
		_ = json.Unmarshal(raw, &v)
		if isbn := ISBN(v); isbn != "" && !slices.Contains(e.ISBN, isbn) && len(e.ISBN) < 50 {
			e.ISBN = append(e.ISBN, isbn)
		}
	}
	for _, raw := range rows(fields.Works) {
		var ref reference
		_ = json.Unmarshal(raw, &ref)
		if key := Key(ref.Key); strings.HasSuffix(key, "W") && !slices.Contains(e.Works, key) && len(e.Works) < 20 {
			e.Works = append(e.Works, key)
		}
	}
	if len(e.Works) == 0 {
		e.Works = []string{e.ID}
	}
	for _, raw := range rows(fields.Authors) {
		var ref reference
		_ = json.Unmarshal(raw, &ref)
		if key := Key(ref.Key); strings.HasSuffix(key, "A") && !slices.Contains(e.Authors, key) && len(e.Authors) < 100 {
			e.Authors = append(e.Authors, key)
		}
	}
	return e, nil
}

func ExportPartName(phase int) string {
	switch phase {
	case 1:
		return "editions"
	case 2:
		return "redirects"
	case 3:
		return "works"
	case 4:
		return "authors"
	}
	return ""
}

// A digital trace indicates a potentially useful edition, not an available
// download. Print editions can also qualify through an existing local book.
var digitalFormatRE = regexp.MustCompile(`(?i)\b(e[ -]?books?|electronic|digital|online|epub|pdf|mobi|kindle|audio(?:[ -]?books?)?|sound recording|cassette|compact disc|mp3|mp4|m4b|daisy|cd[ -]?rom|elektronisch\w*|numerique|numérique)\b`)

func digitalEvidence(format, archive string, sources []string) bool {
	if digitalFormatRE.MatchString(format) || strings.TrimSpace(archive) != "" {
		return true
	}
	for _, source := range sources {
		if strings.HasPrefix(source, "ia:") && strings.TrimSpace(strings.TrimPrefix(source, "ia:")) != "" {
			return true
		}
	}
	return false
}

func editionLanguage(key string) string {
	value := strings.TrimPrefix(key, "/languages/")
	if tag, err := language.Parse(value); err == nil {
		if tag == language.Und {
			return ""
		}
		base, _ := tag.Base()
		value = base.String()
	}
	if len(value) == 2 || len(value) == 3 {
		return value
	}
	return ""
}

type ImportSelection struct {
	RelevantOnly bool
	Languages    []string
	Titles       map[string]bool
	Identifiers  map[string]bool
}

func ImportLanguages(values []string) ([]string, bool) {
	if len(values) > 30 {
		return nil, false
	}
	out := []string{}
	for _, value := range values {
		if value == "" || len(value) > 3 {
			return nil, false
		}
		tag, err := language.Parse(value)
		if err != nil {
			return nil, false
		}
		if tag == language.Und {
			return nil, false
		}
		base, _ := tag.Base()
		code := base.String()
		if code == "und" || (len(code) != 2 && len(code) != 3) {
			return nil, false
		}
		if !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	slices.Sort(out)
	return out, true
}

func (s ImportSelection) Filtered() bool { return s.RelevantOnly || len(s.Languages) > 0 }

func (s ImportSelection) Allows(e Edition) bool {
	if len(s.Languages) > 0 && !slices.ContainsFunc(e.Languages, func(value string) bool { return slices.Contains(s.Languages, value) }) {
		return false
	}
	if !s.RelevantOnly || e.Digital || s.Titles[e.Normalized] || s.Identifiers[e.ID] {
		return true
	}
	for _, key := range append(slices.Clone(e.Works), e.ISBN...) {
		if s.Identifiers[key] {
			return true
		}
	}
	return false
}

// Reuse the release parser's alternatives. No source payloads, provider names
// or links are copied into the selection, and it is never published or saved.
func (s *ImportSelection) Include(record Release) {
	if record.Kind == "" {
		return
	}
	if s.Titles == nil {
		s.Titles = map[string]bool{}
	}
	if s.Identifiers == nil {
		s.Identifiers = map[string]bool{}
	}
	for _, title := range append([]string{record.Title}, record.Alternatives...) {
		if normalized := Normalize(title); normalized != "" {
			s.Titles[normalized] = true
		}
	}
	if record.ISBN != "" {
		s.Identifiers[record.ISBN] = true
	}
	if record.OpenLibrary != "" {
		s.Identifiers[record.OpenLibrary] = true
	}
}
