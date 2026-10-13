package model

// PublicAccess contains no visitor or administrator credentials.
type PublicAccess struct {
	Revision              int64 `json:"revision"`
	Enabled               bool  `json:"enabled"`
	AllowPrivateDownloads bool  `json:"allow_private_downloads"`
	Generation            int64 `json:"-"`
}
