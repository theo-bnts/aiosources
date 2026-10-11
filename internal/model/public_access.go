package model

// PublicAccess contains no visitor or administrator credentials.
type PublicAccess struct {
	Revision   int64 `json:"revision"`
	Enabled    bool  `json:"enabled"`
	Generation int64 `json:"-"`
}
