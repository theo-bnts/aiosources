package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/theo-bnts/aiosources/internal/model"
)

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "A JSON request is required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		writeError(w, http.StatusBadRequest, "Invalid or oversized JSON body")
		return false
	}
	// Validate the document before decoding into a command. encoding/json otherwise
	// accepts duplicate keys and null objects, which can silently change intent.
	if !uniqueJSONObject(data) {
		writeError(w, http.StatusBadRequest, "A single JSON object with unique field names is required")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON fields or values")
		return false
	}
	return true
}

func uniqueJSONObject(data []byte) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := uniqueJSONValue(decoder, 0); err != nil {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return model.ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return model.ErrInvalid
			}
			seen[key] = true
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return model.ErrInvalid
	}
	_, err = decoder.Token()
	return err
}

// Endpoints retain their page sizes, but share parsing and the same offset
// bound. Ambiguous duplicate parameters are rejected by requestQuery first.
func paginationOptions(w http.ResponseWriter, query url.Values, defaultLimit, maxLimit int) (model.ListOptions, bool) {
	options := model.ListOptions{Limit: defaultLimit}
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", maxLimit))
			return options, false
		}
		options.Limit = value
	}
	if raw := query.Get("offset"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1000000 {
			writeError(w, http.StatusBadRequest, "offset must be between 0 and 1000000")
			return options, false
		}
		options.Offset = value
	}
	return options, true
}
