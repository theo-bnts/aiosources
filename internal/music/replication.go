package music

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// ReplicationChange represents one row mutation from dbmirror2.pending_data.
type ReplicationChange struct {
	SeqID   int64
	Schema  string         // "musicbrainz" or "cover_art_archive"
	Table   string         // bare table name, e.g. "release_group", "cover_art"
	Op      string         // "i", "u", "d"
	OldData map[string]any // populated for "u" and "d"
	NewData map[string]any // populated for "i" and "u"
}

// ReplicationPacket holds the verified header and filtered changes from an
// hourly MusicBrainz Live Data Feed archive (replication-<SEQ>-v2.tar.bz2).
type ReplicationPacket struct {
	DumpHeader
	DBMirrorVersion int
	Changes         []ReplicationChange
	Read            int64
	Skipped         int64
}

// ParseReplicationPacket streams a bzip2 replication packet archive and retains
// only changes for watched MusicBrainz core tables (and Cover Art Archive
// tables when includeCovers is true).
func ParseReplicationPacket(ctx context.Context, r io.Reader, includeCovers bool) (ReplicationPacket, error) {
	var packet ReplicationPacket
	tr := tar.NewReader(bzip2.NewReader(r))
	for {
		if err := ctx.Err(); err != nil {
			return packet, err
		}
		entry, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return packet, fmt.Errorf("read MusicBrainz replication packet: %w", err)
		}
		if entry.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(path.Clean(entry.Name), "./")
		switch name {
		case "TIMESTAMP":
			data, err := io.ReadAll(io.LimitReader(tr, 256))
			if err != nil {
				return packet, err
			}
			if ts, ok := parseMusicBrainzTimestamp(string(data)); ok {
				packet.Timestamp = ts
			}
		case "REPLICATION_SEQUENCE":
			data, err := io.ReadAll(io.LimitReader(tr, 64))
			if err != nil {
				return packet, err
			}
			n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			if err != nil || n <= 0 {
				return packet, errors.New("invalid replication REPLICATION_SEQUENCE")
			}
			packet.ReplicationSequence = n
		case "SCHEMA_SEQUENCE":
			data, err := io.ReadAll(io.LimitReader(tr, 64))
			if err != nil {
				return packet, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || n <= 0 {
				return packet, errors.New("invalid replication SCHEMA_SEQUENCE")
			}
			packet.SchemaSequence = n
		case "DBMIRROR_VERSION":
			data, err := io.ReadAll(io.LimitReader(tr, 32))
			if err != nil {
				return packet, err
			}
			n, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || n < 2 {
				return packet, errors.New("unsupported MusicBrainz replication format")
			}
			packet.DBMirrorVersion = n
		case "mbdump/dbmirror2.pending_data", "mbdump/pending_data":
			if packet.SchemaSequence > 0 && !SupportedSchema(packet.SchemaSequence) {
				return packet, fmt.Errorf("%w: %d", ErrUnsupportedSchema, packet.SchemaSequence)
			}
			if err := readPendingData(ctx, tr, includeCovers, &packet); err != nil {
				return packet, err
			}
		}
	}
	if packet.ReplicationSequence <= 0 || packet.SchemaSequence <= 0 {
		return packet, errors.New("incomplete MusicBrainz replication packet")
	}
	if !SupportedSchema(packet.SchemaSequence) {
		return packet, fmt.Errorf("%w: %d", ErrUnsupportedSchema, packet.SchemaSequence)
	}
	return packet, nil
}

func readPendingData(ctx context.Context, r io.Reader, includeCovers bool, packet *ReplicationPacket) error {
	reader := bufio.NewReaderSize(r, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, oversized, eof, err := readBoundedLine(reader, maxDumpLineBytes)
		if err != nil {
			return err
		}
		if eof && len(line) == 0 && !oversized {
			break
		}
		packet.Read++
		if oversized {
			packet.Skipped++
			if eof {
				break
			}
			continue
		}
		if line == "" {
			if eof {
				break
			}
			continue
		}
		change, keep, ok := decodePendingDataLine(line, includeCovers)
		if !ok {
			packet.Skipped++
		} else if keep {
			packet.Changes = append(packet.Changes, change)
		}
		if eof {
			break
		}
	}
	return nil
}

func decodePendingDataLine(line string, includeCovers bool) (ReplicationChange, bool, bool) {
	cols := strings.Split(line, "\t")
	if len(cols) < 6 {
		return ReplicationChange{}, false, false
	}
	seqID, ok := copyInt64(cols, 0)
	rawTable, tableNull := copyField(cols, 1)
	op, opNull := copyField(cols, 2)
	if !ok || tableNull || opNull {
		return ReplicationChange{}, false, false
	}
	op = strings.ToLower(strings.TrimSpace(op))
	if op != "i" && op != "u" && op != "d" {
		return ReplicationChange{}, false, false
	}
	schema, table := splitQualifiedTable(rawTable)
	watched := false
	switch schema {
	case "musicbrainz":
		watched = WatchedCoreTables[table]
	case "cover_art_archive":
		watched = includeCovers && WatchedCoverTables[table]
	}
	if !watched {
		return ReplicationChange{}, false, true
	}
	oldRaw, oldNull := copyField(cols, 4)
	newRaw, newNull := copyField(cols, 5)
	var oldData, newData map[string]any
	if !oldNull && oldRaw != "" {
		if err := decodeJSONObject(oldRaw, &oldData); err != nil {
			return ReplicationChange{}, false, false
		}
	}
	if !newNull && newRaw != "" {
		if err := decodeJSONObject(newRaw, &newData); err != nil {
			return ReplicationChange{}, false, false
		}
	}
	return ReplicationChange{
		SeqID:   seqID,
		Schema:  schema,
		Table:   table,
		Op:      op,
		OldData: oldData,
		NewData: newData,
	}, true, true
}

func splitQualifiedTable(raw string) (schema, table string) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), `"`, "")
	if s, t, ok := strings.Cut(cleaned, "."); ok {
		return strings.ToLower(s), strings.ToLower(t)
	}
	return "musicbrainz", strings.ToLower(cleaned)
}

func decodeJSONObject(raw string, out *map[string]any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// JSONInt reads an integer column value from a dbmirror2 JSONB map.
func JSONInt(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

// JSONInt64 reads a 64-bit integer column value from a dbmirror2 JSONB map.
func JSONInt64(m map[string]any, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	}
	return 0, false
}

// JSONString reads a string column value from a dbmirror2 JSONB map.
func JSONString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
