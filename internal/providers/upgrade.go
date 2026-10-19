package providers

import (
	"encoding/json"
	"strings"
)

// UpgradeDefinitions changes only the adapter token of valid legacy JSON
// definitions. Filenames, IDs, formatting and exact numeric values survive.
// It runs before collection recovery; durable run snapshots are never edited.
func (r *Registry) UpgradeDefinitions() (int, error) {
	if err := r.acquire(); err != nil {
		return 0, err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, entry := range definitions {
		if !entry.readable || len(entry.document.Issues) > 0 || entry.document.Provider.Adapter != "http_json" {
			continue
		}
		raw, err := canonicalJSONAdapter(entry.document.JSON)
		if err != nil {
			return updated, err
		}
		if err := r.atomicWrite(entry.name, []byte(raw), entry.document.Revision); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func canonicalJSONAdapter(raw string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return "", err
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return "", err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		if key == "adapter" {
			end := int(decoder.InputOffset())
			return raw[:end-len(value)] + `"json"` + raw[end:], nil
		}
	}
	return raw, nil
}
