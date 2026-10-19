package model

const MaxHTMLFields = 64

// HTMLConfig describes server-rendered listings. Selectors are data, never
// executable code. Listing markers distinguish an empty result from a login,
// challenge or changed page that must not replace a published catalog.
type HTMLConfig struct {
	PageSelector  string               `json:"page_selector"`
	RowsSelector  string               `json:"rows_selector"`
	EmptySelector string               `json:"empty_selector,omitempty"`
	NextSelector  string               `json:"next_selector,omitempty"`
	ID            HTMLField            `json:"id"`
	Fields        map[string]HTMLField `json:"fields"`
	Total         *HTMLField           `json:"total,omitempty"`
	Current       *HTMLField           `json:"current,omitempty"`
	Details       *HTMLDetails         `json:"details,omitempty"`
}

// HTMLField selects text or an attribute relative to one item. An empty selector
// means the item itself. Pattern extracts its single capture (or whole match).
type HTMLField struct {
	Selector   string `json:"selector,omitempty"`
	Attribute  string `json:"attribute,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
	Format     string `json:"format,omitempty"`
	All        bool   `json:"all,omitempty"`
	Required   bool   `json:"required,omitempty"`
	DateLayout string `json:"date_layout,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

// Details supply fields absent from a listing. Requests use the source's client,
// authentication, pacing, proxy and retry policy, and stay on its origin.
type HTMLDetails struct {
	URL          HTMLField            `json:"url"`
	PageSelector string               `json:"page_selector"`
	Fields       map[string]HTMLField `json:"fields"`
}
