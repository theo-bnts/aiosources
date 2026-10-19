package model

// IsJSON accepts the original identifier so saved definitions and immutable run
// snapshots remain usable. New definitions use the shorter canonical ID.
func (p Provider) IsJSON() bool {
	return p.Adapter == "json" || p.Adapter == "http_json"
}
