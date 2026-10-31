package books

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var ErrChanges = errors.New("invalid Open Library change response")
var exportDateRE = regexp.MustCompile(`(?:ol_dump_(?:(?:editions|works|authors|redirects)_)?|ol_dump/)([0-9]{4}-[0-9]{2}-[0-9]{2})(?:[/_.]|$)`)
var changeIDRE = regexp.MustCompile(`^[0-9]{1,20}$`)

// Only identifiers, revisions and clocks are retained. Editor names, IPs,
// comments and arbitrary changeset data never enter the local catalog.
type ChangeKey struct {
	ID       string
	Revision int64
	At       time.Time
}

type Change struct {
	ID   string
	At   time.Time
	Keys []ChangeKey
}

func ParseTimestamp(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if at, err := time.Parse(layout, value); err == nil {
			// PostgreSQL checkpoints retain microseconds. Compare the same
			// precision if an API response supplies finer timestamp fractions.
			return at.UTC().Truncate(time.Microsecond), nil
		}
	}
	return time.Time{}, ErrChanges
}

// The published snapshot's date, rather than the download time, is the
// conservative starting boundary for changes that the snapshot may not contain.
func ExportDate(location string) (time.Time, bool) {
	match := exportDateRE.FindStringSubmatch(location)
	if len(match) != 2 {
		return time.Time{}, false
	}
	at, err := time.Parse("2006-01-02", match[1])
	return at, err == nil
}

func Path(id string) string {
	if id == "" || Key(id) != id {
		return ""
	}
	switch id[len(id)-1] {
	case 'W':
		return "/works/" + id
	case 'M':
		return "/books/" + id
	case 'A':
		return "/authors/" + id
	}
	return ""
}

func DecodeChanges(body []byte, now time.Time) ([]Change, error) {
	var rows []struct {
		ID        string          `json:"id"`
		Timestamp string          `json:"timestamp"`
		Changes   json.RawMessage `json:"changes"`
	}
	if json.Unmarshal(body, &rows) != nil || rows == nil || len(rows) > 1000 {
		return nil, ErrChanges
	}
	out := make([]Change, 0, len(rows))
	seen := map[string]bool{}
	var prior time.Time
	for _, row := range rows {
		at, err := ParseTimestamp(row.Timestamp)
		if err != nil || at.After(now.Add(5*time.Minute)) || !changeIDRE.MatchString(row.ID) || seen[row.ID] || (!prior.IsZero() && at.After(prior)) || len(row.Changes) == 0 {
			return nil, ErrChanges
		}
		prior, seen[row.ID] = at, true
		var keys []struct {
			Key      string `json:"key"`
			Revision int64  `json:"revision"`
		}
		if json.Unmarshal(row.Changes, &keys) != nil || len(keys) > 100000 {
			return nil, ErrChanges
		}
		change := Change{ID: row.ID, At: at}
		for _, key := range keys {
			id := Key(key.Key)
			if !(strings.HasPrefix(key.Key, "/works/") || strings.HasPrefix(key.Key, "/books/") || strings.HasPrefix(key.Key, "/authors/")) {
				continue
			}
			if id == "" || Path(id) != key.Key || key.Revision < 1 {
				return nil, ErrChanges
			}
			change.Keys = append(change.Keys, ChangeKey{ID: id, Revision: key.Revision, At: at})
		}
		out = append(out, change)
	}
	return out, nil
}

func DecodeDocument(id string, expected int64, body []byte) (Record, error) {
	var envelope struct {
		Key      string                 `json:"key"`
		Type     struct{ Key string }   `json:"type"`
		Revision int64                  `json:"revision"`
		Modified struct{ Value string } `json:"last_modified"`
	}
	if json.Unmarshal(body, &envelope) != nil || Path(id) != envelope.Key || envelope.Revision < 1 || envelope.Revision < expected {
		return Record{}, ErrChanges
	}
	record, err := decodeRecord(envelope.Type.Key, envelope.Key, body)
	if err != nil || record.Skipped || record.Key() != id {
		return Record{}, ErrChanges
	}
	record.Revision = envelope.Revision
	if at, e := ParseTimestamp(envelope.Modified.Value); e == nil {
		record.Modified = &at
	}
	return record, nil
}

// The official client also supports grouped document reads. A missing, stale
// or malformed entry is left out so its caller can retry that document through
// the individual endpoint; absence here is never evidence of deletion.
func DecodeDocuments(body []byte, revisions map[string]int64) (map[string]Record, error) {
	var envelope struct {
		Status string                     `json:"status"`
		Result map[string]json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Status != "ok" || envelope.Result == nil {
		return nil, ErrChanges
	}
	out := make(map[string]Record, len(revisions))
	for id, revision := range revisions {
		if record, err := DecodeDocument(id, revision, envelope.Result[Path(id)]); err == nil {
			out[id] = record
		}
	}
	return out, nil
}
