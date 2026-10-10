package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// The listing window is sufficient for an incremental only when it reaches a
// prior-run boundary or the actual end. It never falls back to a Full crawl.
type orderedJSONConnector struct {
	*windowJSONConnector
}

type orderedJSONCursor struct {
	Version          int    `json:"version"`
	Phase            string `json:"phase"`
	Scope            int    `json:"scope"`
	Page             int    `json:"page"`
	KnownPages       int    `json:"known_pages,omitempty"`
	LastID           int64  `json:"last_id,omitempty"`
	MinimumRemaining int64  `json:"minimum_remaining"`
}

func (c *orderedJSONConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	p, t := c.provider, c.provider.Traversal
	var state orderedJSONCursor
	if len(checkpoint) == 0 {
		state = orderedJSONCursor{Version: 2, Phase: "incremental", Page: p.Pagination.Start, MinimumRemaining: int64(t.MinimumTotal)}
	} else {
		// Version 1 persisted a timestamp frontier. Read it for checkpoint
		// migration, but never use dates to discard unseen native identities.
		stored := struct {
			*orderedJSONCursor
			LastPublishedAt string `json:"last_published_at,omitempty"`
		}{orderedJSONCursor: &state}
		if err := decodeJSON(checkpoint, &stored); err != nil || (state.Version != 1 && state.Version != 2) ||
			(state.Phase != "incremental" && state.Phase != "done") || state.Scope < 0 || state.Scope > len(t.Scopes) ||
			(state.Phase == "incremental" && state.Scope == len(t.Scopes)) || state.Page < p.Pagination.Start ||
			state.Page-p.Pagination.Start >= t.WindowPages || state.KnownPages < 0 || state.KnownPages >= p.Schedule.KnownPages || state.LastID < 0 ||
			state.MinimumRemaining < 0 || state.MinimumRemaining > int64(t.MinimumTotal) {
			return model.Page{}, errors.New("invalid ordered JSON continuation state")
		}
	}
	state.Version = 2
	byPublication := t.IncrementalOrder == "published_at"
	page := model.Page{Next: checkpoint, Position: fmt.Sprintf("incremental:%d:%d", state.Scope, state.Page), Metadata: map[string]any{"known_pages_managed": true, "traversal_phase": "incremental", "requested_page": state.Page}}
	if state.Phase == "done" {
		page.Done = true
		return page, nil
	}
	// Start with the normal scoped listing request, then override only ordering.
	queryState := windowCursor{Phase: "root", Scope: state.Scope, Page: state.Page}
	request, err := c.request(queryState)
	if err != nil {
		return windowFail(page, err)
	}
	for name, value := range t.IDRecovery.DiscoveryQuery {
		request.Query[name], _ = queryValues(value)
	}
	request.Query.Set(p.Pagination.PageParam, strconv.Itoa(state.Page))
	request.Query.Set(p.Pagination.SizeParam, strconv.Itoa(p.PageSize))
	page.Metadata["fingerprint_scope"] = "incremental:" + t.Scopes[state.Scope].ID
	response, err := c.client.Do(ctx, request)
	page.Body, page.ContentType = response.Body, response.Header.Get("Content-Type")
	page.Metadata["http_status"] = response.StatusCode
	if err != nil {
		return windowFail(page, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return windowFail(page, httpFailure(response.StatusCode))
	}
	// Reuse the window parser and its exact raw-item preservation. Its Full
	// traversal transitions are deliberately local and discarded here.
	queryState.Totals = make([]int64, len(t.Scopes))
	if err := c.list(&page, &queryState, response.Body); err != nil {
		return windowFail(page, err)
	}
	ids := make([]string, len(page.Items))
	var last int64
	var datedIDs map[string]struct{}
	if byPublication {
		datedIDs = make(map[string]struct{}, len(page.Items))
	}
	overlap := 0
	for i, record := range page.Items {
		id, valid := canonicalNumericID(record.SourceID)
		if !valid || record.Error != "" || record.Ignored {
			return windowFail(page, errors.New("ordered JSON listing requires valid selected numeric source IDs"))
		}
		if byPublication {
			if _, err := time.Parse(time.RFC3339Nano, FieldString(record.Fields["published_at"])); err != nil {
				return windowFail(page, errors.New("ordered JSON listing requires valid publication dates"))
			}
			if _, duplicate := datedIDs[record.SourceID]; duplicate {
				return windowFail(page, fmt.Errorf("%w: incremental listing repeats a native ID", model.ErrStalled))
			}
			datedIDs[record.SourceID] = struct{}{}
		} else if last != 0 && id >= last {
			return windowFail(page, fmt.Errorf("%w: incremental source IDs are not strictly descending", model.ErrStalled))
		}
		last, ids[i] = id, record.SourceID
		if !byPublication && state.LastID > 0 && id >= state.LastID {
			overlap++
		}
	}
	if overlap > 0 {
		if overlap == len(ids) || c.env.ObservedIDs == nil {
			return windowFail(page, fmt.Errorf("%w: incremental listing did not descend beyond its committed frontier", model.ErrStalled))
		}
		for start := 0; start < overlap; start += 100 {
			batch := ids[start:min(start+100, overlap)]
			observed, err := c.env.ObservedIDs(ctx, batch)
			if err != nil {
				return windowFail(page, err)
			}
			for _, id := range batch {
				if !observed[id] {
					return windowFail(page, fmt.Errorf("%w: incremental overlap contains an unobserved native ID", model.ErrStalled))
				}
			}
		}
	}
	allKnown := len(ids) > 0
	// Keep membership work bounded independently of the configured page size.
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		known, err := c.env.KnownIDs(ctx, batch)
		if err != nil {
			return windowFail(page, err)
		}
		for _, id := range batch {
			allKnown = allKnown && known[id]
		}
	}
	if byPublication {
		// Publication sorting is a request preference, not an ID frontier:
		// late arrivals and reordering must still reach the baseline lookup.
		state.LastID = 0
	} else {
		state.LastID = last
	}
	if allKnown {
		state.KnownPages++
	} else {
		state.KnownPages = 0
	}
	pages := state.Page - p.Pagination.Start + 1
	total := page.Metadata["total"].(int64)
	if state.Page == p.Pagination.Start {
		state.MinimumRemaining -= min(state.MinimumRemaining, total)
	}
	// A short capped response is not a reliable end when JSON still advertises
	// unseen entries. An empty response before that end is actionable too.
	end := total == 0 && len(ids) == 0
	if total > 0 {
		end = int64(pages-1)*int64(p.PageSize)+int64(len(ids)) >= total
	}
	boundary := state.KnownPages >= p.Schedule.KnownPages
	if boundary || end {
		if boundary {
			page.Metadata["incremental_stop"] = "prior_run_boundary"
		} else {
			page.Metadata["incremental_stop"] = "end"
		}
		state.Scope++
		state.Page, state.KnownPages, state.LastID = p.Pagination.Start, 0, 0
		if state.Scope == len(t.Scopes) {
			if state.MinimumRemaining > 0 {
				page.Metadata["minimum_total"] = t.MinimumTotal
				page.Metadata["coverage_incomplete"] = true
				return windowFail(page, fmt.Errorf("%w: root totals are below the configured visibility minimum", model.ErrStalled))
			}
			state.Phase, page.Done = "done", true
		}
	} else if pages >= t.WindowPages || len(ids) < p.PageSize {
		page.Metadata["coverage_incomplete"] = true
		return windowFail(page, fmt.Errorf("%w: ordered incremental window ended before a prior-run boundary or true end", model.ErrStalled))
	} else {
		state.Page++
	}
	page.Next, err = json.Marshal(state)
	if err != nil {
		return windowFail(page, err)
	}
	return page, nil
}

func canonicalNumericID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}
