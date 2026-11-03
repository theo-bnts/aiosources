// Package books parses bibliographic evidence without changing video parsing.
package books

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type Hints struct {
	OpenLibrary                string
	ISBNs                      []string
	Kind, Author, ISBN, Format string
	Categories                 []int
	CategoryNames              []string
}

type Release struct {
	StrippedYear                      bool
	Variants                          []string
	OpenLibrary                       string
	InvalidReference                  bool
	InvalidISBN                       bool
	Title, Author, ISBN, Kind, Format string
	Alternatives                      []string
	Year                              int
	Collection                        bool
}

var isbnRE = regexp.MustCompile(`(?i)\b(?:ISBN(?:[- ]?1[03])?\s*[:=]?\s*)?((?:97[89][- ]?)?[0-9](?:[- ]?[0-9]){8}[- ]?[0-9X])\b`)
var formatRE = regexp.MustCompile(`(?i)\b(epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?)\b`)
var audioRE = regexp.MustCompile(`(?i)\b(?:audio[ ._-]?books?|livres?[ ._-]?audio|audiolivres?|audiobook|unabridged|abridged)\b`)
var ebookRE = regexp.MustCompile(`(?i)\b(?:e[ ._-]?books?|ebooks?|livres?|romans?|comics?|mangas?|bandes?[ ._-]?dessin[ée]es?)\b`)
var videoRE = regexp.MustCompile(`(?i)\b(?:[48]k|\d{3,4}[pi]|bluray|b[dr]rip|hdtv|s\d{1,3}e\d{1,4})\b|\.(?:mkv|mp4|avi|webm)$`)
var yearRE = regexp.MustCompile(`\b(?:18|19|20|21)\d{2}\b`)
var collectionRE = regexp.MustCompile(`(?i)\b(?:pack\s*(?:de\s*)?\d+|\d+\s*(?:books?|ebooks?|livres?|epubs?)|(?:tomes?|volumes?|books?|livres?)\s*\d+\s*[-–]\s*\d+|(?:integrale|intégrale|complete)\s*(?:collection|series|saga)|(?:collection|series|saga)\s*(?:integrale|intégrale|complete))\b|(?i)\[(?:pack|integrale|intégrale|complete)\]`)

var groupRE = regexp.MustCompile(`\[[^\]]*\]`)

// ISBN returns a checksum-validated ISBN-13, making ISBN-10 and ISBN-13
// editions comparable. Other identifiers and arbitrary thirteen digits fail.
func ISBN(raw string) string {
	s := strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(raw)))
	if len(s) == 10 {
		sum := 0
		for i, r := range s {
			n := int(r - '0')
			if r == 'X' && i == 9 {
				n = 10
			}
			if n < 0 || n > 9 && !(n == 10 && i == 9) {
				return ""
			}
			sum += (10 - i) * n
		}
		if sum%11 != 0 {
			return ""
		}
		s = "978" + s[:9]
		sum = 0
		for i, r := range s {
			weight := 1
			if i%2 == 1 {
				weight = 3
			}
			sum += int(r-'0') * weight
		}
		return s + strconv.Itoa((10-sum%10)%10)
	}
	if len(s) != 13 || !(strings.HasPrefix(s, "978") || strings.HasPrefix(s, "979")) {
		return ""
	}
	sum := 0
	for i, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += int(r-'0') * weight
	}
	if sum%10 != 0 {
		return ""
	}
	return s
}

var technicalBookGroupRE = regexp.MustCompile(`(?i)^(?:(?:ebooks?|books?|audiobooks?|pdf|epub|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|mp3|flac|aac|m4a|fr|en|eng|fre|fra|french|english|multi|retail|ocr|scan|unabridged|abridged|vbr|cbr|\d{2,3}(?:kbps)?)[ ._+/-]*)+$`)

// Full language names can be part of a real title (for example a language
// guide). Only abbreviations or explicit bracketed groups are decorations.
var languageSuffixRE = regexp.MustCompile(`(?i)(?:\s+(?:fr|en|eng|fre|fra|multi|vf|vff|vfq))+\s*$`)

// A language name on its own can belong to a title. Following a release-year
// annotation it is technical metadata; keep the original title as an alias.
var yearLanguageSuffixRE = regexp.MustCompile(`(?i)(\b(?:18|19|20|21)\d{2})\s+(?:(?:french|english|german|spanish|italian|portuguese|dutch|russian|japanese|chinese|korean|arabic|polish|swedish|danish|norwegian|finnish)\s*)+$`)
var volumePackRE = regexp.MustCompile(`(?i)\bT\d{1,3}(?:(?:[ ._-]+|\s*[&,/+]+\s*)T?\d{1,3})+\b`)
var formatTailRE = regexp.MustCompile(`(?i)^[\s._()\[\]+/-]*(?:(?:epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|mp3|flac|aac|m4a|ogg|opus|wav|e[ ._-]?books?|audio[ ._-]?books?|fr|en|eng|fre|fra|french|english|multi|retail|ocr|scan|vbr|cbr|\d{2,3}(?:kbps)?|v\d+|[12]\d{3})[\s._()\[\]+/-]*)*(?:-[A-Za-z0-9_-]{2,40})?$`)
var bookTailFormatRE = regexp.MustCompile(`(?i)\b(?:epub|pdf|mobi|azw3?|fb2|djvu|cb[rz7ta]|m4b|aaxc?|mp3|flac|aac|m4a|ogg|opus|wav|zip|rar|7z|txt|rtf|docx?|e[ ._-]?books?|audio[ ._-]?books?)\b`)
var bookLabelSuffixRE = regexp.MustCompile(`(?i)\be[ ._-]?books?\s*$`)
var audioFileRE = regexp.MustCompile(`(?i)\b(?:mp3|flac|aac|m4a|ogg|opus|wav)\b`)
var retailYearSuffixRE = regexp.MustCompile(`(?i)(\b(?:18|19|20|21)\d{2})\s+(?:retail|ocr|scan)\s*$`)
var duplicateBookTagRE = regexp.MustCompile(`(?i)[ ._-]+(?:epub|pdf|mobi|azw3?|fb2)-notag[ ._]*$`)

// ParseRecord shares bibliographic classification between identification and
// maintenance without introducing a second set of source/category heuristics.
func ParseRecord(record model.AIORecord) Release {
	h := record.BookHints
	if h.Title != "" && h.Kind == "" {
		h.Kind = "book"
	}
	p := Parse(record.Title, Hints{OpenLibrary: h.OpenLibrary, ISBNs: h.ISBNs, Kind: h.Kind, Author: h.Author, ISBN: h.ISBN, Format: h.Format, Categories: h.Categories, CategoryNames: h.CategoryNames})
	if p.Kind != "" && h.Title != "" {
		p.Title, p.Alternatives, p.Variants, p.StrippedYear = h.Title, nil, nil, false
	}
	return p
}

func Parse(raw string, hints Hints) Release {
	// Video releases dominate existing catalogs. The same exclusion used at
	// the end of parsing can reject them before bibliographic allocations.
	if videoRE.MatchString(raw) {
		return Release{}
	}
	r := Release{Author: strings.TrimSpace(hints.Author), ISBN: ISBN(hints.ISBN)}
	if ref := strings.TrimSpace(hints.OpenLibrary); ref != "" && ref != "0" && ref != "null" {
		key := Key(ref)
		if WorkKey(key) {
			r.OpenLibrary = key
		} else {
			r.InvalidReference = true
		}
	}
	for _, value := range append(slices.Clone(hints.ISBNs), hints.ISBN) {
		value = strings.TrimSpace(value)
		if value == "" || value == "0" || value == "null" {
			continue
		}
		canonical := ISBN(value)
		if canonical == "" {
			r.InvalidISBN = true
			continue
		}
		if r.ISBN != "" && r.ISBN != canonical {
			r.InvalidISBN = true
		} else {
			r.ISBN = canonical
		}
	}
	text := strings.ReplaceAll(strings.ReplaceAll(raw, "_", " "), ".", " ")
	if r.ISBN == "" {
		for _, m := range isbnRE.FindAllStringSubmatch(raw, -1) {
			if v := ISBN(m[1]); v != "" {
				if strings.HasPrefix(strings.ToLower(m[0]), "isbn") || strings.HasPrefix(strings.ReplaceAll(m[1], "-", ""), "97") || formatRE.MatchString(raw) {
					r.ISBN = v
				}
				break
			}
		}
	}
	for _, c := range hints.Categories {
		if c == 3030 {
			r.Kind = "audiobook"
		}
		if c >= 7000 && c < 8000 && r.Kind == "" {
			r.Kind = "ebook"
		}
	}
	category := strings.Join(hints.CategoryNames, " ")
	if ebookRE.MatchString(category) {
		r.Kind = "ebook"
	}
	if audioRE.MatchString(category) || audioRE.MatchString(text) {
		r.Kind = "audiobook"
	}
	if slices.Contains([]string{"book", "ebook", "audiobook"}, strings.ToLower(hints.Kind)) {
		r.Kind = strings.ToLower(hints.Kind)
	}
	if matches := formatRE.FindAllStringSubmatchIndex(raw, -1); len(matches) > 0 {
		match := matches[len(matches)-1]
		format := strings.ToLower(raw[match[2]:match[3]])
		if formatTailRE.MatchString(raw[match[1]:]) {
			r.Format = format
			r.Kind = bookFormatKind(format)
		}
	}
	if hints.Format != "" {
		r.Format = strings.ToLower(strings.TrimPrefix(hints.Format, "."))
		if kind := bookFormatKind(r.Format); kind != "" {
			r.Kind = kind
		}
	}
	if r.Kind == "" && (r.ISBN != "" || r.OpenLibrary != "" || r.InvalidReference) {
		r.Kind = "book"
	}
	if r.Kind == "" && bookLabelSuffixRE.MatchString(text) {
		r.Kind = "ebook"
	}
	if r.Kind == "" {
		return Release{}
	}
	// Audio codecs alone do not classify music as books. Once bibliographic
	// evidence exists, a technical audio suffix identifies the book's format.
	for _, at := range groupRE.FindAllStringIndex(raw, -1) {
		group := strings.Trim(raw[at[0]:at[1]], "[] ")
		if hints.Format == "" && technicalBookGroupRE.MatchString(group) && !formatRE.MatchString(raw[at[1]:]) {
			if format := audioFileRE.FindString(group); format != "" {
				r.Kind, r.Format = "audiobook", strings.ToLower(format)
			}
		}
	}
	for _, at := range audioFileRE.FindAllStringIndex(raw, -1) {
		if hints.Format == "" && !formatRE.MatchString(raw[at[1]:]) && formatTailRE.MatchString(raw[at[1]:]) {
			r.Kind, r.Format = "audiobook", strings.ToLower(raw[at[0]:at[1]])
			break
		}
	}
	// An ISBN followed by EPUB is one book, not a thirteen-digit book count.
	collectionText := isbnRE.ReplaceAllStringFunc(text, func(value string) string {
		match := isbnRE.FindStringSubmatch(value)
		if len(match) > 1 && ISBN(match[1]) != "" {
			return " "
		}
		return value
	})
	r.Collection = collectionRE.MatchString(collectionText) || volumePackRE.MatchString(collectionText)
	if years := yearRE.FindAllString(text, -1); len(years) > 0 {
		r.Year, _ = strconv.Atoi(years[len(years)-1])
	}
	formats := bookTailFormatRE.FindAllStringIndex(raw, -1)
	for i := len(formats) - 1; i >= 0; i-- {
		at := formats[i]
		if !formatTailRE.MatchString(raw[at[1]:]) {
			continue
		}
		cut := at[0]
		if bracket := strings.LastIndex(raw[:cut], "["); bracket >= 0 && !strings.Contains(raw[bracket:cut], "]") {
			cut = bracket
		}
		if bracket := strings.LastIndex(raw[:cut], "("); bracket >= 0 && !strings.Contains(raw[bracket:cut], ")") {
			cut = bracket
		}
		raw = raw[:cut]
		raw = duplicateBookTagRE.ReplaceAllString(raw, "")
		break
	}
	text = strings.ReplaceAll(strings.ReplaceAll(raw, "_", " "), ".", " ")
	text = isbnRE.ReplaceAllStringFunc(text, func(s string) string {
		if ISBN(strings.TrimSpace(s)) != "" || strings.HasPrefix(strings.ToLower(s), "isbn") {
			return " "
		}
		return s
	})
	text = groupRE.ReplaceAllStringFunc(text, func(group string) string {
		inner := strings.Trim(group, "[] ")
		if technicalBookGroupRE.MatchString(inner) {
			return " "
		}
		return " " + inner + " "
	})
	text = strings.Trim(strings.Join(strings.Fields(text), " "), " -()[]")
	originalText := text
	text = languageSuffixRE.ReplaceAllString(text, "")
	text = yearLanguageSuffixRE.ReplaceAllString(text, "$1")
	text = retailYearSuffixRE.ReplaceAllString(text, "$1")
	reversedHint := false
	if a, b, ok := strings.Cut(text, " - "); ok && a != "" && b != "" {
		if r.Author == "" {
			r.Author = strings.TrimSpace(a)
		}
		r.Title = strings.TrimSpace(b)
		r.Alternatives = []string{strings.TrimSpace(a)}
		if hints.Author != "" && AuthorSignature(b) == AuthorSignature(hints.Author) {
			r.Title, r.Alternatives, reversedHint = strings.TrimSpace(a), nil, true
		}
	} else {
		r.Title = text
	}
	if hints.Author != "" {
		r.Title = strings.TrimSpace(strings.TrimPrefix(r.Title, hints.Author))
		r.Title = strings.Trim(r.Title, " -")
	}
	// A numeric title is bibliographic evidence. Strip only a trailing
	// publication annotation after finding the author/title boundary.
	withoutYear := stripBookYear(r.Title)
	r.StrippedYear = withoutYear != r.Title
	if r.StrippedYear {
		r.Variants = append(r.Variants, r.Title)
	}
	r.Title = withoutYear
	originalTitle := originalText
	if before, after, ok := strings.Cut(originalText, " - "); ok {
		originalTitle = strings.TrimSpace(after)
		if reversedHint {
			originalTitle = strings.TrimSpace(before)
		}
	}
	if originalTitle != "" && originalTitle != r.Title {
		r.Variants = append(r.Variants, originalTitle)
	}
	if strconv.Itoa(r.Year) == strings.TrimSpace(r.Title) {
		r.Year = 0
	}

	return r
}

var trailingYearRE = regexp.MustCompile(`\s+[([]?((?:18|19|20|21)\d{2})[)\]]?\s*$`)

func stripBookYear(s string) string {
	if at := trailingYearRE.FindStringIndex(s); at != nil && len(strings.TrimSpace(s[:at[0]])) > 2 {
		return strings.TrimSpace(s[:at[0]])
	}
	return s
}

func Normalize(s string) string { return model.NormalizeTitle(s) }

func bookFormatKind(format string) string {
	switch format {
	case "m4b", "aax", "aaxc", "mp3", "flac", "aac", "m4a", "ogg", "opus", "wav":
		return "audiobook"
	case "epub", "pdf", "mobi", "azw", "azw3", "fb2", "djvu", "cbr", "cbz", "cb7", "cba", "cbt":
		return "ebook"
	}
	return ""
}

func IsType(s string) bool { return s == "book" || s == "ebook" || s == "audiobook" }
