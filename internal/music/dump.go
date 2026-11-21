package music

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// SupportedSchemaSequences enumerates MusicBrainz PostgreSQL schema versions
// whose core COPY TSV column layout is verified. A newer unknown schema fails
// closed rather than silently misreading shifted columns.
var SupportedSchemaSequences = []int{29, 30, 31}

// ErrUnsupportedSchema reports a MusicBrainz schema version this build does
// not yet know how to decode safely.
var ErrUnsupportedSchema = errors.New("unsupported MusicBrainz schema sequence")

// SupportedSchema reports whether the given SCHEMA_SEQUENCE is known safe.
func SupportedSchema(sequence int) bool {
	return slices.Contains(SupportedSchemaSequences, sequence)
}

const maxDumpLineBytes = 1 << 20 // 1 MiB per TSV line safety cap

// DumpHeader holds archive-level replication metadata extracted from
// TIMESTAMP, REPLICATION_SEQUENCE and SCHEMA_SEQUENCE.
type DumpHeader struct {
	Timestamp           time.Time
	ReplicationSequence int64
	SchemaSequence      int
}

// Core & Cover Art Archive row types decoded from mbdump.tar.bz2 and
// mbdump-cover-art-archive.tar.bz2.
type LookupRow struct {
	Kind string
	ID   int
	Name string
}

type LanguageRow struct {
	ID   int
	Code string
}

type ArtistRow struct {
	ID       int
	GID      string
	Name     string
	SortName string
}

type AliasRow struct {
	Entity   string // artist, release_group, release, label
	ID       int
	EntityID int
	Name     string
	Locale   string
}

type ArtistCreditRow struct {
	ID   int
	Name string
}

type ArtistCreditNameRow struct {
	ArtistCredit int
	Position     int
	ArtistID     int
	Name         string
	JoinPhrase   string
}

type ReleaseGroupRow struct {
	ID           int
	GID          string
	Name         string
	ArtistCredit int
	PrimaryType  int
}

type ReleaseGroupSecondaryTypeRow struct {
	ReleaseGroupID  int
	SecondaryTypeID int
}

type ReleaseRow struct {
	ID             int
	GID            string
	Name           string
	ArtistCredit   int
	ReleaseGroupID int
	Status         int
	LanguageID     int
	Barcode        string
}

type ReleaseDateRow struct {
	ReleaseID int
	Year      int
}

type LabelRow struct {
	ID   int
	GID  string
	Name string
}

type ReleaseLabelRow struct {
	ID            int
	ReleaseID     int
	LabelID       int
	CatalogNumber string
}

type MediumRow struct {
	ID         int
	ReleaseID  int
	Position   int
	FormatID   int
	TrackCount int
}

type URLRow struct {
	ID          int
	DiscogsKind string
	DiscogsID   string
	WikidataID  string
}

type EntityURLRow struct {
	Entity   string // release_group, release, artist
	LinkID   int
	EntityID int
	URLID    int
}

type RedirectRow struct {
	Entity    string // artist, release_group, release, label
	SourceGID string
	TargetID  int
}

type CoverArtRow struct {
	ID        int64
	ReleaseID int
	Ordering  int
}

type CoverArtTypeRow struct {
	CoverArtID int64
	TypeID     int
}

type ReleaseGroupCoverArtRow struct {
	ReleaseGroupID int
	ReleaseID      int
}

// DumpTableBatch groups decoded rows from a single table inside a dump archive.
type DumpTableBatch struct {
	Table string

	Lookups        []LookupRow
	Languages      []LanguageRow
	Artists        []ArtistRow
	Aliases        []AliasRow
	ArtistCredits  []ArtistCreditRow
	CreditNames    []ArtistCreditNameRow
	ReleaseGroups  []ReleaseGroupRow
	SecondaryTypes []ReleaseGroupSecondaryTypeRow
	Releases       []ReleaseRow
	ReleaseDates   []ReleaseDateRow
	Labels         []LabelRow
	ReleaseLabels  []ReleaseLabelRow
	Mediums        []MediumRow
	URLs           []URLRow
	EntityURLs     []EntityURLRow
	Redirects      []RedirectRow
	CoverArts      []CoverArtRow
	CoverArtTypes  []CoverArtTypeRow
	RGCoverArts    []ReleaseGroupCoverArtRow

	Read    int64
	Skipped int64
}

func (b *DumpTableBatch) Len() int {
	return len(b.Lookups) + len(b.Languages) + len(b.Artists) + len(b.Aliases) +
		len(b.ArtistCredits) + len(b.CreditNames) + len(b.ReleaseGroups) +
		len(b.SecondaryTypes) + len(b.Releases) + len(b.ReleaseDates) +
		len(b.Labels) + len(b.ReleaseLabels) + len(b.Mediums) + len(b.URLs) +
		len(b.EntityURLs) + len(b.Redirects) + len(b.CoverArts) +
		len(b.CoverArtTypes) + len(b.RGCoverArts)
}

func (b *DumpTableBatch) Reset() {
	b.Lookups = b.Lookups[:0]
	b.Languages = b.Languages[:0]
	b.Artists = b.Artists[:0]
	b.Aliases = b.Aliases[:0]
	b.ArtistCredits = b.ArtistCredits[:0]
	b.CreditNames = b.CreditNames[:0]
	b.ReleaseGroups = b.ReleaseGroups[:0]
	b.SecondaryTypes = b.SecondaryTypes[:0]
	b.Releases = b.Releases[:0]
	b.ReleaseDates = b.ReleaseDates[:0]
	b.Labels = b.Labels[:0]
	b.ReleaseLabels = b.ReleaseLabels[:0]
	b.Mediums = b.Mediums[:0]
	b.URLs = b.URLs[:0]
	b.EntityURLs = b.EntityURLs[:0]
	b.Redirects = b.Redirects[:0]
	b.CoverArts = b.CoverArts[:0]
	b.CoverArtTypes = b.CoverArtTypes[:0]
	b.RGCoverArts = b.RGCoverArts[:0]
	b.Read = 0
	b.Skipped = 0
}

// WatchedCoreTables maps mbdump/<name> entries to decode from mbdump.tar.bz2.
// Heavy tables such as track (~59M rows), recording (~41M rows), work, event,
// place, area and non-URL relationships are skipped without decoding.
var WatchedCoreTables = map[string]bool{
	"release_group_primary_type":        true,
	"release_group_secondary_type":      true,
	"release_status":                    true,
	"medium_format":                     true,
	"language":                          true,
	"artist":                            true,
	"artist_alias":                      true,
	"artist_credit":                     true,
	"artist_credit_name":                true,
	"artist_gid_redirect":               true,
	"release_group":                     true,
	"release_group_alias":               true,
	"release_group_secondary_type_join": true,
	"release_group_gid_redirect":        true,
	"release":                           true,
	"release_alias":                     true,
	"release_country":                   true,
	"release_unknown_country":           true,
	"release_gid_redirect":              true,
	"label":                             true,
	"label_alias":                       true,
	"label_gid_redirect":                true,
	"release_label":                     true,
	"medium":                            true,
	"url":                               true,
	"l_release_group_url":               true,
	"l_release_url":                     true,
	"l_artist_url":                      true,
}

// WatchedCoverTables lists the tables decoded from mbdump-cover-art-archive.tar.bz2.
var WatchedCoverTables = map[string]bool{
	"cover_art":               true,
	"cover_art_type":          true,
	"release_group_cover_art": true,
}

// StreamDumpArchive streams a bzip2-compressed MusicBrainz tar archive
// (mbdump.tar.bz2 or mbdump-cover-art-archive.tar.bz2) in a single pass.
// Header metadata is validated before any watched table is emitted.
func StreamDumpArchive(ctx context.Context, r io.Reader, watched map[string]bool, batchSize int, onBatch func(DumpTableBatch) error) (DumpHeader, error) {
	if batchSize < 1 {
		batchSize = 2000
	}
	var header DumpHeader
	tr := tar.NewReader(bzip2.NewReader(r))
	schemaChecked := false
	for {
		if err := ctx.Err(); err != nil {
			return header, err
		}
		entry, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return header, fmt.Errorf("read MusicBrainz archive entry: %w", err)
		}
		if entry.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(path.Clean(entry.Name), "./")
		switch name {
		case "TIMESTAMP":
			data, err := io.ReadAll(io.LimitReader(tr, 256))
			if err != nil {
				return header, err
			}
			if ts, ok := parseMusicBrainzTimestamp(string(data)); ok {
				header.Timestamp = ts
			}
			continue
		case "REPLICATION_SEQUENCE":
			data, err := io.ReadAll(io.LimitReader(tr, 64))
			if err != nil {
				return header, err
			}
			n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil || n < 0 {
				return header, errors.New("invalid MusicBrainz REPLICATION_SEQUENCE")
			}
			header.ReplicationSequence = n
			continue
		case "SCHEMA_SEQUENCE":
			data, err := io.ReadAll(io.LimitReader(tr, 64))
			if err != nil {
				return header, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || n <= 0 {
				return header, errors.New("invalid MusicBrainz SCHEMA_SEQUENCE")
			}
			if !SupportedSchema(n) {
				return header, fmt.Errorf("%w: %d", ErrUnsupportedSchema, n)
			}
			header.SchemaSequence = n
			schemaChecked = true
			continue
		}
		table, ok := dumpTableName(name)
		if !ok || !watched[table] {
			continue
		}
		if schemaChecked && !SupportedSchema(header.SchemaSequence) {
			return header, fmt.Errorf("%w: %d", ErrUnsupportedSchema, header.SchemaSequence)
		}
		if err := streamTableTSV(ctx, table, tr, batchSize, onBatch); err != nil {
			return header, err
		}
	}
	if header.SchemaSequence > 0 && !SupportedSchema(header.SchemaSequence) {
		return header, fmt.Errorf("%w: %d", ErrUnsupportedSchema, header.SchemaSequence)
	}
	return header, nil
}

func dumpTableName(entryPath string) (string, bool) {
	if !strings.HasPrefix(entryPath, "mbdump/") {
		return "", false
	}
	table := strings.TrimPrefix(entryPath, "mbdump/")
	if strings.Contains(table, "/") || table == "" {
		return "", false
	}
	if dot := strings.LastIndexByte(table, '.'); dot >= 0 {
		table = table[dot+1:]
	}
	return table, table != ""
}

func streamTableTSV(ctx context.Context, table string, r io.Reader, batchSize int, onBatch func(DumpTableBatch) error) error {
	reader := bufio.NewReaderSize(r, 64<<10)
	batch := DumpTableBatch{Table: table}
	flush := func() error {
		if batch.Len() == 0 && batch.Read == 0 && batch.Skipped == 0 {
			return nil
		}
		if err := onBatch(batch); err != nil {
			return err
		}
		batch.Reset()
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, oversized, eof, err := readBoundedLine(reader, maxDumpLineBytes)
		if err != nil {
			return err
		}
		if eof && len(line) == 0 && !oversized {
			break
		}
		batch.Read++
		if oversized {
			batch.Skipped++
		} else if len(line) > 0 {
			if ok := decodeTSVRow(table, line, &batch); !ok {
				batch.Skipped++
			}
		}
		if batch.Len() >= batchSize || batch.Read >= int64(batchSize*4) {
			if err := flush(); err != nil {
				return err
			}
		}
		if eof {
			break
		}
	}
	return flush()
}

func readBoundedLine(r *bufio.Reader, limit int) (line string, oversized bool, eof bool, err error) {
	var buf strings.Builder
	for {
		frag, more, readErr := r.ReadLine()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				eof = true
				break
			}
			return "", false, false, readErr
		}
		if !oversized {
			if buf.Len()+len(frag) > limit {
				oversized = true
				buf.Reset()
			} else {
				buf.Write(frag)
			}
		}
		if !more {
			break
		}
	}
	return buf.String(), oversized, eof, nil
}

// UnescapeCopyField decodes a PostgreSQL text COPY field, returning ("", true)
// for SQL NULL (\N).
func UnescapeCopyField(raw string) (string, bool) {
	if raw == `\N` {
		return "", true
	}
	if !strings.ContainsRune(raw, '\\') {
		return raw, false
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			b.WriteByte(raw[i])
			continue
		}
		i++
		switch raw[i] {
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(raw[i])
		}
	}
	return b.String(), false
}

func copyField(cols []string, idx int) (string, bool) {
	if idx < 0 || idx >= len(cols) {
		return "", true
	}
	return UnescapeCopyField(cols[idx])
}

func copyInt(cols []string, idx int) (int, bool) {
	s, isNull := copyField(cols, idx)
	if isNull || s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func copyInt64(cols []string, idx int) (int64, bool) {
	s, isNull := copyField(cols, idx)
	if isNull || s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func decodeTSVRow(table, line string, batch *DumpTableBatch) bool {
	cols := strings.Split(line, "\t")
	switch table {
	case "release_group_primary_type", "release_group_secondary_type", "release_status", "medium_format":
		id, ok := copyInt(cols, 0)
		name, isNull := copyField(cols, 1)
		if !ok || isNull || name == "" {
			return false
		}
		batch.Lookups = append(batch.Lookups, LookupRow{Kind: table, ID: id, Name: strings.TrimSpace(name)})
		return true

	case "language":
		id, ok := copyInt(cols, 0)
		if !ok {
			return false
		}
		iso1, _ := copyField(cols, 3)
		iso3, _ := copyField(cols, 6)
		if iso3 == "" {
			iso3, _ = copyField(cols, 1)
		}
		if code := NormalizeLanguage(iso1, iso3); code != "" {
			batch.Languages = append(batch.Languages, LanguageRow{ID: id, Code: code})
		}
		return true

	case "artist":
		id, ok := copyInt(cols, 0)
		gidRaw, _ := copyField(cols, 1)
		gid := MBID(gidRaw)
		name, isNull := copyField(cols, 2)
		sortName, _ := copyField(cols, 3)
		if !ok || gid == "" || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		batch.Artists = append(batch.Artists, ArtistRow{ID: id, GID: gid, Name: trimCap(name, 500), SortName: trimCap(sortName, 500)})
		return true

	case "artist_alias", "release_group_alias", "release_alias", "label_alias":
		id, ok := copyInt(cols, 0)
		entityID, ok2 := copyInt(cols, 1)
		name, isNull := copyField(cols, 2)
		locale, _ := copyField(cols, 3)
		if !ok || !ok2 || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		entity := strings.TrimSuffix(table, "_alias")
		batch.Aliases = append(batch.Aliases, AliasRow{Entity: entity, ID: id, EntityID: entityID, Name: trimCap(name, 500), Locale: trimCap(locale, 32)})
		return true

	case "artist_credit":
		id, ok := copyInt(cols, 0)
		name, isNull := copyField(cols, 1)
		if !ok || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		batch.ArtistCredits = append(batch.ArtistCredits, ArtistCreditRow{ID: id, Name: trimCap(name, 500)})
		return true

	case "artist_credit_name":
		ac, ok := copyInt(cols, 0)
		pos, ok2 := copyInt(cols, 1)
		artistID, ok3 := copyInt(cols, 2)
		name, _ := copyField(cols, 3)
		joinPhrase, _ := copyField(cols, 4)
		if !ok || !ok2 || !ok3 {
			return false
		}
		batch.CreditNames = append(batch.CreditNames, ArtistCreditNameRow{ArtistCredit: ac, Position: pos, ArtistID: artistID, Name: trimCap(name, 500), JoinPhrase: trimCap(joinPhrase, 64)})
		return true

	case "release_group":
		id, ok := copyInt(cols, 0)
		gidRaw, _ := copyField(cols, 1)
		gid := MBID(gidRaw)
		name, isNull := copyField(cols, 2)
		ac, ok2 := copyInt(cols, 3)
		pt, _ := copyInt(cols, 4)
		if !ok || !ok2 || gid == "" || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		batch.ReleaseGroups = append(batch.ReleaseGroups, ReleaseGroupRow{ID: id, GID: gid, Name: trimCap(name, 500), ArtistCredit: ac, PrimaryType: pt})
		return true

	case "release_group_secondary_type_join":
		rgID, ok := copyInt(cols, 0)
		secID, ok2 := copyInt(cols, 1)
		if !ok || !ok2 {
			return false
		}
		batch.SecondaryTypes = append(batch.SecondaryTypes, ReleaseGroupSecondaryTypeRow{ReleaseGroupID: rgID, SecondaryTypeID: secID})
		return true

	case "release":
		id, ok := copyInt(cols, 0)
		gidRaw, _ := copyField(cols, 1)
		gid := MBID(gidRaw)
		name, isNull := copyField(cols, 2)
		ac, ok2 := copyInt(cols, 3)
		rgID, ok3 := copyInt(cols, 4)
		status, _ := copyInt(cols, 5)
		langID, _ := copyInt(cols, 7)
		barcodeRaw, _ := copyField(cols, 9)
		if !ok || !ok2 || !ok3 || gid == "" || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		batch.Releases = append(batch.Releases, ReleaseRow{
			ID:             id,
			GID:            gid,
			Name:           trimCap(name, 500),
			ArtistCredit:   ac,
			ReleaseGroupID: rgID,
			Status:         status,
			LanguageID:     langID,
			Barcode:        Barcode(barcodeRaw),
		})
		return true

	case "release_country":
		relID, ok := copyInt(cols, 0)
		yearRaw, _ := copyField(cols, 2)
		if !ok {
			return false
		}
		if y := parseYear(yearRaw); y > 0 {
			batch.ReleaseDates = append(batch.ReleaseDates, ReleaseDateRow{ReleaseID: relID, Year: y})
		}
		return true

	case "release_unknown_country":
		relID, ok := copyInt(cols, 0)
		yearRaw, _ := copyField(cols, 1)
		if !ok {
			return false
		}
		if y := parseYear(yearRaw); y > 0 {
			batch.ReleaseDates = append(batch.ReleaseDates, ReleaseDateRow{ReleaseID: relID, Year: y})
		}
		return true

	case "label":
		id, ok := copyInt(cols, 0)
		gidRaw, _ := copyField(cols, 1)
		gid := MBID(gidRaw)
		name, isNull := copyField(cols, 2)
		if !ok || gid == "" || isNull || strings.TrimSpace(name) == "" {
			return false
		}
		batch.Labels = append(batch.Labels, LabelRow{ID: id, GID: gid, Name: trimCap(name, 300)})
		return true

	case "release_label":
		id, ok := copyInt(cols, 0)
		relID, ok2 := copyInt(cols, 1)
		labelID, _ := copyInt(cols, 2)
		catNo, _ := copyField(cols, 3)
		if !ok || !ok2 {
			return false
		}
		if labelID <= 0 && strings.TrimSpace(catNo) == "" {
			return true
		}
		batch.ReleaseLabels = append(batch.ReleaseLabels, ReleaseLabelRow{ID: id, ReleaseID: relID, LabelID: labelID, CatalogNumber: trimCap(catNo, 128)})
		return true

	case "medium":
		id, ok := copyInt(cols, 0)
		relID, ok2 := copyInt(cols, 1)
		pos, _ := copyInt(cols, 2)
		formatID, _ := copyInt(cols, 3)
		trackCount, _ := copyInt(cols, 7)
		if !ok || !ok2 {
			return false
		}
		batch.Mediums = append(batch.Mediums, MediumRow{ID: id, ReleaseID: relID, Position: pos, FormatID: formatID, TrackCount: max(0, trackCount)})
		return true

	case "url":
		id, ok := copyInt(cols, 0)
		rawURL, isNull := copyField(cols, 2)
		if !ok || isNull {
			return false
		}
		dKind, dID := DiscogsURL(rawURL)
		wd := WikidataURL(rawURL)
		if dID == "" && wd == "" {
			return true // Discard unrelated URLs immediately.
		}
		batch.URLs = append(batch.URLs, URLRow{ID: id, DiscogsKind: dKind, DiscogsID: dID, WikidataID: wd})
		return true

	case "l_release_group_url", "l_release_url", "l_artist_url":
		linkID, ok := copyInt(cols, 0)
		entityID, ok2 := copyInt(cols, 2)
		urlID, ok3 := copyInt(cols, 3)
		if !ok || !ok2 || !ok3 {
			return false
		}
		entity := strings.TrimSuffix(strings.TrimPrefix(table, "l_"), "_url")
		batch.EntityURLs = append(batch.EntityURLs, EntityURLRow{Entity: entity, LinkID: linkID, EntityID: entityID, URLID: urlID})
		return true

	case "artist_gid_redirect", "release_group_gid_redirect", "release_gid_redirect", "label_gid_redirect":
		gidRaw, _ := copyField(cols, 0)
		gid := MBID(gidRaw)
		targetID, ok := copyInt(cols, 1)
		if gid == "" || !ok {
			return false
		}
		entity := strings.TrimSuffix(table, "_gid_redirect")
		batch.Redirects = append(batch.Redirects, RedirectRow{Entity: entity, SourceGID: gid, TargetID: targetID})
		return true

	case "cover_art":
		id, ok := copyInt64(cols, 0)
		relID, ok2 := copyInt(cols, 1)
		ordering, _ := copyInt(cols, 3)
		if !ok || !ok2 || id <= 0 {
			return false
		}
		batch.CoverArts = append(batch.CoverArts, CoverArtRow{ID: id, ReleaseID: relID, Ordering: ordering})
		return true

	case "cover_art_type":
		id, ok := copyInt64(cols, 0)
		typeID, ok2 := copyInt(cols, 1)
		if !ok || !ok2 || id <= 0 {
			return false
		}
		if typeID == 1 { // Type 1 = Front cover art.
			batch.CoverArtTypes = append(batch.CoverArtTypes, CoverArtTypeRow{CoverArtID: id, TypeID: typeID})
		}
		return true

	case "release_group_cover_art":
		rgID, ok := copyInt(cols, 0)
		relID, ok2 := copyInt(cols, 1)
		if !ok || !ok2 {
			return false
		}
		batch.RGCoverArts = append(batch.RGCoverArts, ReleaseGroupCoverArtRow{ReleaseGroupID: rgID, ReleaseID: relID})
		return true
	}
	return false
}

func trimCap(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func parseMusicBrainzTimestamp(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05.999999+00",
		"2006-01-02 15:04:05+00",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05",
		"Mon Jan _2 15:04:05 MST 2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
