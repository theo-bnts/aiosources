package model

// StreamPreferences are installation and view preferences, never credentials.
// An empty mode preserves older installations which did not select a language.
type StreamPreferences struct {
	Language string `json:"language"`
	Mode     string `json:"mode"`
}
