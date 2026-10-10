package torznab

import (
	"errors"
	"strings"
)

const (
	projectionGUID = iota
	projectionTitle
	projectionLink
	projectionComments
	projectionPublishedAt
	projectionSize
	projectionInfoHash
	projectionMagnetURL
	projectionCategories
	projectionSeeders
	projectionPeers
)

var projectionKinds = map[string]int{
	"guid":         projectionGUID,
	"title":        projectionTitle,
	"link":         projectionLink,
	"comments":     projectionComments,
	"published_at": projectionPublishedAt,
	"size":         projectionSize,
	"info_hash":    projectionInfoHash,
	"magnet_url":   projectionMagnetURL,
	"categories":   projectionCategories,
	"seeders":      projectionSeeders,
	"peers":        projectionPeers,
}

type projectedField struct {
	name string
	kind int
}

// Projection selects output fields without changing the Item used by pagination.
// Construct it once with NewProjection, then reuse it across items and goroutines.
// It has no database dependency and does not construct SQL statements.
type Projection struct {
	fields        []projectedField
	attributes    []string
	allAttributes bool
	capacity      int
}

// NewProjection compiles a nonempty field selection. Normalized field names use
// the Item JSON names: guid, title, link, comments, published_at, size, info_hash,
// magnet_url, categories, seeders and peers. Select attributes.NAME for a literal
// extended attribute name, or attributes for all extended attributes. Attribute
// values remain lists under the nested attributes object, never coerced scalars.
// Unknown normalized fields and empty selections are errors, not full output.
func NewProjection(fields []string) (*Projection, error) {
	if len(fields) == 0 {
		return nil, errors.New("torznab: output.fields must contain at least one field when specified")
	}
	projection := &Projection{}
	seen := make(map[string]struct{}, len(fields))
	for _, name := range fields {
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		switch {
		case name == "attributes":
			projection.allAttributes = true
			projection.attributes = nil
		case strings.HasPrefix(name, "attributes."):
			attribute := strings.TrimPrefix(name, "attributes.")
			if attribute == "" || strings.TrimSpace(attribute) != attribute {
				return nil, errors.New("torznab: output.fields contains an invalid attribute selector")
			}
			if !projection.allAttributes {
				projection.attributes = append(projection.attributes, attribute)
			}
		default:
			kind, known := projectionKinds[name]
			if !known {
				return nil, errors.New("torznab: output.fields contains an unknown normalized field")
			}
			projection.fields = append(projection.fields, projectedField{name: name, kind: kind})
		}
	}
	projection.capacity = len(projection.fields)
	if projection.allAttributes || len(projection.attributes) > 0 {
		projection.capacity++
	}
	return projection, nil
}

// Project returns only selected, available values from a non-nil item. Missing
// optional values are omitted; explicit numeric zero remains zero. The returned
// map owns its top-level entries and selected-attribute map but borrows source
// slices and, when all attributes are selected, the source attributes map. Treat
// those values as read-only. Project never mutates the source item or its IDs.
//
// Omitted fields can be left untouched during database updates rather than
// overwriting a known value with NULL. An application controls insert defaults,
// SQL column mappings and its own persistence policy.
func (p *Projection) Project(item *Item) map[string]any {
	result := make(map[string]any, p.capacity)
	for _, field := range p.fields {
		var value any
		switch field.kind {
		case projectionGUID:
			if item.GUID != "" {
				value = item.GUID
			}
		case projectionTitle:
			if item.Title != "" {
				value = item.Title
			}
		case projectionLink:
			if item.Link != "" {
				value = item.Link
			}
		case projectionComments:
			if item.Comments != "" {
				value = item.Comments
			}
		case projectionPublishedAt:
			if item.PublishedAt != nil {
				value = *item.PublishedAt
			}
		case projectionSize:
			if item.Size != nil {
				value = *item.Size
			}
		case projectionInfoHash:
			if item.InfoHash != "" {
				value = item.InfoHash
			}
		case projectionMagnetURL:
			if item.MagnetURL != "" {
				value = item.MagnetURL
			}
		case projectionCategories:
			if len(item.Categories) > 0 {
				value = item.Categories
			}
		case projectionSeeders:
			if item.Seeders != nil {
				value = *item.Seeders
			}
		case projectionPeers:
			if item.Peers != nil {
				value = *item.Peers
			}
		}
		if value != nil {
			result[field.name] = value
		}
	}
	if p.allAttributes {
		if len(item.Attributes) > 0 {
			result["attributes"] = item.Attributes
		}
		return result
	}
	var attributes map[string][]string
	for _, name := range p.attributes {
		if values := item.Attributes[name]; len(values) > 0 {
			if attributes == nil {
				attributes = make(map[string][]string, len(p.attributes))
			}
			attributes[name] = values
		}
	}
	if attributes != nil {
		result["attributes"] = attributes
	}
	return result
}
