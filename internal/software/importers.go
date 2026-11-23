package software

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// ErrNVDChecksumMismatch reports that a streamed nvdcpe-2.0.tar.gz archive did
// not match the SHA-256 digest published in nvdcpe-2.0.meta.
var ErrNVDChecksumMismatch = errors.New("nvd archive sha256 mismatch")

func sanitizeUTF8(s string) string {
	if s == "" || (!strings.ContainsRune(s, 0) && utf8.ValidString(s)) {
		return s
	}
	s = strings.ToValidUTF8(s, "")
	return strings.ReplaceAll(s, "\x00", "")
}

func sanitizeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if cleaned := strings.TrimSpace(sanitizeUTF8(s)); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func sanitizeSourceEntry(e SourceEntry) SourceEntry {
	e.Name = strings.TrimSpace(sanitizeUTF8(e.Name))
	e.Vendor = strings.TrimSpace(sanitizeUTF8(e.Vendor))
	e.VendorNorm = strings.TrimSpace(sanitizeUTF8(e.VendorNorm))
	e.ProductNorm = strings.TrimSpace(sanitizeUTF8(e.ProductNorm))
	e.Description = strings.TrimSpace(sanitizeUTF8(e.Description))
	e.License = strings.TrimSpace(sanitizeUTF8(e.License))
	e.LatestVersion = strings.TrimSpace(sanitizeUTF8(e.LatestVersion))
	e.HomepageKey = strings.TrimSpace(sanitizeUTF8(e.HomepageKey))
	e.Platforms = sanitizeStrings(e.Platforms)
	e.Tags = sanitizeStrings(e.Tags)
	e.Languages = sanitizeStrings(e.Languages)
	e.Aliases = sanitizeStrings(e.Aliases)
	return e
}

const (
	// MaxNVDIncrementalWindow defines the safe maximum checkpoint age before
	// switching automatically to a full nvdcpe-2.0.tar.gz re-import (NVD API
	// caps individual requests at 120 days).
	MaxNVDIncrementalWindow = 110 * 24 * time.Hour

	// MaxNVDIncrementalResults bounds the volume of incremental CPE records
	// before preferring a full compressed snapshot re-import.
	MaxNVDIncrementalResults = 50000

	maxWinGetManifestBytes = 256 * 1024
)

// SourceEntry is a normalized, source-scoped software product record stored in
// sw_source_entries before deterministic cross-source merging and projection.
type SourceEntry struct {
	Source         string // "nvd", "winget", "homebrew"
	ExternalID     string // canonical CPE 2.3 product id, WinGet ID, or Homebrew cask token
	Part           string // "a" (application) or "o" (operating system)
	Name           string
	Vendor         string
	VendorNorm     string
	ProductNorm    string
	Description    string
	License        string
	LatestVersion  string
	HomepageKey    string
	Platforms      []string
	Tags           []string
	Languages      []string
	Aliases        []string
	IsPlugin       bool
	LastModifiedAt *time.Time
}

// IconEntry stores lightweight metadata from selfh.st/icons (zero image bytes).
type IconEntry struct {
	Reference  string
	Name       string
	Normalized string
}

// NVDMeta holds parsed metadata from nvdcpe-2.0.meta.
type NVDMeta struct {
	LastModifiedRaw string
	LastModifiedAt  *time.Time
	Size            int64
	ZipSize         int64
	GzSize          int64
	SHA256          string
}

// ParseNVDMeta parses NIST NVD's key:value .meta feed descriptor.
func ParseNVDMeta(r io.Reader) (NVDMeta, error) {
	var meta NVDMeta
	scanner := bufio.NewScanner(io.LimitReader(r, 64*1024))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "lastmodifieddate":
			meta.LastModifiedRaw = val
			if ts, ok := parseNVDTime(val); ok {
				meta.LastModifiedAt = &ts
			}
		case "size":
			meta.Size, _ = strconv.ParseInt(val, 10, 64)
		case "zipsize":
			meta.ZipSize, _ = strconv.ParseInt(val, 10, 64)
		case "gzsize":
			meta.GzSize, _ = strconv.ParseInt(val, 10, 64)
		case "sha256":
			low := strings.ToLower(val)
			if len(low) == 64 && isHex(low) {
				meta.SHA256 = low
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return NVDMeta{}, err
	}
	if meta.SHA256 == "" {
		return NVDMeta{}, errors.New("nvd meta file is missing a valid sha256")
	}
	return meta, nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func parseNVDTime(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000-07:00",
		"2006-01-02T15:04:05-07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// NVDDumpStats summarizes a streamed nvdcpe-2.0.tar.gz import.
type NVDDumpStats struct {
	ProductsProcessed int64
	EntriesEmitted    int64
	SkippedDeprecated int64
	SkippedHardware   int64
	LatestModified    *time.Time
	ContentSHA256     string
	TarSHA256         string
	GzSHA256          string
}

type nvdProductWrapper struct {
	CPE nvdCPEPayload `json:"cpe"`
}

type nvdCPEPayload struct {
	Deprecated   bool          `json:"deprecated"`
	CPEName      string        `json:"cpeName"`
	LastModified string        `json:"lastModified"`
	Titles       []nvdCPETitle `json:"titles"`
	Refs         []nvdCPERef   `json:"refs"`
}

type nvdCPETitle struct {
	Title string `json:"title"`
	Lang  string `json:"lang"`
}

type nvdCPERef struct {
	Ref  string `json:"ref"`
	Type string `json:"type"`
}

type nvdAggregate struct {
	entry           SourceEntry
	hasNonPlugin    bool
	hasEnglishTitle bool
}

func (a *nvdAggregate) absorb(c CPE23, p nvdCPEPayload, modified time.Time) {
	var refs []string
	var bestHomepage string
	var fallbackHomepage string
	for _, r := range p.Refs {
		if r.Ref == "" {
			continue
		}
		refs = append(refs, r.Ref)
		if key := CanonicalHomepageKey(r.Ref); key != "" {
			t := strings.ToLower(strings.TrimSpace(r.Type))
			if t == "product" || t == "project" {
				if bestHomepage == "" {
					bestHomepage = key
				}
			} else if t == "vendor" || t == "" {
				if fallbackHomepage == "" {
					fallbackHomepage = key
				}
			}
		}
	}
	if a.entry.HomepageKey == "" {
		if bestHomepage != "" {
			a.entry.HomepageKey = bestHomepage
		} else if fallbackHomepage != "" {
			a.entry.HomepageKey = fallbackHomepage
		}
	}

	var rawTitle string
	isEnglish := false
	for _, t := range p.Titles {
		lang := strings.ToLower(strings.TrimSpace(t.Lang))
		if lang == "en" || strings.HasPrefix(lang, "en-") || strings.HasPrefix(lang, "en_") {
			rawTitle = strings.TrimSpace(t.Title)
			isEnglish = true
			break
		}
		if rawTitle == "" && strings.TrimSpace(t.Title) != "" {
			rawTitle = strings.TrimSpace(t.Title)
		}
	}
	cleanedTitle := CleanCPETitle(rawTitle, c.Vendor, c.Product)
	if a.entry.Name == "" || (!a.hasEnglishTitle && isEnglish && cleanedTitle != "") {
		a.entry.Name = cleanedTitle
		a.hasEnglishTitle = isEnglish
	}
	if cleanedTitle != "" && cleanedTitle != a.entry.Name && len(a.entry.Aliases) < 12 && !slices.Contains(a.entry.Aliases, cleanedTitle) {
		a.entry.Aliases = append(a.entry.Aliases, cleanedTitle)
	}
	if c.Version != "" && CompareVersions(c.Version, a.entry.LatestVersion) > 0 {
		a.entry.LatestVersion = c.Version
	}
	for _, plat := range PlatformsFromCPE(c, rawTitle) {
		if !slices.Contains(a.entry.Platforms, plat) {
			a.entry.Platforms = append(a.entry.Platforms, plat)
		}
	}
	if c.Language != "" && len(c.Language) <= 8 && !slices.Contains(a.entry.Languages, c.Language) && len(a.entry.Languages) < 16 {
		a.entry.Languages = append(a.entry.Languages, c.Language)
	}
	if !IsPluginOrLibrary(c, rawTitle, refs) {
		a.hasNonPlugin = true
	}
	if !modified.IsZero() && (a.entry.LastModifiedAt == nil || modified.After(*a.entry.LastModifiedAt)) {
		a.entry.LastModifiedAt = &modified
	}
}

func newNVDAggregate(c CPE23) *nvdAggregate {
	vendor := HumanizeCPEToken(c.Vendor)
	return &nvdAggregate{
		entry: SourceEntry{
			Source:      "nvd",
			ExternalID:  c.CanonicalID(),
			Part:        c.Part,
			Vendor:      vendor,
			VendorNorm:  VendorNorm(c.Vendor),
			ProductNorm: ProductNorm(HumanizeCPEToken(c.Product), vendor),
			Platforms:   []string{},
			Tags:        []string{},
			Languages:   []string{},
			Aliases:     []string{},
		},
	}
}

func (a *nvdAggregate) finalize() SourceEntry {
	e := a.entry
	if e.Name == "" {
		e.Name = CleanCPETitle("", e.Vendor, e.ProductNorm)
	}
	if e.ProductNorm == "" {
		e.ProductNorm = ProductNorm(e.Name, e.Vendor)
	}
	e.IsPlugin = !a.hasNonPlugin
	slices.Sort(e.Platforms)
	slices.Sort(e.Languages)
	return sanitizeSourceEntry(e)
}

// StreamNVDDump streams nvdcpe-2.0.tar.gz without loading the archive into RAM,
// verifies SHA-256 (matching the concatenated archive file payloads reported by
// NIST nvdcpe-2.0.meta, as well as the uncompressed tar or compressed tar.gz
// checksum), aggregates active (part, vendor, product) entries, and emits
// bounded batches.
func StreamNVDDump(ctx context.Context, r io.Reader, expectedSHA256 string, batchSize int, emit func([]SourceEntry) error) (NVDDumpStats, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	gzHasher := sha256.New()
	rawTee := io.TeeReader(r, gzHasher)
	gzReader, err := gzip.NewReader(rawTee)
	if err != nil {
		return NVDDumpStats{}, fmt.Errorf("open nvd gzip archive: %w", err)
	}
	defer gzReader.Close()

	tarHasher := sha256.New()
	tarTee := io.TeeReader(gzReader, tarHasher)
	tr := tar.NewReader(tarTee)
	contentHasher := sha256.New()

	var stats NVDDumpStats
	agg := make(map[string]*nvdAggregate, min(batchSize+16, 4096))
	var lastKey string

	flush := func(keepLast bool) error {
		if len(agg) == 0 {
			return nil
		}
		batch := make([]SourceEntry, 0, len(agg))
		for key, item := range agg {
			if keepLast && key == lastKey {
				continue
			}
			batch = append(batch, item.finalize())
			delete(agg, key)
		}
		if len(batch) == 0 {
			return nil
		}
		if err := emit(batch); err != nil {
			return err
		}
		stats.EntriesEmitted += int64(len(batch))
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, fmt.Errorf("read nvd tar header: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		entryReader := io.TeeReader(tr, contentHasher)
		if strings.HasSuffix(strings.ToLower(hdr.Name), ".json") {
			if err := streamNVDJSONProducts(ctx, entryReader, func(p nvdCPEPayload) error {
				stats.ProductsProcessed++
				modified, validTime := parseNVDTime(p.LastModified)
				if validTime && (stats.LatestModified == nil || modified.After(*stats.LatestModified)) {
					stats.LatestModified = &modified
				}
				if p.Deprecated {
					stats.SkippedDeprecated++
					return nil
				}
				c, ok := ParseCPE23(p.CPEName)
				if !ok {
					return nil
				}
				if c.Part == "h" {
					stats.SkippedHardware++
					return nil
				}
				key := c.CanonicalID()
				if key == "" {
					return nil
				}
				item, exists := agg[key]
				if !exists {
					item = newNVDAggregate(c)
					agg[key] = item
				}
				item.absorb(c, p, modified)
				lastKey = key
				if len(agg) > batchSize {
					return flush(true)
				}
				return nil
			}); err != nil {
				return stats, err
			}
		}
		// Drain any trailing bytes (e.g. final newline after JSON object) so
		// contentHasher covers 100% of every regular file payload in the tar.
		if _, err := io.Copy(io.Discard, entryReader); err != nil {
			return stats, fmt.Errorf("drain nvd tar entry %s: %w", hdr.Name, err)
		}
	}

	// Drain any trailing padding so content, tar and gzip checksums cover full EOF.
	if _, err := io.Copy(io.Discard, tarTee); err != nil {
		return stats, fmt.Errorf("drain nvd gzip stream: %w", err)
	}
	if _, err := io.Copy(io.Discard, rawTee); err != nil {
		return stats, fmt.Errorf("drain nvd raw stream: %w", err)
	}
	stats.ContentSHA256 = hex.EncodeToString(contentHasher.Sum(nil))
	stats.TarSHA256 = hex.EncodeToString(tarHasher.Sum(nil))
	stats.GzSHA256 = hex.EncodeToString(gzHasher.Sum(nil))

	if expected := strings.ToLower(strings.TrimSpace(expectedSHA256)); expected != "" {
		if stats.ContentSHA256 != expected && stats.TarSHA256 != expected && stats.GzSHA256 != expected {
			return stats, fmt.Errorf("%w: expected %s", ErrNVDChecksumMismatch, expected)
		}
	}
	if err := flush(false); err != nil {
		return stats, err
	}
	if stats.EntriesEmitted == 0 {
		return stats, errors.New("NVD CPE archive contains no usable products")
	}
	return stats, nil
}

func streamNVDJSONProducts(ctx context.Context, r io.Reader, handle func(nvdCPEPayload) error) error {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return errors.New("expected nvd json object")
	}
	hasProducts := false
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		if key != "products" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
			continue
		}
		if hasProducts {
			return errors.New("duplicate NVD products array")
		}
		hasProducts = true
		arrTok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := arrTok.(json.Delim); !ok || delim != '[' {
			return errors.New("expected nvd products array")
		}
		for dec.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			var wrapper nvdProductWrapper
			if err := dec.Decode(&wrapper); err != nil {
				return err
			}
			if err := handle(wrapper.CPE); err != nil {
				return err
			}
		}
		// Consume closing ']'.
		if _, err := dec.Token(); err != nil {
			return err
		}
	}
	if _, err = dec.Token(); err != nil {
		return err
	}
	if !hasProducts {
		return errors.New("NVD CPE file is missing the products array")
	}
	return requireJSONEnd(dec)
}

func requireJSONEnd(dec *json.Decoder) error {
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected content after JSON document")
	}
	return nil
}

// NVDAPIResponse captures one page from the NVD CPE 2.0 REST API.
type NVDAPIResponse struct {
	ResultsPerPage      int
	StartIndex          int
	TotalResults        int
	ProductCount        int
	Entries             []SourceEntry
	SkippedDeprecated   int
	SkippedHardware     int
	LatestModified      *time.Time
	LatestDeprecation   *time.Time
	UndatedDeprecations bool
}

// ParseNVDAPIResponse decodes one paginated NVD CPE 2.0 REST API response and
// aggregates active (part, vendor, product) entries.
func ParseNVDAPIResponse(r io.Reader) (NVDAPIResponse, error) {
	var raw struct {
		ResultsPerPage *int                `json:"resultsPerPage"`
		StartIndex     *int                `json:"startIndex"`
		TotalResults   *int                `json:"totalResults"`
		Products       []nvdProductWrapper `json:"products"`
	}
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return NVDAPIResponse{}, err
	}
	if err := requireJSONEnd(dec); err != nil {
		return NVDAPIResponse{}, err
	}
	if raw.ResultsPerPage == nil || raw.StartIndex == nil || raw.TotalResults == nil || raw.Products == nil {
		return NVDAPIResponse{}, errors.New("NVD CPE page is missing pagination or product data")
	}
	out := NVDAPIResponse{
		ResultsPerPage: *raw.ResultsPerPage,
		StartIndex:     *raw.StartIndex,
		TotalResults:   *raw.TotalResults,
		ProductCount:   len(raw.Products),
	}
	if out.ResultsPerPage < 0 || out.StartIndex < 0 || out.TotalResults < out.StartIndex ||
		out.ProductCount != min(out.ResultsPerPage, out.TotalResults-out.StartIndex) ||
		(out.StartIndex < out.TotalResults && out.ProductCount == 0) {
		return NVDAPIResponse{}, errors.New("incomplete NVD CPE page")
	}
	agg := make(map[string]*nvdAggregate, len(raw.Products))
	for _, w := range raw.Products {
		c, ok := ParseCPE23(w.CPE.CPEName)
		if w.CPE.Deprecated {
			out.SkippedDeprecated++
			if ok && c.Part != "h" {
				if modified, ok := parseNVDTime(w.CPE.LastModified); !ok {
					out.UndatedDeprecations = true
				} else if out.LatestDeprecation == nil || modified.After(*out.LatestDeprecation) {
					out.LatestDeprecation = &modified
				}
			}
			continue
		}
		if !ok {
			continue
		}
		if c.Part == "h" {
			out.SkippedHardware++
			continue
		}
		key := c.CanonicalID()
		if key == "" {
			continue
		}
		item, exists := agg[key]
		if !exists {
			item = newNVDAggregate(c)
			agg[key] = item
		}
		modified, _ := parseNVDTime(w.CPE.LastModified)
		item.absorb(c, w.CPE, modified)
		if item.entry.LastModifiedAt != nil && (out.LatestModified == nil || item.entry.LastModifiedAt.After(*out.LatestModified)) {
			ts := *item.entry.LastModifiedAt
			out.LatestModified = &ts
		}
	}
	out.Entries = make([]SourceEntry, 0, len(agg))
	for _, item := range agg {
		out.Entries = append(out.Entries, item.finalize())
	}
	slices.SortFunc(out.Entries, func(a, b SourceEntry) int {
		return strings.Compare(a.ExternalID, b.ExternalID)
	})
	return out, nil
}

// WinGetStats summarizes a streamed microsoft/winget-pkgs tar.gz import.
type WinGetStats struct {
	ManifestsRead   int64
	PackagesEmitted int64
	SkippedFiles    int64
}

type wingetYAML struct {
	PackageIdentifier string   `yaml:"PackageIdentifier"`
	PackageVersion    string   `yaml:"PackageVersion"`
	PackageLocale     string   `yaml:"PackageLocale"`
	DefaultLocale     string   `yaml:"DefaultLocale"`
	ManifestType      string   `yaml:"ManifestType"`
	PackageName       string   `yaml:"PackageName"`
	Publisher         string   `yaml:"Publisher"`
	ShortDescription  string   `yaml:"ShortDescription"`
	Tags              []string `yaml:"Tags"`
	Monikers          []string `yaml:"Monikers"`
	Moniker           string   `yaml:"Moniker"`
	License           string   `yaml:"License"`
	PackageURL        string   `yaml:"PackageUrl"`
	// Security: Icons / IconUrl are intentionally omitted and never parsed or stored.
}

type wingetPkgState struct {
	pkgDir        string
	pkgID         string
	bestVersion   string
	defaultLocale string
	primary       *wingetYAML
	fallback      *wingetYAML
	aliases       []string
	languages     []string
}

func (p *wingetPkgState) resetVersion(version string) {
	p.bestVersion = version
	p.defaultLocale = ""
	p.primary = nil
	p.fallback = nil
	p.aliases = nil
	p.languages = nil
}

func (p *wingetPkgState) addManifest(m wingetYAML, pathLocale string) {
	if id := WinGetID(m.PackageIdentifier); id != "" {
		p.pkgID = id
	}
	loc := strings.TrimSpace(m.PackageLocale)
	if loc == "" {
		loc = pathLocale
	}
	if loc != "" && len(loc) <= 16 && !slices.Contains(p.languages, loc) && len(p.languages) < 16 {
		p.languages = append(p.languages, loc)
	}
	if name := strings.TrimSpace(m.PackageName); name != "" && !slices.Contains(p.aliases, name) && len(p.aliases) < 16 {
		p.aliases = append(p.aliases, name)
	}
	for _, mon := range m.Monikers {
		mon = strings.TrimSpace(mon)
		if mon != "" && !slices.Contains(p.aliases, mon) && len(p.aliases) < 16 {
			p.aliases = append(p.aliases, mon)
		}
	}
	if mon := strings.TrimSpace(m.Moniker); mon != "" && !slices.Contains(p.aliases, mon) && len(p.aliases) < 16 {
		p.aliases = append(p.aliases, mon)
	}

	mType := strings.ToLower(strings.TrimSpace(m.ManifestType))
	if mType == "version" {
		if dl := strings.TrimSpace(m.DefaultLocale); dl != "" {
			p.defaultLocale = dl
		}
		return
	}
	copyM := m
	switch {
	case mType == "singleton" || mType == "defaultlocale":
		p.primary = &copyM
	case p.defaultLocale != "" && strings.EqualFold(loc, p.defaultLocale):
		p.primary = &copyM
	case p.primary == nil && strings.EqualFold(loc, "en-us"):
		p.primary = &copyM
	case p.fallback == nil && strings.TrimSpace(m.PackageName) != "":
		p.fallback = &copyM
	}
}

func (p *wingetPkgState) finalize() (SourceEntry, bool) {
	chosen := p.primary
	if chosen == nil {
		chosen = p.fallback
	}
	if chosen == nil {
		return SourceEntry{}, false
	}
	id := WinGetID(p.pkgID)
	if id == "" {
		id = WinGetID(chosen.PackageIdentifier)
	}
	if id == "" {
		return SourceEntry{}, false
	}
	name := strings.TrimSpace(chosen.PackageName)
	if name == "" {
		if _, tail, ok := strings.Cut(id, "."); ok {
			name = strings.ReplaceAll(tail, ".", " ")
		}
	}
	if name == "" {
		return SourceEntry{}, false
	}
	vendor := strings.TrimSpace(chosen.Publisher)
	if vendor == "" {
		if head, _, ok := strings.Cut(id, "."); ok {
			vendor = head
		}
	}
	aliases := make([]string, 0, len(p.aliases)+2)
	for _, a := range p.aliases {
		if a != "" && !strings.EqualFold(a, name) && !slices.Contains(aliases, a) {
			aliases = append(aliases, a)
		}
	}
	tags := make([]string, 0, len(chosen.Tags))
	for _, t := range chosen.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && len(t) <= 64 && !slices.Contains(tags, t) && len(tags) < 16 {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	langs := make([]string, 0, len(p.languages))
	for _, l := range p.languages {
		base, _, _ := strings.Cut(strings.ToLower(l), "-")
		if base != "" && len(base) <= 3 && !slices.Contains(langs, base) {
			langs = append(langs, base)
		}
	}
	slices.Sort(langs)

	version := strings.TrimSpace(chosen.PackageVersion)
	if version == "" {
		version = p.bestVersion
	}
	desc := strings.TrimSpace(chosen.ShortDescription)
	if len(desc) > 1000 {
		desc = desc[:1000]
	}
	lic := strings.TrimSpace(chosen.License)
	if len(lic) > 128 {
		lic = lic[:128]
	}

	return sanitizeSourceEntry(SourceEntry{
		Source:        "winget",
		ExternalID:    id,
		Part:          "a",
		Name:          name,
		Vendor:        vendor,
		VendorNorm:    VendorNorm(vendor),
		ProductNorm:   ProductNorm(name, vendor),
		Description:   desc,
		License:       lic,
		LatestVersion: version,
		HomepageKey:   CanonicalHomepageKey(chosen.PackageURL),
		Platforms:     []string{"windows"},
		Tags:          tags,
		Languages:     langs,
		Aliases:       aliases,
		IsPlugin:      false,
	}), true
}

// StreamWinGetArchive streams the microsoft/winget-pkgs tar.gz archive in
// constant memory per package, keeping only the latest version's version/
// defaultLocale/locale/singleton manifests and skipping .installer.yaml files.
func StreamWinGetArchive(ctx context.Context, r io.Reader, batchSize int, emit func([]SourceEntry) error) (WinGetStats, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return WinGetStats{}, fmt.Errorf("open winget gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var stats WinGetStats
	var cur *wingetPkgState
	batchMap := make(map[string]SourceEntry, min(batchSize, 1024))

	flushBatch := func() error {
		if len(batchMap) == 0 {
			return nil
		}
		batch := make([]SourceEntry, 0, len(batchMap))
		for _, e := range batchMap {
			batch = append(batch, e)
		}
		clear(batchMap)
		if err := emit(batch); err != nil {
			return err
		}
		stats.PackagesEmitted += int64(len(batch))
		return nil
	}

	commitCurrent := func() error {
		if cur == nil {
			return nil
		}
		if entry, ok := cur.finalize(); ok {
			if existing, dup := batchMap[entry.ExternalID]; !dup || CompareVersions(entry.LatestVersion, existing.LatestVersion) >= 0 {
				batchMap[entry.ExternalID] = entry
			}
		}
		cur = nil
		if len(batchMap) >= batchSize {
			return flushBatch()
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, fmt.Errorf("read winget tar header: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		pkgDir, version, pkgID, pathLocale, ok := parseWinGetTarPath(hdr.Name)
		if !ok {
			stats.SkippedFiles++
			continue
		}
		if cur == nil || cur.pkgDir != pkgDir {
			if err := commitCurrent(); err != nil {
				return stats, err
			}
			cur = &wingetPkgState{
				pkgDir:      pkgDir,
				pkgID:       pkgID,
				bestVersion: version,
			}
		} else {
			cmp := CompareVersions(version, cur.bestVersion)
			if cmp < 0 {
				// Older version than already buffered: skip YAML decoding completely.
				stats.SkippedFiles++
				continue
			}
			if cmp > 0 {
				cur.resetVersion(version)
			}
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxWinGetManifestBytes))
		if err != nil {
			return stats, fmt.Errorf("read winget manifest %s: %w", hdr.Name, err)
		}
		var doc wingetYAML
		if err := yaml.Unmarshal(data, &doc); err != nil {
			stats.SkippedFiles++
			continue
		}
		stats.ManifestsRead++
		cur.addManifest(doc, pathLocale)
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return stats, fmt.Errorf("verify WinGet gzip archive: %w", err)
	}
	if err := commitCurrent(); err != nil {
		return stats, err
	}
	if err := flushBatch(); err != nil {
		return stats, err
	}
	if stats.PackagesEmitted == 0 {
		return stats, errors.New("WinGet archive contains no usable packages")
	}
	return stats, nil
}

// parseWinGetTarPath parses paths of the form:
//
//	winget-pkgs-master/manifests/<bucket>/<Publisher>/<Pkg...>/<Version>/<PackageIdentifier>[.locale.<lang>].yaml
//
// Installer manifests (*.installer.yaml) and non-manifest files return ok=false.
func parseWinGetTarPath(tarPath string) (pkgDir, version, pkgID, locale string, ok bool) {
	cleaned := path.Clean(strings.ReplaceAll(tarPath, "\\", "/"))
	idx := strings.Index(cleaned, "manifests/")
	if idx < 0 {
		return "", "", "", "", false
	}
	rel := cleaned[idx+len("manifests/"):]
	parts := strings.Split(rel, "/")
	// Minimum: <bucket>/<Publisher>/<Package>/<Version>/<file.yaml> => 5 segments.
	if len(parts) < 5 {
		return "", "", "", "", false
	}
	file := parts[len(parts)-1]
	lowFile := strings.ToLower(file)
	if (!strings.HasSuffix(lowFile, ".yaml") && !strings.HasSuffix(lowFile, ".yml")) ||
		strings.HasSuffix(lowFile, ".installer.yaml") ||
		strings.HasSuffix(lowFile, ".installer.yml") {
		return "", "", "", "", false
	}
	stem := file[:strings.LastIndex(file, ".")]
	if locIdx := strings.Index(strings.ToLower(stem), ".locale."); locIdx > 0 {
		pkgID = stem[:locIdx]
		locale = stem[locIdx+len(".locale."):]
	} else {
		pkgID = stem
	}
	if WinGetID(pkgID) == "" {
		return "", "", "", "", false
	}
	version = parts[len(parts)-2]
	pkgDir = strings.Join(parts[:len(parts)-2], "/")
	return pkgDir, version, pkgID, locale, true
}

// HomebrewStats summarizes a streamed Homebrew Cask JSON import.
type HomebrewStats struct {
	CasksRead       int64
	EntriesEmitted  int64
	SkippedDisabled int64
}

type homebrewCaskPayload struct {
	Token              string          `json:"token"`
	OldTokens          []string        `json:"old_tokens"`
	Name               []string        `json:"name"`
	Desc               string          `json:"desc"`
	Homepage           string          `json:"homepage"`
	Version            string          `json:"version"`
	Languages          []string        `json:"languages"`
	Disabled           bool            `json:"disabled"`
	SupportedPlatforms json.RawMessage `json:"supported_platforms"`
}

// StreamHomebrewCasks streams the JSON array returned by
// https://formulae.brew.sh/api/cask.json, ignoring disabled=true casks.
func StreamHomebrewCasks(ctx context.Context, r io.Reader, batchSize int, emit func([]SourceEntry) error) (HomebrewStats, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if err != nil {
		return HomebrewStats{}, fmt.Errorf("read homebrew cask json: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return HomebrewStats{}, errors.New("expected homebrew cask json array")
	}

	var stats HomebrewStats
	batch := make([]SourceEntry, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := emit(batch); err != nil {
			return err
		}
		stats.EntriesEmitted += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for dec.More() {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		var cask homebrewCaskPayload
		if err := dec.Decode(&cask); err != nil {
			return stats, fmt.Errorf("decode homebrew cask entry: %w", err)
		}
		stats.CasksRead++
		if cask.Disabled {
			stats.SkippedDisabled++
			continue
		}
		token := HomebrewCask(cask.Token)
		if token == "" {
			continue
		}
		var primaryName string
		var aliases []string
		for _, n := range cask.Name {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if primaryName == "" {
				primaryName = n
			} else if !strings.EqualFold(n, primaryName) && !slices.Contains(aliases, n) && len(aliases) < 16 {
				aliases = append(aliases, n)
			}
		}
		if primaryName == "" {
			primaryName = HumanizeCPEToken(strings.ReplaceAll(token, "-", "_"))
		}
		if !strings.EqualFold(token, primaryName) && !slices.Contains(aliases, token) && len(aliases) < 16 {
			aliases = append(aliases, token)
		}
		for _, old := range cask.OldTokens {
			if tok := HomebrewCask(old); tok != "" && !slices.Contains(aliases, tok) && len(aliases) < 16 {
				aliases = append(aliases, tok)
			}
		}
		platforms := parseHomebrewPlatforms(cask.SupportedPlatforms)
		langs := make([]string, 0, len(cask.Languages))
		for _, l := range cask.Languages {
			base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(l)), "-")
			if base != "" && len(base) <= 3 && !slices.Contains(langs, base) && len(langs) < 16 {
				langs = append(langs, base)
			}
		}
		slices.Sort(langs)

		desc := strings.TrimSpace(cask.Desc)
		if len(desc) > 1000 {
			desc = desc[:1000]
		}
		version := strings.TrimSpace(cask.Version)
		if version == "latest" {
			version = ""
		}
		if comma := strings.IndexByte(version, ','); comma > 0 {
			version = strings.TrimSpace(version[:comma])
		}

		batch = append(batch, sanitizeSourceEntry(SourceEntry{
			Source:        "homebrew",
			ExternalID:    token,
			Part:          "a",
			Name:          primaryName,
			ProductNorm:   Normalize(primaryName),
			Description:   desc,
			LatestVersion: version,
			HomepageKey:   CanonicalHomepageKey(cask.Homepage),
			Platforms:     platforms,
			Tags:          []string{},
			Languages:     langs,
			Aliases:       aliases,
			IsPlugin:      false,
		}))
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return stats, err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return stats, err
	}
	if err := requireJSONEnd(dec); err != nil {
		return stats, err
	}
	if stats.CasksRead == 0 {
		return stats, errors.New("Homebrew catalog contains no casks")
	}
	if err := flush(); err != nil {
		return stats, err
	}
	return stats, nil
}

func parseHomebrewPlatforms(raw json.RawMessage) []string {
	out := []string{"macos"}
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, item := range list {
			if p := NormalizePlatform(item); p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		slices.Sort(out)
		return out
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		for key := range obj {
			if p := NormalizePlatform(key); p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		slices.Sort(out)
		return out
	}
	return out
}

type selfhstIconPayload struct {
	Name      string `json:"Name"`
	Reference string `json:"Reference"`
	PNG       string `json:"PNG"`
}

// ParseSelfhstIcons parses selfh.st/icons index.json and keeps only entries
// with PNG == "Yes" and a strictly validated slug reference.
func ParseSelfhstIcons(r io.Reader) (entries []IconEntry, skipped int64, err error) {
	dec := json.NewDecoder(io.LimitReader(r, 16*1024*1024))
	tok, err := dec.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("read selfh.st icons index: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, 0, errors.New("expected selfh.st icons json array")
	}
	seen := make(map[string]struct{}, 4096)
	for dec.More() {
		var item selfhstIconPayload
		if err := dec.Decode(&item); err != nil {
			return nil, skipped, fmt.Errorf("decode selfh.st icon entry: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(item.PNG), "yes") {
			skipped++
			continue
		}
		ref := ValidIconRef(item.Reference)
		if ref == "" {
			skipped++
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		name := strings.TrimSpace(sanitizeUTF8(item.Name))
		if name == "" {
			name = HumanizeCPEToken(strings.ReplaceAll(ref, "-", "_"))
		}
		entries = append(entries, IconEntry{
			Reference:  ref,
			Name:       name,
			Normalized: Normalize(name),
		})
	}
	if _, err := dec.Token(); err != nil {
		return nil, skipped, err
	}
	if err := requireJSONEnd(dec); err != nil {
		return nil, skipped, err
	}
	return entries, skipped, nil
}
