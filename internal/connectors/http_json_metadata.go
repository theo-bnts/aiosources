package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Metadata walks published native identities, independently of listing windows
// and selected discovery scopes. Only committed progress enters its checkpoint;
// candidates are read again after a restart or an uncommitted response.
type metadataJSONConnector struct {
	*windowJSONConnector
}

type metadataJSONCursor struct {
	Version int    `json:"metadata_version"`
	After   string `json:"after,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

func fieldsComplete(fields map[string]any, required []string) bool {
	for _, field := range required {
		if value, found := fields[field]; !found || value == nil {
			return false
		}
	}
	return true
}

func metadataNext(page model.Page, state metadataJSONCursor) (model.Page, error) {
	var err error
	page.Next, err = json.Marshal(state)
	if err != nil {
		return windowFail(page, err)
	}
	page.Done = state.Done
	return page, nil
}

func (c *metadataJSONConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	var state metadataJSONCursor
	if len(checkpoint) == 0 {
		state.Version = 1
	} else if err := decodeJSON(checkpoint, &state); err != nil || state.Version != 1 {
		return model.Page{}, errors.New("invalid JSON metadata continuation state")
	}
	page := model.Page{Next: checkpoint, Position: "metadata:" + state.After, Metadata: map[string]any{
		"known_pages_managed": true, "traversal_phase": "metadata", "auxiliary_response": true,
	}}
	if state.Done {
		page.Done = true
		return page, nil
	}
	// One candidate keeps both memory and membership work bounded without
	// persisting a stale queue or consuming progress before SavePage commits.
	candidates, err := c.env.MetadataCandidates(ctx, state.After, 1)
	if err != nil {
		return windowFail(page, err)
	}
	if len(candidates) == 0 {
		state.Done = true
		return metadataNext(page, state)
	}
	if len(candidates) != 1 {
		return windowFail(page, errors.New("JSON metadata candidates exceed the requested batch size"))
	}
	candidate := candidates[0]
	if candidate.SourceID <= state.After {
		return windowFail(page, errors.New("JSON metadata candidates are not ordered native identities"))
	}
	page.Position = "metadata:" + candidate.SourceID
	page.Metadata["requested_id"] = candidate.SourceID
	hash, err := normalizeField("info_hash", candidate.InfoHash, c.unit)
	if err != nil || hash != candidate.InfoHash || !c.validToken(candidate.InfoHash) {
		page.Metadata["detail_skip_reason"] = "invalid_info_hash"
		state.After = candidate.SourceID
		return metadataNext(page, state)
	}
	page.Metadata["fingerprint_scope"] = fmt.Sprintf("json-metadata:%s:%s", candidate.SourceID, candidate.InfoHash)
	recovery := c.provider.Traversal.IDRecovery
	target, err := windowURL(c.provider, recovery.DetailURL, map[string]string{"value": candidate.InfoHash})
	if err != nil {
		return windowFail(page, err)
	}
	response, requestErr := c.client.Do(ctx, Request{Method: http.MethodGet, URL: target, Headers: http.Header{"Accept": {"application/json"}}, DisableRedirects: true})
	page.Body, page.ContentType = response.Body, response.Header.Get("Content-Type")
	page.Metadata["http_status"] = response.StatusCode
	hole := (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone) && FailureCode(requestErr) == "http"
	if requestErr != nil && !hole {
		return windowFail(page, requestErr)
	}
	if hole {
		page.Metadata["detail_skip_reason"] = "unavailable"
	} else {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return windowFail(page, httpFailure(response.StatusCode))
		}
		var root any
		if err := decodeJSON(response.Body, &root); err != nil {
			return windowFail(page, err)
		}
		raw, found, err := rawPointer(response.Body, recovery.DetailPath)
		if err != nil {
			return windowFail(page, err)
		}
		if !found {
			return windowFail(page, errors.New("detail_path is missing"))
		}
		// Existing catalogue membership, not today's discovery scopes, makes
		// this identity eligible. Do not run the listing scope matcher here.
		id, fields, parseErr := FieldsFromJSON(raw, recovery.Mapping, c.unit)
		record := model.Record{SourceID: id, Fields: fields, Raw: raw, ContentType: "application/json"}
		if parseErr != nil {
			record.Error = parseErr.Error()
		}
		page.Items = []model.Record{record}
		if err := c.checkMetadataDetail(&page, candidate); err != nil {
			return windowFail(page, err)
		}
	}
	state.After = candidate.SourceID
	return metadataNext(page, state)
}

func (c *metadataJSONConnector) checkMetadataDetail(page *model.Page, candidate model.MetadataCandidate) error {
	record := &page.Items[0]
	page.Metadata["returned_id"] = record.SourceID
	if record.Error != "" {
		return errors.New(record.Error)
	}
	if record.SourceID != candidate.SourceID {
		// The hash is only a locator: never publish another native identity or
		// leave an ambiguous shared-hash lookup blocking the catalogue walk.
		record.Auxiliary, record.Ignored = true, true
		page.Metadata["detail_skip_reason"] = "native_id_mismatch"
		return nil
	}
	if FieldString(record.Fields["info_hash"]) != candidate.InfoHash {
		return errors.New("JSON detail info_hash differs from the catalogue candidate")
	}
	if !fieldsComplete(record.Fields, c.provider.MetadataFields()) {
		return errors.New("JSON detail is missing required metadata fields")
	}
	return nil
}
