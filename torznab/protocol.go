package torznab

import (
	"bytes"
	"encoding/base32"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var errProtocol = errors.New("torznab: invalid XML protocol response")

// xmlTokens enforces document rules that encoding/xml alone does not enforce.
// Directives (including DTDs) are unnecessary for this protocol and rejected;
// no custom entities or external resource resolver are ever installed.
type xmlTokens struct {
	decoder  *xml.Decoder
	depth    int
	root     xml.Name
	declared bool
}

func (r *xmlTokens) Token() (xml.Token, error) {
	token, err := r.decoder.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case xml.StartElement:
		if r.depth == 0 {
			if r.root.Local != "" {
				return nil, errProtocol
			}
			r.root = t.Name
		}
		seen := make(map[xml.Name]struct{}, len(t.Attr))
		for _, attr := range t.Attr {
			if _, exists := seen[attr.Name]; exists {
				return nil, errProtocol
			}
			seen[attr.Name] = struct{}{}
		}
		r.depth++
	case xml.EndElement:
		r.depth--
	case xml.CharData:
		if r.depth == 0 && len(bytes.TrimSpace(t)) != 0 {
			return nil, errProtocol
		}
	case xml.Directive:
		return nil, errProtocol
	case xml.ProcInst:
		if strings.EqualFold(t.Target, "xml") {
			if t.Target != "xml" || r.root.Local != "" || r.declared {
				return nil, errProtocol
			}
			r.declared = true
		}
	}
	return token, nil
}

func decodeProtocol(data []byte, root string, value any) error {
	tokens := &xmlTokens{decoder: xml.NewDecoder(bytes.NewReader(data))}
	decoder := xml.NewTokenDecoder(tokens)
	if err := decoder.Decode(value); err != nil {
		return errProtocol
	}
	if tokens.root.Local != root || tokens.root.Space == "http://www.w3.org/1999/xhtml" {
		return errProtocol
	}
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errProtocol
		}
	}
}

func parseAPIError(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "error" {
			return nil
		}
		var raw struct {
			Code        string `xml:"code,attr"`
			Description string `xml:"description,attr"`
		}
		if err := decodeProtocol(data, "error", &raw); err != nil {
			return err
		}
		code, err := protocolInt(raw.Code)
		if err != nil {
			return err
		}
		return &APIError{Code: code, Description: raw.Description}
	}
}

type capsCategory struct {
	ID            string         `xml:"id,attr"`
	Name          string         `xml:"name,attr"`
	Subcategories []capsCategory `xml:"subcat"`
}

func parseCapabilities(data []byte) (Capabilities, error) {
	if err := parseAPIError(data); err != nil {
		return Capabilities{}, err
	}
	var raw struct {
		Server struct {
			Title string `xml:"title,attr"`
		} `xml:"server"`
		Limits struct {
			Default string `xml:"default,attr"`
			Max     string `xml:"max,attr"`
		} `xml:"limits"`
		Searching struct {
			Modes []struct {
				XMLName   xml.Name
				Available string `xml:"available,attr"`
				Params    string `xml:"supportedParams,attr"`
			} `xml:",any"`
		} `xml:"searching"`
		Categories []capsCategory `xml:"categories>category"`
	}
	if err := decodeProtocol(data, "caps", &raw); err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{Title: strings.TrimSpace(raw.Server.Title), Searches: make(map[SearchMode]SearchCapability)}
	for _, limit := range []struct {
		raw         string
		destination *int
	}{
		{raw.Limits.Default, &caps.Limits.Default}, {raw.Limits.Max, &caps.Limits.Max},
	} {
		if strings.TrimSpace(limit.raw) == "" {
			continue
		}
		value, err := protocolInt(limit.raw)
		if err != nil || value == 0 {
			return Capabilities{}, errProtocol
		}
		*limit.destination = value
	}
	for _, search := range raw.Searching.Modes {
		var mode SearchMode
		switch search.XMLName.Local {
		case "search":
			mode = ModeSearch
		case "tv-search":
			mode = ModeTV
		case "movie-search":
			mode = ModeMovie
		case "audio-search":
			mode = ModeMusic
		case "book-search":
			mode = ModeBook
		default:
			continue
		}
		available := strings.ToLower(strings.TrimSpace(search.Available))
		capability := SearchCapability{Available: available == "yes" || available == "true" || available == "1"}
		for _, param := range strings.Split(search.Params, ",") {
			if param = strings.TrimSpace(param); param != "" {
				capability.SupportedParams = appendUniqueString(capability.SupportedParams, param)
			}
		}
		caps.Searches[mode] = capability
	}
	var err error
	caps.Categories, err = normalizeCategories(raw.Categories)
	if err != nil {
		return Capabilities{}, err
	}
	return caps, nil
}

func normalizeCategories(raw []capsCategory) ([]Category, error) {
	var categories []Category
	for _, source := range raw {
		id, err := protocolInt(source.ID)
		if err != nil {
			return nil, err
		}
		subcategories, err := normalizeCategories(source.Subcategories)
		if err != nil {
			return nil, err
		}
		categories = append(categories, Category{ID: id, Name: strings.TrimSpace(source.Name), Subcategories: subcategories})
	}
	return categories, nil
}

type rssItem struct {
	GUID       string   `xml:"guid"`
	Title      string   `xml:"title"`
	Link       string   `xml:"link"`
	Comments   string   `xml:"comments"`
	Date       string   `xml:"pubDate"`
	Size       string   `xml:"size"`
	Categories []string `xml:"category"`
	Magnet     string   `xml:"magneturl"`
	InfoHash   string   `xml:"infohash"`
	Enclosures []struct {
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
	Attributes []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

type rssResponse struct {
	XMLName xml.Name
	Offset  string `xml:"offset,attr"`
	Total   string `xml:"total,attr"`
}

func parsePagination(responses []rssResponse, requestedOffset int) (int, *int, error) {
	if len(responses) > 2 {
		return 0, nil, errProtocol
	}
	if len(responses) == 2 {
		const newznab = "http://www.newznab.com/DTD/2010/feeds/attributes/"
		const torznab = "http://torznab.com/schemas/2015/feed"
		first, second := responses[0].XMLName.Space, responses[1].XMLName.Space
		// Only the equivalent Newznab/Torznab pair is an alias, not repeated
		// envelopes in the same namespace (even under different prefixes).
		if !((first == newznab && second == torznab) || (first == torznab && second == newznab)) {
			return 0, nil, errProtocol
		}
	}
	type pagination struct {
		offset, total       int
		hasOffset, hasTotal bool
	}
	var parsed pagination
	for index, response := range responses {
		current := pagination{
			hasOffset: strings.TrimSpace(response.Offset) != "",
			hasTotal:  strings.TrimSpace(response.Total) != "",
		}
		var err error
		if current.hasOffset {
			current.offset, err = protocolInt(response.Offset)
			if err != nil {
				return 0, nil, err
			}
		}
		if current.hasTotal {
			current.total, err = protocolInt(response.Total)
			if err != nil {
				return 0, nil, err
			}
		}
		if index > 0 && current != parsed {
			return 0, nil, errProtocol
		}
		parsed = current
	}
	if parsed.hasOffset {
		requestedOffset = parsed.offset
	}
	if parsed.hasTotal {
		total := parsed.total
		return requestedOffset, &total, nil
	}
	return requestedOffset, nil, nil
}

type rssQuota struct {
	Max     string `xml:"apiMax,attr"`
	Current string `xml:"apiCurrent,attr"`
	Next    string `xml:"apiNextAvailable,attr"`
}

func parseQuota(quota rssQuota) (*RateLimit, error) {
	limit, err := optionalProtocolInt64(quota.Max)
	if err != nil {
		return nil, err
	}
	current, err := optionalProtocolInt64(quota.Current)
	if err != nil {
		return nil, err
	}
	rate := &RateLimit{Limit: limit, ResetAt: protocolDate(quota.Next)}
	if strings.TrimSpace(quota.Next) != "" && rate.ResetAt == nil {
		return nil, errProtocol
	}
	if limit != nil && current != nil {
		remaining := max(0, *limit-*current)
		rate.Remaining = &remaining
	}
	return rate, nil
}

func parsePage(data []byte, requestedOffset int, publishedAtUnit string) (Page, error) {
	if requestedOffset < 0 {
		return Page{}, errProtocol
	}
	if err := parseAPIError(data); err != nil {
		return Page{}, err
	}
	var raw struct {
		Channels []struct {
			Items     []rssItem     `xml:"item"`
			Responses []rssResponse `xml:"response"`
			Quotas    []rssQuota    `xml:"apilimits"`
		} `xml:"channel"`
	}
	if err := decodeProtocol(data, "rss", &raw); err != nil {
		return Page{}, err
	}
	if len(raw.Channels) != 1 {
		return Page{}, errProtocol
	}
	channel := raw.Channels[0]
	if len(channel.Quotas) > 1 {
		return Page{}, errProtocol
	}
	offset, total, err := parsePagination(channel.Responses, requestedOffset)
	if err != nil {
		return Page{}, err
	}
	page := Page{Offset: offset, Total: total, Items: make([]Item, 0, len(channel.Items))}
	if len(channel.Quotas) == 1 {
		page.RateLimit, err = parseQuota(channel.Quotas[0])
		if err != nil {
			return Page{}, err
		}
	}
	for _, source := range channel.Items {
		item, err := normalizeItem(source, publishedAtUnit)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func normalizeItem(raw rssItem, publishedAtUnit string) (Item, error) {
	publishedAt, dateErr := publicationDate(raw.Date, publishedAtUnit)
	item := Item{
		GUID: strings.TrimSpace(raw.GUID), Title: strings.TrimSpace(raw.Title),
		Link: strings.TrimSpace(raw.Link), Comments: strings.TrimSpace(raw.Comments),
		PublishedAt: publishedAt, MagnetURL: strings.TrimSpace(raw.Magnet),
		InfoHash:   strings.ToLower(strings.TrimSpace(raw.InfoHash)),
		Attributes: make(map[string][]string),
	}
	for _, attr := range raw.Attributes {
		name, value := strings.TrimSpace(attr.Name), strings.TrimSpace(attr.Value)
		if name == "" || value == "" {
			continue
		}
		item.Attributes[name] = appendUniqueString(item.Attributes[name], value)
	}
	for _, field := range []struct {
		name        string
		destination *string
	}{
		{"guid", &item.GUID}, {"magneturl", &item.MagnetURL}, {"infohash", &item.InfoHash},
	} {
		if values := item.Attributes[field.name]; *field.destination == "" && len(values) > 0 {
			*field.destination = values[0]
		}
	}
	item.InfoHash = strings.ToLower(item.InfoHash)
	for _, field := range []struct {
		name        string
		destination **int64
	}{
		{"size", &item.Size}, {"seeders", &item.Seeders}, {"peers", &item.Peers},
	} {
		for _, rawValue := range item.Attributes[field.name] {
			value, err := optionalProtocolInt64(rawValue)
			if err != nil {
				return Item{}, err
			}
			if *field.destination == nil {
				*field.destination = value
			}
		}
	}
	inlineSize, err := optionalProtocolInt64(raw.Size)
	if err != nil {
		return Item{}, err
	}
	if item.Size == nil {
		item.Size = inlineSize
	}
	for _, category := range raw.Categories {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		// RSS also permits category names; only numeric values are category IDs.
		if !protocolDigits(category) && !((category[0] == '-' || category[0] == '+') && protocolDigits(category[1:])) {
			continue
		}
		id, err := protocolInt(category)
		if err != nil {
			return Item{}, err
		}
		item.Categories = appendUniqueInt(item.Categories, id)
	}
	for _, category := range item.Attributes["category"] {
		id, err := protocolInt(category)
		if err != nil {
			return Item{}, err
		}
		item.Categories = appendUniqueInt(item.Categories, id)
	}
	for _, enclosure := range raw.Enclosures {
		link := strings.TrimSpace(enclosure.URL)
		if item.Link == "" {
			item.Link = link
		}
		if item.MagnetURL == "" && isMagnet(link) {
			item.MagnetURL = link
		}
		size, err := optionalProtocolInt64(enclosure.Length)
		if err != nil {
			return Item{}, err
		}
		if item.Size == nil {
			item.Size = size
		}
	}
	if item.MagnetURL == "" && isMagnet(item.Link) {
		item.MagnetURL = item.Link
	}
	if item.Link == "" {
		item.Link = item.MagnetURL
	}
	if item.InfoHash == "" && item.MagnetURL != "" {
		item.InfoHash = magnetInfoHash(item.MagnetURL)
	}
	return item, dateErr
}

func isMagnet(value string) bool {
	return len(value) >= len("magnet:") && strings.EqualFold(value[:len("magnet:")], "magnet:")
}

func magnetInfoHash(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "magnet") {
		return ""
	}
	for _, topic := range parsed.Query()["xt"] {
		const prefix = "urn:btih:"
		if len(topic) < len(prefix) || !strings.EqualFold(topic[:len(prefix)], prefix) {
			continue
		}
		hash := topic[len(prefix):]
		if len(hash) == 40 {
			if _, err := hex.DecodeString(hash); err == nil {
				return strings.ToLower(hash)
			}
		}
		if len(hash) == 32 {
			if decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(hash)); err == nil {
				return hex.EncodeToString(decoded)
			}
		}
	}
	return ""
}

func protocolInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if !protocolDigits(value) {
		return 0, errProtocol
	}
	number, err := strconv.ParseUint(value, 10, strconv.IntSize-1)
	if err != nil {
		return 0, errProtocol
	}
	return int(number), nil
}

func optionalProtocolInt64(value string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if !protocolDigits(value) {
		return nil, errProtocol
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, errProtocol
	}
	return &number, nil
}

func protocolDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func protocolDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	date, err := mail.ParseDate(value)
	if err != nil {
		date, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return nil
	}
	return &date
}

func publicationDate(value, unit string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if date := protocolDate(value); date != nil {
		utc := date.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return nil, errProtocol
		}
		return &utc, nil
	}
	if unit == "" {
		// Preserve Search's existing behavior for unrecognized textual dates.
		// SearchRaw separately marks nonempty unrecognized dates as invalid.
		return nil, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, errProtocol
	}
	var date time.Time
	switch unit {
	case "seconds":
		if number < -62135596800 || number > 253402300799 {
			return nil, errProtocol
		}
		date = time.Unix(number, 0).UTC()
	case "milliseconds":
		if number < -62135596800000 || number > 253402300799999 {
			return nil, errProtocol
		}
		date = time.UnixMilli(number).UTC()
	default:
		return nil, errProtocol
	}
	return &date, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
