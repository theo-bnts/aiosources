package model

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
)

const CatalogSnapshotExpiredCode = "catalog_snapshot_expired"

// CatalogOrigin preserves identity across imports without confusing it with the
// local source that owns an imported copy.
// Native source identifiers are opaque: raw GUIDs can contain tracker secrets.
type CatalogOrigin struct {
	InstanceID string `json:"instance_id"`
	ProviderID string `json:"provider_id"`
	SourceID   string `json:"source_id"`
}

// CatalogItemID hashes a length-delimited UTF-8 identity tuple. It is stable
// across repeated imports and does not depend on JSON escaping conventions.
func CatalogItemID(origin CatalogOrigin) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, "ingest.catalog.identity.v1\x00")
	var size [8]byte
	for _, value := range []string{origin.InstanceID, origin.ProviderID, origin.SourceID} {
		binary.BigEndian.PutUint64(size[:], uint64(len(value)))
		_, _ = hash.Write(size[:])
		_, _ = io.WriteString(hash, value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type CatalogItem struct {
	ID      string         `json:"id"`
	Origin  CatalogOrigin  `json:"origin"`
	Deleted bool           `json:"deleted,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// CatalogEnvelope is the versioned HTTP/JSON sharing protocol. A checkpoint is
// published only on the final page; next_cursor and checkpoint are exclusive.
type CatalogEnvelope struct {
	Version    int           `json:"version"`
	InstanceID string        `json:"instance_id"`
	Mode       string        `json:"mode"`
	Items      []CatalogItem `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Checkpoint string        `json:"checkpoint,omitempty"`
}

// CatalogCursor is immutable run continuation state, separate from the last
// successfully published cross-run checkpoint.
type CatalogCursor struct {
	Version        int    `json:"catalog_version"`
	InstanceID     string `json:"instance_id"`
	Mode           string `json:"mode"`
	Cursor         string `json:"cursor,omitempty"`
	Checkpoint     string `json:"checkpoint,omitempty"`
	BaseCheckpoint string `json:"base_checkpoint,omitempty"`
	BaseEndpoint   string `json:"base_endpoint,omitempty"`
	Done           bool   `json:"done,omitempty"`
}
