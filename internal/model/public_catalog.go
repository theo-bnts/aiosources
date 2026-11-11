package model

// Public catalog configuration contains data permissions only. It cannot carry
// source definitions, credentials, announce URLs or personal account data.
type PublicCatalogOptions struct {
	Sources         []string        `json:"sources,omitempty"`
	Fields          []string        `json:"fields,omitempty"`
	CatalogEntities CatalogEntities `json:"catalog_entities,omitzero"`
}

// One administrator-owned ceiling per instance, never a visitor profile.
type PublicCatalogPolicy struct {
	PublicCatalogOptions
	Revision int64 `json:"revision"`
}

// The HTTP service constructs this grant only after authenticating its sealed
// public configuration. Storage rechecks the live policy for every page.
type PublicCatalogGrant struct {
	Trusted *TrustedGrant
	PublicCatalogOptions
	Generation        int64
	CatalogGeneration int64
}
