package connectors

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
)

// Only an exact info-dictionary hash is retained. Announce URLs, passkeys,
// filenames and the downloaded metainfo body never become torrent fields.
func htmlTorrentHash(data []byte) (string, error) {
	invalid := errors.New("source did not return valid torrent metadata")
	reader := torrentReader{data: data}
	if len(data) == 0 || data[0] != 'd' {
		return "", invalid
	}
	reader.position++
	var info []byte
	var previous []byte
	for reader.position < len(data) && data[reader.position] != 'e' {
		key, err := reader.text()
		if err != nil || previous != nil && bytes.Compare(previous, key) >= 0 {
			return "", invalid
		}
		previous = key
		start := reader.position
		if reader.value(1) != nil {
			return "", invalid
		}
		if string(key) == "info" {
			info = data[start:reader.position]
		}
	}
	if reader.position != len(data)-1 || data[reader.position] != 'e' || len(info) == 0 || info[0] != 'd' {
		return "", invalid
	}
	reader = torrentReader{data: info, position: 1}
	var named, pieces, tree, pieceLength, version2 bool
	for reader.position < len(info) && info[reader.position] != 'e' {
		key, err := reader.text()
		if err != nil {
			return "", invalid
		}
		start := reader.position
		if reader.value(1) != nil {
			return "", invalid
		}
		value := info[start:reader.position]
		switch string(key) {
		case "name", "name.utf-8":
			text := torrentReader{data: value}
			name, err := text.text()
			named = named || err == nil && len(name) > 0
		case "piece length":
			length, err := strconv.ParseInt(string(value[1:len(value)-1]), 10, 64)
			pieceLength = value[0] == 'i' && err == nil && length > 0
		case "pieces":
			text := torrentReader{data: value}
			bytes, err := text.text()
			if err != nil || len(bytes)%20 != 0 {
				return "", invalid
			}
			pieces = true
		case "meta version":
			version2 = string(value) == "i2e"
		case "file tree":
			tree = value[0] == 'd'
		}
	}
	if !named || !pieceLength || !pieces && !(version2 && tree) {
		return "", invalid
	}
	if pieces {
		digest := sha1.Sum(info)
		return hex.EncodeToString(digest[:]), nil
	}
	digest := sha256.Sum256(info)
	return hex.EncodeToString(digest[:]), nil
}

// Validate bounded bencode without re-encoding the info dictionary: re-encoding
// could change the content identity. Untrusted counts never allocate buffers.
type torrentReader struct {
	data     []byte
	position int
}

func (reader *torrentReader) text() ([]byte, error) {
	start := reader.position
	for reader.position < len(reader.data) && reader.data[reader.position] >= '0' && reader.data[reader.position] <= '9' {
		reader.position++
	}
	if start == reader.position || reader.position >= len(reader.data) || reader.data[reader.position] != ':' || reader.position-start > 1 && reader.data[start] == '0' {
		return nil, errors.New("invalid bencode string")
	}
	length, err := strconv.Atoi(string(reader.data[start:reader.position]))
	reader.position++
	if err != nil || length < 0 || length > len(reader.data)-reader.position {
		return nil, errors.New("invalid bencode string length")
	}
	value := reader.data[reader.position : reader.position+length]
	reader.position += length
	return value, nil
}

func (reader *torrentReader) value(depth int) error {
	if depth > 128 || reader.position >= len(reader.data) {
		return errors.New("invalid bencode depth or boundary")
	}
	kind := reader.data[reader.position]
	if kind >= '0' && kind <= '9' {
		_, err := reader.text()
		return err
	}
	reader.position++
	switch kind {
	case 'i':
		start := reader.position
		for reader.position < len(reader.data) && reader.data[reader.position] != 'e' {
			reader.position++
		}
		value := reader.data[start:reader.position]
		if reader.position == len(reader.data) || len(value) == 0 || bytes.Equal(value, []byte("-0")) || len(value) > 1 && value[0] == '0' || len(value) > 2 && value[0] == '-' && value[1] == '0' {
			return errors.New("invalid bencode integer")
		}
		digits := value
		if digits[0] == '-' {
			digits = digits[1:]
		}
		if len(digits) == 0 {
			return errors.New("invalid bencode integer")
		}
		for _, digit := range digits {
			if digit < '0' || digit > '9' {
				return errors.New("invalid bencode integer")
			}
		}
		reader.position++
		return nil
	case 'l', 'd':
		var previous []byte
		for reader.position < len(reader.data) && reader.data[reader.position] != 'e' {
			if kind == 'd' {
				key, err := reader.text()
				if err != nil || previous != nil && bytes.Compare(previous, key) >= 0 {
					return errors.New("invalid bencode dictionary key")
				}
				previous = key
			}
			if err := reader.value(depth + 1); err != nil {
				return err
			}
		}
		if reader.position == len(reader.data) {
			return errors.New("unterminated bencode collection")
		}
		reader.position++
		return nil
	default:
		return errors.New("invalid bencode value")
	}
}
