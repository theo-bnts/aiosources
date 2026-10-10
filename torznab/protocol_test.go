package torznab

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseCapabilitiesModesAndCategories(t *testing.T) {
	caps, err := parseCapabilities([]byte(`<caps>
		<server title="Example &amp; Friends"/>
		<limits default="25" max="100"/>
		<searching>
			<search available="yes" supportedParams="q,cat, q,,vendorid"/>
			<tv-search available="yes" supportedParams="tvdbid,season,ep"/>
			<movie-search available="no" supportedParams="imdbid"/>
			<audio-search available="yes" supportedParams="artist,album"/>
			<book-search available="yes" supportedParams="author,title"/>
		</searching>
		<categories><category id="5000" name="TV"><subcat id="5040" name="HD"/><subcat id="5070" name="Anime"/></category></categories>
	</caps>`))
	if err != nil {
		t.Fatal(err)
	}
	want := Capabilities{
		Title: "Example & Friends", Limits: Limits{Default: 25, Max: 100},
		Searches: map[SearchMode]SearchCapability{
			ModeSearch: {Available: true, SupportedParams: []string{"q", "cat", "vendorid"}},
			ModeTV:     {Available: true, SupportedParams: []string{"tvdbid", "season", "ep"}},
			ModeMovie:  {Available: false, SupportedParams: []string{"imdbid"}},
			ModeMusic:  {Available: true, SupportedParams: []string{"artist", "album"}},
			ModeBook:   {Available: true, SupportedParams: []string{"author", "title"}},
		},
		Categories: []Category{{ID: 5000, Name: "TV", Subcategories: []Category{{ID: 5040, Name: "HD"}, {ID: 5070, Name: "Anime"}}}},
	}
	if !reflect.DeepEqual(caps, want) {
		t.Fatalf("capabilities = %#v, want %#v", caps, want)
	}
	missing, err := parseCapabilities([]byte(`<caps><server title="Minimal"/></caps>`))
	if err != nil {
		t.Fatal(err)
	}
	if missing.Limits != (Limits{}) || len(missing.Searches) != 0 {
		t.Fatalf("invented capabilities: %#v", missing)
	}
}

func TestParsePageNamespaceAndMultiValueNormalization(t *testing.T) {
	for _, namespace := range []struct{ prefix, uri string }{
		{"torznab", "http://torznab.com/schemas/2015/feed"},
		{"newznab", "http://www.newznab.com/DTD/2010/feeds/attributes/"},
		{"vendor", "http://torznab.com/schemas/2015/feed"},
	} {
		t.Run(namespace.prefix, func(t *testing.T) {
			body := `<rss xmlns:EXT="URI"><channel><EXT:response offset="4" total="9"/>
			<item><guid isPermaLink="false">  urn:provider:release:alpha/suffix  </guid>
			<title> A &amp; B </title><link>https://example.invalid/get?id=alpha</link><comments>https://example.invalid/discuss/alpha</comments>
			<pubDate>Tue, 16 Jul 2019 20:56:54 +0000</pubDate>
			<category>TV &gt; HD</category><category>3D Movies</category><category>5000</category>
			<EXT:attr name="category" value="5000"/><EXT:attr name="category" value="5040"/><EXT:attr name="category" value="5040"/>
			<EXT:attr name="tag" value="internal"/><EXT:attr name="tag" value="trusted"/><EXT:attr name="tag" value="internal"/>
			<EXT:attr name="imdbid" value="tt0012345"/><EXT:attr name="vendor-score" value="excellent"/>
			<EXT:attr name="size" value="4294967296"/><EXT:attr name="seeders" value="0"/><EXT:attr name="peers" value=""/>
			</item><item><guid>another-nonnumeric-guid</guid><title>Missing counts</title></item>
			</channel></rss>`
			body = strings.NewReplacer("EXT", namespace.prefix, "URI", namespace.uri).Replace(body)
			page, err := parsePage([]byte(body), 40, "")
			if err != nil {
				t.Fatal(err)
			}
			if page.Offset != 4 || page.Total == nil || *page.Total != 9 || len(page.Items) != 2 {
				t.Fatalf("page = %#v", page)
			}
			item := page.Items[0]
			if item.GUID != "urn:provider:release:alpha/suffix" || item.Title != "A & B" || item.Link != "https://example.invalid/get?id=alpha" || item.Comments != "https://example.invalid/discuss/alpha" {
				t.Fatalf("identity = %#v", item)
			}
			if item.Size == nil || *item.Size != 4294967296 || item.Seeders == nil || *item.Seeders != 0 || item.Peers != nil {
				t.Fatalf("counts = %#v", item)
			}
			if !reflect.DeepEqual(item.Categories, []int{5000, 5040}) {
				t.Fatalf("categories = %v", item.Categories)
			}
			wantAttributes := map[string][]string{
				"category": {"5000", "5040"}, "tag": {"internal", "trusted"},
				"imdbid": {"tt0012345"}, "vendor-score": {"excellent"}, "size": {"4294967296"}, "seeders": {"0"},
			}
			if !reflect.DeepEqual(item.Attributes, wantAttributes) {
				t.Fatalf("attributes = %#v", item.Attributes)
			}
			if item.PublishedAt == nil || !item.PublishedAt.Equal(time.Date(2019, 7, 16, 20, 56, 54, 0, time.UTC)) {
				t.Fatalf("date = %v", item.PublishedAt)
			}
			missing := page.Items[1]
			if missing.GUID != "another-nonnumeric-guid" || missing.Size != nil || missing.Seeders != nil || missing.Peers != nil || missing.InfoHash != "" {
				t.Fatalf("missing metadata invented: %#v", missing)
			}
		})
	}
}

func TestParsePageEnclosureAndMagnetFallbacks(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	page, err := parsePage([]byte(`<rss><channel>
		<item><guid>download-only</guid><enclosure url="https://example.invalid/file.torrent" length="0"/></item>
		<item><attr name="guid" value="attribute-guid"/><attr name="magneturl" value="magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&amp;dn=Release"/><pubDate>2026-09-22T01:02:03Z</pubDate></item>
		<item><link>https://example.invalid/preferred</link><enclosure url="magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" length="200"/><attr name="size" value="100"/></item>
		<item><link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567</link><attr name="infohash" value="ABCDEF"/></item>
		<item><guid>empty-optionals</guid><enclosure url="" length=""/><attr name="seeders" value=""/><pubDate></pubDate></item>
	</channel></rss>`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("items = %d", len(page.Items))
	}
	item := page.Items[0]
	if item.Link != "https://example.invalid/file.torrent" || item.Size == nil || *item.Size != 0 || item.InfoHash != "" {
		t.Fatalf("enclosure = %#v", item)
	}
	item = page.Items[1]
	if item.GUID != "attribute-guid" || item.MagnetURL == "" || item.Link != item.MagnetURL || item.InfoHash != hash || item.PublishedAt == nil || !item.PublishedAt.Equal(time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("magnet = %#v", item)
	}
	item = page.Items[2]
	if item.Link != "https://example.invalid/preferred" || item.Size == nil || *item.Size != 100 || item.MagnetURL == "" || item.InfoHash != strings.Repeat("0", 40) {
		t.Fatalf("fallback precedence = %#v", item)
	}
	item = page.Items[3]
	if item.MagnetURL != item.Link || item.InfoHash != "abcdef" {
		t.Fatalf("explicit hash precedence = %#v", item)
	}
	item = page.Items[4]
	if item.Link != "" || item.MagnetURL != "" || item.InfoHash != "" || item.Size != nil || item.Seeders != nil || item.PublishedAt != nil {
		t.Fatalf("empty values invented metadata: %#v", item)
	}
}

func TestParsePageSupportsInlineSizeWithoutReplacingStandardAttribute(t *testing.T) {
	page, err := parsePage([]byte(`<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
		<item><guid>inline</guid><title>Inline size</title><size>2048</size><enclosure length="1024"/></item>
		<item><guid>standard</guid><title>Standard size</title><size>2048</size><torznab:attr name="size" value="4096"/></item>
	</channel></rss>`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Size == nil || *page.Items[0].Size != 2048 || page.Items[1].Size == nil || *page.Items[1].Size != 4096 {
		t.Fatalf("size precedence lost: %#v", page.Items)
	}
}

func TestParsePageOptionalResponseAndQuota(t *testing.T) {
	page, err := parsePage([]byte(`<rss><channel/></rss>`), 23, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 23 || page.Total != nil || page.RateLimit != nil {
		t.Fatalf("missing metadata = %#v", page)
	}
	page, err = parsePage([]byte(`<rss><channel><response offset="0" total="0"/><apilimits/></channel></rss>`), 23, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 0 || page.Total == nil || *page.Total != 0 || page.RateLimit == nil || page.RateLimit.Limit != nil || page.RateLimit.Remaining != nil || page.RateLimit.ResetAt != nil {
		t.Fatalf("explicit zero and unknown quota = %#v", page)
	}
	for _, test := range []struct {
		name, attrs      string
		limit, remaining int64
	}{
		{"usage", `apiMax="100" apiCurrent="90"`, 100, 10},
		{"exhausted", `apiMax="100" apiCurrent="101"`, 100, 0},
		{"zero limit", `apiMax="0" apiCurrent="0"`, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `<rss xmlns:n="http://www.newznab.com/DTD/2010/feeds/attributes/"><channel><n:apilimits ` + test.attrs + ` apiNextAvailable="Tue, 16 Jul 2019 20:56:54 +0000"/></channel></rss>`
			page, err := parsePage([]byte(body), 0, "")
			if err != nil {
				t.Fatal(err)
			}
			rate := page.RateLimit
			if rate == nil || rate.Limit == nil || *rate.Limit != test.limit || rate.Remaining == nil || *rate.Remaining != test.remaining || rate.ResetAt == nil || !rate.ResetAt.Equal(time.Date(2019, 7, 16, 20, 56, 54, 0, time.UTC)) {
				t.Fatalf("quota = %#v", rate)
			}
		})
	}
	page, err = parsePage([]byte(`<rss><channel><apilimits apiCurrent="7" apiNextAvailable="2026-09-22T01:02:03Z"/></channel></rss>`), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.RateLimit == nil || page.RateLimit.Limit != nil || page.RateLimit.Remaining != nil || page.RateLimit.ResetAt == nil {
		t.Fatalf("usage without max = %#v", page.RateLimit)
	}
}

func TestParseAPIError(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><error code="100" description="private-token-do-not-log"/>`)
	for name, parse := range map[string]func([]byte) error{
		"error": parseAPIError,
		"caps":  func(data []byte) error { _, err := parseCapabilities(data); return err },
		"page":  func(data []byte) error { _, err := parsePage(data, 0, ""); return err },
	} {
		t.Run(name, func(t *testing.T) {
			err := parse(body)
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.Code != 100 || apiError.Description != "private-token-do-not-log" {
				t.Fatalf("API error = %#v", err)
			}
			if strings.Contains(err.Error(), "private-token") {
				t.Fatal("API error exposes server description")
			}
		})
	}
	if err := parseAPIError([]byte(`<rss><channel><error code="100"/></channel></rss>`)); err != nil {
		t.Fatalf("nested error treated as error document: %v", err)
	}
	for _, body := range []string{`<error code="-1"/>`, `<error code="9223372036854775808"/>`, `<error code="100"/><rss/>`, `<error code="100">`, `<error/>`} {
		if err := parseAPIError([]byte(body)); err == nil {
			t.Errorf("accepted invalid error document %q", body)
		}
	}
}

func TestParsePageRejectsMalformedDocumentsAndNumbers(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "", "html": `<html><body>Login</body></html>`,
		"xhtml rss":       `<rss xmlns="http://www.w3.org/1999/xhtml"><channel/></rss>`,
		"missing channel": `<rss/>`, "nested channel": `<rss><html><channel/></html></rss>`,
		"multiple channels": `<rss><channel/><channel/></rss>`,
		"unclosed":          `<rss><channel>`, "mismatched": `<rss><channel></rss>`,
		"extra root":             `<rss><channel/></rss><rss><channel/></rss>`,
		"trailing content":       `<rss><channel/></rss>secret`,
		"leading content":        `secret<rss><channel/></rss>`,
		"duplicate attributes":   `<rss><channel><response total="1" total="2"/></channel></rss>`,
		"duplicate response":     `<rss><channel><response/><response/></channel></rss>`,
		"negative offset":        `<rss><channel><response offset="-1"/></channel></rss>`,
		"overflow total":         `<rss><channel><response total="9223372036854775808"/></channel></rss>`,
		"invalid size":           `<rss><channel><item><attr name="size" value="secret"/></item></channel></rss>`,
		"negative seeders":       `<rss><channel><item><attr name="seeders" value="-1"/></item></channel></rss>`,
		"overflow peers":         `<rss><channel><item><attr name="peers" value="9223372036854775808"/></item></channel></rss>`,
		"invalid repeated count": `<rss><channel><item><attr name="seeders" value="0"/><attr name="seeders" value="-1"/></item></channel></rss>`,
		"negative category":      `<rss><channel><item><attr name="category" value="-1"/></item></channel></rss>`,
		"negative enclosure":     `<rss><channel><item><enclosure length="-1"/></item></channel></rss>`,
		"negative quota":         `<rss><channel><apilimits apiMax="-1"/></channel></rss>`,
		"overflow usage":         `<rss><channel><apilimits apiCurrent="9223372036854775808"/></channel></rss>`,
		"invalid quota reset":    `<rss><channel><apilimits apiNextAvailable="secret-invalid-date"/></channel></rss>`,
		"external entity":        `<!DOCTYPE rss [<!ENTITY private SYSTEM "file:///private/secret">]><rss><channel><item><title>&private;</title></item></channel></rss>`,
		"unknown entity":         `<rss><channel><item><title>&private;</title></item></channel></rss>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parsePage([]byte(body), 0, "")
			if err == nil {
				t.Fatal("accepted invalid protocol document")
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "file:") {
				t.Fatal("parse error exposes response content")
			}
		})
	}
}

func TestParseCapabilitiesRejectsInvalidLimitsAndDocument(t *testing.T) {
	for _, body := range []string{
		`<html/>`, `<rss><channel/></rss>`, `<caps/><caps/>`, `<caps/>trailing`,
		`<caps><limits max="0"/></caps>`, `<caps><limits default="-1"/></caps>`,
		`<caps><limits max="9223372036854775808"/></caps>`,
		`<caps><categories><category id="-1"/></categories></caps>`,
		`<caps><categories><category id="1"><subcat id="9223372036854775808"/></category></categories></caps>`,
	} {
		if _, err := parseCapabilities([]byte(body)); err == nil {
			t.Errorf("accepted invalid capabilities %q", body)
		}
	}
}

func TestPublicationDatesRejectInvalidValuesWithoutLosingRawItems(t *testing.T) {
	for _, test := range []struct{ name, unit, value string }{
		{"malformed", "seconds", "not-a-date"},
		{"fraction", "seconds", "1.5"},
		{"fractional milliseconds", "milliseconds", "1.0"},
		{"exponent", "milliseconds", "1e3"},
		{"overflow", "seconds", "9223372036854775808"},
		{"underflow", "milliseconds", "-9223372036854775809"},
		{"seconds before year one", "seconds", "-62135596801"},
		{"seconds after year 9999", "seconds", "253402300800"},
		{"milliseconds before year one", "milliseconds", "-62135596800001"},
		{"milliseconds after year 9999", "milliseconds", "253402300800000"},
		{"UTC year zero", "seconds", "0001-01-01T00:00:00+01:00"},
		{"UTC year 10000", "milliseconds", "9999-12-31T23:59:59-01:00"},
		{"missing unit", "", "1726966923"},
		{"zero without unit", "", "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fragment := `<item data-original='keep'><guid>bad</guid><pubDate>` + test.value + `</pubDate></item>`
			sibling := `<item><guid>good</guid><pubDate>2026-09-22T01:02:03Z</pubDate></item>`
			body := []byte("<rss><channel>\n" + fragment + "\n" + sibling + "\n</channel></rss>")
			page, err := parsePage(body, 0, test.unit)
			if test.unit == "" {
				if err != nil || len(page.Items) != 2 || page.Items[0].PublishedAt != nil {
					t.Fatalf("default behavior guessed a numeric unit: %#v, %v", page, err)
				}
			} else if !errors.Is(err, errProtocol) {
				t.Fatalf("invalid publication date error = %v", err)
			}
			raw, err := parseRawPage(body, 0, test.unit)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw.Body) != string(body) || len(raw.Items) != 2 ||
				string(raw.Items[0].Body) != fragment || string(raw.Items[1].Body) != sibling {
				t.Fatal("raw response or item bytes changed")
			}
			if !errors.Is(raw.Items[0].Error, errProtocol) || raw.Items[0].Item.PublishedAt != nil {
				t.Fatalf("invalid date was authoritative: %#v", raw.Items[0])
			}
			if raw.Items[0].Item.GUID != "bad" {
				t.Fatal("invalid date erased source identity")
			}
			if raw.Items[1].Error != nil || raw.Items[1].Item.PublishedAt == nil {
				t.Fatalf("invalid sibling discarded valid date: %#v", raw.Items[1])
			}
		})
	}
}

func TestPublicationDateOptionPreservesMetadataErrors(t *testing.T) {
	body := []byte(`<rss><channel><response total="invalid"/><item><pubDate>0</pubDate><size>invalid</size></item></channel></rss>`)
	if _, err := parsePage(body, 0, "seconds"); !errors.Is(err, errProtocol) {
		t.Fatalf("Search metadata error = %v", err)
	}
	raw, err := parseRawPage(body, 0, "seconds")
	if !errors.Is(err, errProtocol) || len(raw.Items) != 1 || !errors.Is(raw.Items[0].Error, errProtocol) {
		t.Fatalf("raw metadata errors lost: %#v, %v", raw, err)
	}
	if string(raw.Body) != string(body) {
		t.Fatal("metadata failure changed raw response")
	}
}
