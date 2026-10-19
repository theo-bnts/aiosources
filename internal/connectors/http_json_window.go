package connectors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Refresh root totals between finite numeric ranges, even when the listing
// cannot expose its largest ID. Range exhaustion is never proof of completion.
const windowRecoverySpan = 1000

// Each checkpoint names the next network operation. In particular a successful
// resolver response is committed before its detail request can begin.
type windowCursor struct {
	Version         int      `json:"version"`
	Phase           string   `json:"phase"`
	Scope           int      `json:"scope,omitempty"`
	Page            int      `json:"page,omitempty"`
	Partition       int      `json:"partition,omitempty"`
	Value           int      `json:"value,omitempty"`
	Variant         int      `json:"variant,omitempty"`
	Option          int      `json:"option,omitempty"`
	Options         []string `json:"options,omitempty"`
	Ends            []int    `json:"ends"`
	Totals          []int64  `json:"totals"`
	Recovery        bool     `json:"recovery,omitempty"`
	NextID          int64    `json:"next_id,omitempty"`
	MaxID           int64    `json:"max_id,omitempty"`
	RecheckAfter    int64    `json:"recheck_after,omitempty"`
	RecheckID       int64    `json:"recheck_id,omitempty"`
	Pending         string   `json:"pending,omitempty"`
	Round           int      `json:"round,omitempty"`
	Refresh         []int    `json:"refresh,omitempty"`
	Contracted      []int    `json:"contracted,omitempty"`
	Catchup         bool     `json:"catchup,omitempty"`
	CatchupRound    int      `json:"catchup_round,omitempty"`
	CatchupDeficits []int64  `json:"catchup_deficits,omitempty"`
	Attempt         string   `json:"attempt,omitempty"`
	Generation      int64    `json:"generation,omitempty"`
}

type windowJSONConnector struct {
	provider           model.Provider
	client             *Client
	env                Environment
	unit               string
	resolver           *regexp.Regexp
	checkedFilterQuery string
}

func newWindowJSON(ctx context.Context, p model.Provider, mode model.RunMode, env Environment) (Connector, error) {
	p = providerDefaults(p)
	if err := validateWindowJSON(p); err != nil {
		return nil, err
	}
	ordered := mode == model.ModeIncremental && p.Traversal.IDRecovery != nil && p.Schedule.KnownPages > 0
	if mode == model.ModeMetadata && env.MetadataCandidates == nil {
		return nil, errors.New("JSON metadata collection requires published catalogue candidates")
	}
	if ordered && env.KnownIDs == nil {
		return nil, errors.New("ordered JSON incremental requires prior-run source-ID membership")
	}
	if !ordered && mode != model.ModeMetadata {
		if env.ScopeCounts == nil || env.ObservedIDs == nil {
			return nil, errors.New("bounded JSON traversal requires committed scope counts and source-ID membership")
		}
		if p.Traversal.IDRecovery != nil && env.NextRefreshID == nil {
			return nil, errors.New("numeric JSON recovery requires committed scope refresh candidates")
		}
		if p.Traversal.Options != nil && len(p.Traversal.QueryVariants) > 0 && env.FilterComplete == nil {
			return nil, errors.New("option query variants require committed filter coverage")
		}
	}
	client, err := NewClient(ctx, p, env)
	if err != nil {
		return nil, err
	}
	// Preserve every HTTP response boundary, including quota failures.
	client.returnEveryResponse = true
	unit, _ := publishedAtUnit(p)
	c := &windowJSONConnector{provider: p, client: client, env: env, unit: unit}
	if p.Traversal.IDRecovery != nil {
		c.resolver, _ = regexp.Compile(p.Traversal.IDRecovery.ResolvePattern)
	}
	if mode == model.ModeMetadata {
		return &metadataJSONConnector{windowJSONConnector: c}, nil
	}
	if ordered {
		return &orderedJSONConnector{windowJSONConnector: c}, nil
	}
	return c, nil
}

func (c *windowJSONConnector) Close() error { return c.client.Close() }

func validateWindowJSON(p model.Provider) error {
	t := p.Traversal
	if t == nil {
		return errors.New("bounded JSON traversal configuration is required")
	}
	if !p.IsJSON() || p.HTTP.Catalog || p.HTTP.Method != http.MethodGet || p.HTTP.Body != nil || p.Pagination.Type != "page" || p.Pagination.In != "query" || p.Pagination.NextPath != "" {
		return errors.New("bounded traversal requires native GET JSON with query page pagination and no next_path")
	}
	if t.WindowPages < 1 || p.Pagination.Start < 0 || t.WindowPages > int(^uint(0)>>1)-p.Pagination.Start || p.PageSize < 1 || p.Pagination.PageParam == "" || p.Pagination.SizeParam == "" {
		return errors.New("bounded traversal requires a finite positive page window and page size")
	}
	if len(t.TotalPaths) == 0 || len(t.Scopes) == 0 {
		return errors.New("bounded traversal requires total_paths and scopes")
	}
	if t.MinimumTotal < 0 {
		return errors.New("minimum_total must be nonnegative")
	}
	if t.TotalMode != "" && t.TotalMode != "strict" && t.TotalMode != "at_least" {
		return errors.New("total_mode must be strict or at_least")
	}
	if t.IncrementalOrder != "" && t.IncrementalOrder != "id" && t.IncrementalOrder != "published_at" {
		return errors.New("incremental_order must be id or published_at")
	}
	if t.IncrementalOrder != "" && t.IDRecovery == nil {
		return errors.New("incremental_order requires id_recovery")
	}
	if t.IncrementalOrder == "published_at" {
		if _, mapped := p.Mapping.Fields["published_at"]; !mapped {
			return errors.New("publication-ordered incremental requires mapped published_at")
		}
	}
	for _, path := range t.TotalPaths {
		if path == "" {
			return errors.New("bounded traversal total_paths must select counts")
		}
		if _, err := pointerTokens(path); err != nil {
			return err
		}
	}
	if err := windowMapping(p.Mapping); err != nil {
		return err
	}
	if err := windowQuery(p, p.HTTP.Query); err != nil {
		return err
	}
	base, err := url.Parse(p.URL)
	if err != nil || !validHTTPURL(base) {
		return errors.New("bounded traversal requires an absolute HTTP(S) source URL")
	}
	for name := range base.Query() {
		if err := windowParameter(p, name); err != nil {
			return err
		}
	}
	seen := make(map[string]bool, len(t.Scopes))
	for _, scope := range t.Scopes {
		if !safeIdentifier.MatchString(scope.ID) || seen[scope.ID] || len(scope.Match) == 0 {
			return errors.New("bounded traversal scopes require unique safe IDs and mapped scalar matches")
		}
		seen[scope.ID] = true
		if err := windowQuery(p, scope.Query); err != nil {
			return err
		}
		for field, value := range scope.Match {
			if _, ok := p.Mapping.Fields[field]; !ok || FieldString(value) == "" {
				return errors.New("scope matches must name mapped scalar fields with nonempty scalar values")
			}
		}
	}
	for _, partition := range t.Partitions {
		if err := windowParameter(p, partition.Parameter); err != nil {
			return err
		}
		if _, err := windowPartitionEnd(partition, time.Now().UTC().Year()); err != nil {
			return err
		}
	}
	for _, variant := range t.QueryVariants {
		if len(variant) == 0 {
			return errors.New("query variants must contain query parameters")
		}
		if err := windowQuery(p, variant); err != nil {
			return err
		}
	}
	if options := t.Options; options != nil {
		if err := windowParameter(p, options.QueryParam); err != nil {
			return err
		}
		for _, path := range []string{options.GroupsPath, options.ValuesPath, options.ValuePath, options.PriorityPath} {
			if _, err := pointerTokens(path); err != nil {
				return err
			}
		}
		for _, scope := range t.Scopes {
			if _, err := windowURL(p, options.URL, windowScopeValues(p, scope)); err != nil {
				return err
			}
		}
	}
	if recovery := t.IDRecovery; recovery != nil {
		if recovery.First < 1 || len(recovery.DiscoveryQuery) == 0 {
			return errors.New("ID recovery requires a positive first ID and a discovery query")
		}
		if err := windowQuery(p, recovery.DiscoveryQuery); err != nil {
			return err
		}
		if strings.Count(recovery.ResolveURL, "{id}") != 1 || strings.Count(recovery.DetailURL, "{value}") != 1 {
			return errors.New("ID recovery URLs require exactly one {id} or {value} placeholder respectively")
		}
		if _, err := windowURL(p, recovery.ResolveURL, map[string]string{"id": "1"}); err != nil {
			return err
		}
		if _, err := windowURL(p, recovery.DetailURL, map[string]string{"value": "fixture"}); err != nil {
			return err
		}
		pattern, err := regexp.Compile(recovery.ResolvePattern)
		if err != nil || !strings.HasPrefix(recovery.ResolvePattern, "^") || !strings.HasSuffix(recovery.ResolvePattern, "$") || pattern.MatchString("") {
			return errors.New("resolve_pattern must be a nonempty anchored regular expression")
		}
		if _, err := pointerTokens(recovery.DetailPath); err != nil {
			return err
		}
		if err := windowMapping(recovery.Mapping); err != nil {
			return err
		}
		for _, scope := range t.Scopes {
			for field := range scope.Match {
				if _, ok := recovery.Mapping.Fields[field]; !ok {
					return errors.New("detail mapping must include every scope match field")
				}
			}
		}
	}
	if len(t.EnrichFields) > 0 {
		_, listHash := p.Mapping.Fields["info_hash"]
		if t.IDRecovery == nil || !listHash {
			return errors.New("JSON metadata collection requires ID recovery and mapped listing and detail info_hash")
		}
		if _, detailHash := t.IDRecovery.Mapping.Fields["info_hash"]; !detailHash {
			return errors.New("JSON metadata collection requires ID recovery and mapped listing and detail info_hash")
		}
		fields := make(map[string]bool, len(t.EnrichFields))
		for _, field := range t.EnrichFields {
			_, mapped := t.IDRecovery.Mapping.Fields[field]
			if !safeField.MatchString(field) || fields[field] || !mapped {
				return errors.New("enrich_fields must name unique safe fields present in the detail mapping")
			}
			fields[field] = true
		}
	}
	return nil
}

func windowMapping(mapping model.Mapping) error {
	if mapping.ID == "" {
		return errors.New("bounded traversal requires an explicit source-ID mapping")
	}
	if _, err := pointerTokens(mapping.ID); err != nil {
		return err
	}
	for name, path := range mapping.Fields {
		if !safeField.MatchString(name) {
			return errors.New("mapping field names must be safe identifiers")
		}
		if _, err := pointerTokens(path); err != nil {
			return err
		}
	}
	return nil
}

func windowParameter(p model.Provider, name string) error {
	if !safeField.MatchString(name) || sensitiveName(name) || (p.Auth.Name != "" && strings.EqualFold(name, p.Auth.Name)) {
		return errors.New("traversal query parameters must be safe non-credential names")
	}
	for _, reserved := range []string{p.Pagination.PageParam, p.Pagination.SizeParam, p.Pagination.OffsetParam, p.Pagination.CursorParam} {
		if reserved != "" && strings.EqualFold(name, reserved) {
			return errors.New("traversal queries cannot overwrite pagination parameters")
		}
	}
	return nil
}

func windowQuery(p model.Provider, query map[string]any) error {
	for name, value := range query {
		if err := windowParameter(p, name); err != nil {
			return err
		}
		if _, err := queryValues(value); err != nil {
			return err
		}
	}
	return nil
}

func windowPartitionEnd(partition model.JSONPartition, year int) (int, error) {
	if (partition.End == nil) == (partition.EndYearOffset == nil) {
		return 0, errors.New("partitions require exactly one end or end_year_offset")
	}
	end := 0
	if partition.End != nil {
		end = *partition.End
	} else {
		offset := *partition.EndYearOffset
		if offset > int(^uint(0)>>1)-year {
			return 0, errors.New("partition end_year_offset overflows")
		}
		end = year + offset
	}
	if end < partition.Start {
		return 0, errors.New("partition end must not precede start")
	}
	return end, nil
}

var windowPlaceholder = regexp.MustCompile(`\{([^{}]+)\}`)

// Substitutions are data, never URL syntax; path and query values use their
// respective encodings before the ordinary same-origin client sees the URL.
func windowURL(p model.Provider, template string, values map[string]string) (string, error) {
	if template == "" {
		return "", errors.New("traversal URL template is required")
	}
	parts := strings.SplitN(template, "?", 2)
	missing := false
	for i := range parts {
		parts[i] = windowPlaceholder.ReplaceAllStringFunc(parts[i], func(token string) string {
			value, found := values[token[1:len(token)-1]]
			if !found || value == "" {
				missing = true
			}
			if i == 1 {
				return url.QueryEscape(value)
			}
			return url.PathEscape(value)
		})
	}
	text := strings.Join(parts, "?")
	if missing || strings.ContainsAny(text, "{}") {
		return "", errors.New("traversal URL template contains an unknown or empty placeholder")
	}
	base, _ := url.Parse(p.URL)
	target, err := url.Parse(text)
	if err != nil || base == nil {
		return "", errors.New("invalid traversal URL template")
	}
	if !target.IsAbs() {
		if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") {
			return "", errors.New("traversal URLs must be absolute or root-relative")
		}
		target = base.ResolveReference(target)
	}
	if !sameOrigin(base, target) {
		return "", errors.New("cross-origin traversal URL refused")
	}
	for name := range target.Query() {
		if err := windowParameter(p, name); err != nil {
			return "", err
		}
	}
	return target.String(), nil
}

func windowScopeValues(p model.Provider, scope model.JSONScope) map[string]string {
	values := make(map[string]string, len(p.HTTP.Query)+len(scope.Query))
	for key, value := range p.HTTP.Query {
		values[key] = FieldString(value)
	}
	for key, value := range scope.Query {
		values[key] = FieldString(value)
	}
	return values
}

func (c *windowJSONConnector) initialState() windowCursor {
	t := c.provider.Traversal
	state := windowCursor{Version: 3, Phase: "root", Page: c.provider.Pagination.Start, Ends: make([]int, len(t.Partitions)), Totals: make([]int64, len(t.Scopes))}
	for i := range state.Totals {
		state.Totals[i] = -1
	}
	year := time.Now().UTC().Year()
	for i, partition := range t.Partitions {
		state.Ends[i], _ = windowPartitionEnd(partition, year)
	}
	return state
}

func (c *windowJSONConnector) validState(state windowCursor) bool {
	t := c.provider.Traversal
	if state.Version < 1 || state.Version > 3 || state.Round < 0 || state.Round > 3 || state.CatchupRound < 0 || state.CatchupRound > 3 || state.Scope < 0 || state.Scope > len(t.Scopes) || state.Page < c.provider.Pagination.Start || state.Page-c.provider.Pagination.Start >= t.WindowPages || len(state.Ends) != len(t.Partitions) || len(state.Totals) != len(t.Scopes) || state.Partition < 0 || state.Partition > len(t.Partitions) || state.Variant < -1 || state.Variant > len(t.QueryVariants) || state.Option < 0 || state.Option > len(state.Options) || state.NextID < 0 || state.MaxID < 0 {
		return false
	}
	for _, indexes := range [][]int{state.Refresh, state.Contracted} {
		for _, index := range indexes {
			if index < 0 || index >= len(t.Scopes) {
				return false
			}
		}
	}
	if state.Generation < 0 {
		return false
	}
	if state.RecheckAfter < 0 || state.RecheckAfter > state.MaxID || state.RecheckID < 0 || (state.RecheckID > 0 && (state.RecheckID <= state.RecheckAfter || state.RecheckID > state.MaxID || (state.Phase != "resolve" && state.Phase != "detail"))) {
		return false
	}
	if len(state.CatchupDeficits) != 0 && len(state.CatchupDeficits) != len(t.Scopes) {
		return false
	}
	for _, deficit := range state.CatchupDeficits {
		if deficit < 0 {
			return false
		}
	}
	for i, end := range state.Ends {
		if end < t.Partitions[i].Start || (t.Partitions[i].End != nil && end != *t.Partitions[i].End) {
			return false
		}
	}
	for _, total := range state.Totals {
		if total < -1 || (state.Phase != "root" && total < 0) {
			return false
		}
	}
	if state.Phase == "partition" && state.Partition < len(t.Partitions) && (state.Value < t.Partitions[state.Partition].Start || state.Value > state.Ends[state.Partition]) {
		return false
	}
	if state.Phase == "variant" && state.Variant < 0 {
		return false
	}
	if state.Phase == "options_list" && state.Variant >= len(t.QueryVariants) {
		return false
	}
	switch state.Phase {
	case "root", "partition", "variant", "reconcile", "check", "done":
		return true
	case "options", "options_list":
		return t.Options != nil
	case "discover", "recheck", "resolve", "detail":
		if t.IDRecovery == nil || !state.Recovery {
			return false
		}
		return state.Phase == "discover" || state.Phase == "recheck" || (state.recoveryID() >= int64(t.IDRecovery.First) && state.recoveryID() <= state.MaxID && (state.Phase != "detail" || c.validToken(state.Pending)))
	default:
		return false
	}
}

func windowPage(checkpoint json.RawMessage, state windowCursor) model.Page {
	return model.Page{Next: checkpoint, Position: fmt.Sprintf("%s:%d:%d", state.Phase, state.Scope, state.Page), Metadata: map[string]any{"known_pages_managed": true, "traversal_phase": state.Phase}}
}

func windowFail(page model.Page, err error) (model.Page, error) {
	failure := safeFailure(err, "parse")
	page.Error = failure.Error()
	page.Metadata["failure_code"] = FailureCode(failure)
	return page, failure
}

func windowNext(page model.Page, state windowCursor) (model.Page, error) {
	next, err := json.Marshal(state)
	if err != nil {
		return windowFail(page, errors.New("could not encode traversal continuation"))
	}
	page.Next = next
	page.Done = state.Phase == "done"
	return page, nil
}

func (c *windowJSONConnector) coverage(state windowCursor, counts map[string]int64) bool {
	for i, scope := range c.provider.Traversal.Scopes {
		if state.Totals[i] < 0 || counts[scope.ID] < state.Totals[i] || (c.provider.Traversal.TotalMode != "at_least" && counts[scope.ID] != state.Totals[i]) {
			return false
		}
	}
	return true
}

func (c *windowJSONConnector) resetPartitions(state *windowCursor) {
	state.Partition = 0
	if len(c.provider.Traversal.Partitions) > 0 {
		state.Value = c.provider.Traversal.Partitions[0].Start
	}
}

func (c *windowJSONConnector) reconcile(state *windowCursor) {
	state.Phase, state.Scope, state.Page = "reconcile", 0, c.provider.Pagination.Start
	if c.provider.Traversal.TotalMode == "at_least" {
		state.RecheckAfter = min(state.RecheckAfter, state.MaxID)
		state.Phase = "check"
	}
	state.Options, state.Pending = nil, ""
	state.Option, state.Variant = 0, 0
	state.RecheckID = 0
	state.Catchup = false
}

// seek only changes local state. Its caller either commits that state with one
// real response or emits a bodyless control page for a bounded membership batch.
func (c *windowJSONConnector) seek(state *windowCursor, counts map[string]int64) {
	t := c.provider.Traversal
	for {
		// A refresh enumerates only affected scopes. Root reconciliation
		// still checks every scope for concurrent changes.
		if (t.TotalMode != "at_least" || state.Catchup) && len(state.Refresh) > 0 && state.Scope < len(t.Scopes) && (state.Phase == "root" || state.Phase == "partition" || state.Phase == "variant" || state.Phase == "options" || state.Phase == "options_list") && !slices.Contains(state.Refresh, state.Scope) {
			state.Scope++
			state.Page, state.Option, state.Variant = c.provider.Pagination.Start, 0, 0
			state.Options = nil
			if state.Phase == "options_list" {
				state.Phase = "options"
			}
			c.resetPartitions(state)
			continue
		}
		switch state.Phase {
		case "root":
			if state.Scope < len(t.Scopes) {
				canSkip := state.Catchup && state.Page > c.provider.Pagination.Start
				if t.TotalMode == "at_least" {
					// Even archived coverage must obtain this scope's first target.
					canSkip = state.Totals[state.Scope] >= 0
				}
				if canSkip && counts[t.Scopes[state.Scope].ID] >= state.Totals[state.Scope] {
					state.Scope++
					state.Page = c.provider.Pagination.Start
					continue
				}
				return
			}
			state.Phase, state.Scope = "partition", 0
			c.resetPartitions(state)
			if state.Catchup && t.Options != nil {
				// Metadata filters can reach records absent from year partitions.
				// Try them before repeating those partitions and global sorts.
				state.Phase, state.Variant = "options", 0
			}
		case "partition":
			if len(t.Partitions) == 0 || state.Scope == len(t.Scopes) {
				state.Phase, state.Scope, state.Variant = "variant", 0, 0
				continue
			}
			if counts[t.Scopes[state.Scope].ID] >= state.Totals[state.Scope] || state.Partition == len(t.Partitions) {
				state.Scope++
				state.Page = c.provider.Pagination.Start
				c.resetPartitions(state)
				continue
			}
			return
		case "variant":
			if len(t.QueryVariants) == 0 || state.Scope == len(t.Scopes) {
				if t.Options == nil || state.Catchup {
					c.reconcile(state)
				} else {
					state.Phase, state.Scope, state.Variant = "options", 0, 0
				}
				continue
			}
			if counts[t.Scopes[state.Scope].ID] >= state.Totals[state.Scope] || state.Variant == len(t.QueryVariants) {
				state.Scope++
				state.Page, state.Variant = c.provider.Pagination.Start, 0
				continue
			}
			return
		case "options", "options_list":
			if state.Scope == len(t.Scopes) {
				if state.Catchup {
					state.Phase, state.Scope = "partition", 0
					c.resetPartitions(state)
				} else {
					c.reconcile(state)
				}
				continue
			}
			if counts[t.Scopes[state.Scope].ID] >= state.Totals[state.Scope] || (state.Phase == "options_list" && state.Option == len(state.Options)) {
				state.Scope++
				state.Phase, state.Page, state.Option, state.Variant = "options", c.provider.Pagination.Start, 0, 0
				state.Options = nil
				continue
			}
			return
		case "reconcile":
			if t.TotalMode == "at_least" || state.Scope == len(t.Scopes) {
				state.Phase = "check"
			}
			return
		case "discover":
			if t.TotalMode == "at_least" && c.coverage(*state, counts) {
				c.reconcile(state)
				continue
			}
			return
		case "resolve", "recheck":
			if c.coverage(*state, counts) {
				if state.Phase == "resolve" && state.RecheckID == 0 {
					// A fresh root can grow; do not skip the unvisited tail.
					state.MaxID = state.NextID - 1
				}
				c.reconcile(state)
				continue
			}
			return
		default:
			return
		}
	}
}

func (c *windowJSONConnector) Fetch(ctx context.Context, checkpoint json.RawMessage) (model.Page, error) {
	var state windowCursor
	if len(checkpoint) == 0 {
		state = c.initialState()
	} else {
		if err := decodeJSON(checkpoint, &state); err != nil || !c.validState(state) {
			return model.Page{}, errors.New("invalid bounded JSON continuation state")
		}
	}
	if state.Recovery && state.NextID > 0 && state.NextID < state.MaxID && (state.Phase == "reconcile" || state.Phase == "check" || state.Phase == "discover") {
		// Older checkpoints kept the unvisited range end after an early
		// coverage match. Do not skip that tail if refreshed totals grow.
		state.MaxID = state.NextID - 1
		if c.provider.Traversal.TotalMode == "at_least" {
			state.RecheckAfter = min(state.RecheckAfter, state.MaxID)
		}
	}
	if state.Version < 3 && state.Phase != "detail" {
		// Finish an already-resolved detail before upgrading its checkpoint.
		// Older recovery loops never tried additive catch-up; recheck roots
		// once and keep only the completed portion of an active numeric range.
		if state.Phase == "resolve" && state.RecheckID == 0 {
			state.MaxID = state.NextID - 1
		}
		state.Version = 3
		if state.Recovery {
			state.Round = 0
		}
		if state.Recovery || state.Phase == "reconcile" || state.Phase == "check" {
			c.reconcile(&state)
		}
	}
	if c.env.CoverageAttempt != "" && state.Attempt != c.env.CoverageAttempt {
		state.Attempt = c.env.CoverageAttempt
		state.Generation++
		if state.Generation <= 0 {
			return model.Page{}, errors.New("bounded JSON continuation generation exhausted")
		}
		state.Round = 0
		state.CatchupRound, state.CatchupDeficits = 0, nil
		if state.Phase == "check" || state.Phase == "reconcile" {
			state.Contracted, state.Refresh = nil, nil
			state.RecheckAfter = 0
			c.reconcile(&state)
		}
	}
	page := windowPage(checkpoint, state)
	if state.Phase == "done" {
		page.Done = true
		return page, nil
	}
	counts, err := c.env.ScopeCounts(ctx)
	if err != nil {
		return windowFail(page, err)
	}
	// Old checkpoints may already be scanning numeric IDs for a category whose
	// whole response fits on one page. Recheck that response before continuing
	// the scan; an upstream reclassification can make its old target impossible.
	if c.provider.Traversal.TotalMode == "at_least" && state.Recovery && state.CatchupRound == 0 && (state.Phase == "resolve" || state.Phase == "recheck" || state.Phase == "discover") {
		for i, scope := range c.provider.Traversal.Scopes {
			if state.Totals[i] <= int64(c.provider.PageSize) && counts[scope.ID] < state.Totals[i] {
				c.reconcile(&state)
				break
			}
		}
	}
	c.seek(&state, counts)
	page = windowPage(checkpoint, state)
	if state.Phase == "check" {
		expected, observed := make(map[string]int64), make(map[string]int64)
		atLeast := c.provider.Traversal.TotalMode == "at_least"
		// A resumed root sweep may carry an unfinished numeric range. Only
		// its visited prefix may seed the next bounded discovery range.
		if atLeast && state.Recovery && state.NextID > 0 && state.NextID < state.MaxID {
			state.MaxID = state.NextID - 1
			state.RecheckAfter = min(state.RecheckAfter, state.MaxID)
		}
		complete := c.coverage(state, counts)
		var invalidated []int
		if !atLeast {
			invalidated = append(invalidated, state.Contracted...)
		}
		// Strict reconciliation can repeat affected listing views. at_least
		// keeps accepted evidence and checks small roots before numeric recovery.
		var missing []int
		minimumRemaining := int64(c.provider.Traversal.MinimumTotal)
		for i, scope := range c.provider.Traversal.Scopes {
			expected[scope.ID], observed[scope.ID] = state.Totals[i], counts[scope.ID]
			if !atLeast && observed[scope.ID] > expected[scope.ID] && !slices.Contains(invalidated, i) {
				invalidated = append(invalidated, i)
			}
			if observed[scope.ID] < expected[scope.ID] && (!atLeast || (c.provider.Traversal.IDRecovery != nil && expected[scope.ID] <= int64(c.provider.PageSize))) {
				missing = append(missing, i)
			}
			if minimumRemaining > 0 {
				if expected[scope.ID] >= minimumRemaining {
					minimumRemaining = 0
				} else {
					minimumRemaining -= expected[scope.ID]
				}
			}
		}
		page.Metadata["coverage_expected"], page.Metadata["coverage_observed"] = expected, observed
		if minimumRemaining > 0 {
			page.Metadata["minimum_total"] = c.provider.Traversal.MinimumTotal
			page.Metadata["coverage_incomplete"] = true
			return windowFail(page, fmt.Errorf("%w: root totals are below the configured visibility minimum", model.ErrStalled))
		}
		if complete && len(invalidated) == 0 {
			state.Phase = "done"
			return windowNext(page, state)
		}
		enumerate := invalidated
		catchup := len(invalidated) == 0
		if catchup {
			for _, index := range missing {
				gap := state.Totals[index] - counts[c.provider.Traversal.Scopes[index].ID]
				if c.provider.Traversal.IDRecovery == nil || len(state.CatchupDeficits) == 0 || state.CatchupDeficits[index] == 0 || gap < state.CatchupDeficits[index] {
					enumerate = append(enumerate, index)
				}
			}
		}
		roundAvailable := state.Round < 3
		if catchup {
			roundAvailable = state.CatchupRound < 3
		}
		if len(enumerate) > 0 && roundAvailable {
			// Contraction/excess retires stale evidence. A deficit instead
			// gets a bounded additive pass before numeric recovery, including
			// filters skipped when an older root total appeared satisfied.
			state.Catchup = catchup
			if catchup {
				state.CatchupRound++
				if len(state.CatchupDeficits) == 0 {
					state.CatchupDeficits = make([]int64, len(state.Totals))
				}
				for _, index := range enumerate {
					state.CatchupDeficits[index] = state.Totals[index] - counts[c.provider.Traversal.Scopes[index].ID]
				}
				page.Metadata["coverage_catchup_round"] = state.CatchupRound
			} else {
				state.Round++
			}
			state.Refresh, state.Contracted = enumerate, nil
			state.Phase, state.Scope, state.Page = "root", 0, c.provider.Pagination.Start
			// Re-enumeration invalidates only these scopes, not the numeric
			// frontier. Revisit their older candidates separately.
			if !catchup {
				state.RecheckAfter, state.RecheckID = 0, 0
			}
			state.Options, state.Pending = nil, ""
			state.Option, state.Variant = 0, 0
			c.resetPartitions(&state)
			var catchupScopes []string
			for _, index := range enumerate {
				id := c.provider.Traversal.Scopes[index].ID
				if slices.Contains(invalidated, index) {
					page.RefreshScopes = append(page.RefreshScopes, id)
				} else {
					catchupScopes = append(catchupScopes, id)
				}
			}
			if len(page.RefreshScopes) > 0 {
				page.Metadata["coverage_refresh_scopes"] = page.RefreshScopes
			}
			if len(catchupScopes) > 0 {
				page.Metadata["coverage_catchup_scopes"] = catchupScopes
			}
			page.Metadata["coverage_round"] = state.Round
			return windowNext(page, state)
		}
		coverageMismatch := "source-ID coverage differs from refreshed root totals"
		if atLeast {
			coverageMismatch = "source-ID coverage is below the initial root targets"
		}
		if len(invalidated) > 0 || c.provider.Traversal.IDRecovery == nil {
			page.Metadata["coverage_incomplete"] = true
			return windowFail(page, fmt.Errorf("%w: %s", model.ErrStalled, coverageMismatch))
		}
		if state.Recovery && state.RecheckAfter < state.MaxID {
			state.Phase = "recheck"
			return windowNext(page, state)
		}
		if state.Recovery && state.MaxID == math.MaxInt64 {
			page.Metadata["coverage_incomplete"] = true
			return windowFail(page, fmt.Errorf("%w: %s", model.ErrStalled, coverageMismatch))
		}
		// Numeric progress is not a retry of the same reconciliation. A
		// listing maximum is only a hint: missing IDs can lie beyond it,
		// even when root totals are unchanged and refresh rounds are spent.
		state.Phase, state.Recovery = "discover", true
		return windowNext(page, state)
	}
	if state.Phase == "recheck" {
		after := max(state.RecheckAfter, int64(c.provider.Traversal.IDRecovery.First)-1)
		id, err := c.env.NextRefreshID(ctx, after, state.MaxID)
		if err != nil {
			return windowFail(page, err)
		}
		if id == 0 {
			state.RecheckAfter, state.Phase = state.MaxID, "check"
			return windowNext(page, state)
		}
		if id <= after || id > state.MaxID {
			return windowFail(page, errors.New("invalid scope refresh identity"))
		}
		state.RecheckID, state.Phase = id, "resolve"
	}
	if state.Phase == "resolve" && state.RecheckID == 0 {
		// This fixed-size lookup never becomes a checkpoint-sized ID collection.
		const batchSize = 100
		n := state.MaxID - state.NextID + 1
		if n > batchSize {
			n = batchSize
		}
		ids := make([]string, int(n))
		for i := range ids {
			ids[i] = strconv.FormatInt(state.NextID+int64(i), 10)
		}
		known, err := c.env.ObservedIDs(ctx, ids)
		if err != nil {
			return windowFail(page, err)
		}
		skipped := 0
		for _, id := range ids {
			if !known[id] {
				break
			}
			skipped++
			c.advanceID(&state)
			if state.Phase != "resolve" {
				break
			}
		}
		if state.Phase != "resolve" || skipped == len(ids) {
			return windowNext(page, state)
		}
	}
	request, err := c.request(state)
	if err != nil {
		return windowFail(page, err)
	}
	fingerprint := c.fingerprint(state, request)
	if state.Phase == "options_list" && c.env.FilterComplete != nil {
		complete, err := c.filterComplete(ctx, state, fingerprint)
		if err != nil {
			return windowFail(page, err)
		}
		if complete {
			page.Metadata["coverage_option_complete"] = state.Options[state.Option]
			page.Metadata["coverage_option_scope"] = c.provider.Traversal.Scopes[state.Scope].ID
			state.Option++
			state.Variant, state.Page = -1, c.provider.Pagination.Start
			return windowNext(page, state)
		}
	}
	if state.Phase == "resolve" || state.Phase == "detail" {
		page.Position = state.Phase + ":" + strconv.FormatInt(state.recoveryID(), 10)
		page.Metadata["recovery_id"] = state.recoveryID()
		if state.RecheckID > 0 {
			page.Metadata["recovery_recheck"] = true
		}
	}
	response, requestErr := c.client.Do(ctx, request)
	page.Body, page.ContentType = response.Body, response.Header.Get("Content-Type")
	page.Metadata["http_status"] = response.StatusCode
	page.Metadata["fingerprint_scope"] = fingerprint
	if state.Phase == "options" || state.Phase == "resolve" {
		// Discovery metadata and ID resolution do not return catalogue records.
		page.Metadata["auxiliary_response"] = true
	}
	// Not-found responses are holes only for the numeric recovery resources,
	// and only when the failure really is the HTTP status (not a body read).
	hole := (state.Phase == "resolve" || state.Phase == "detail") && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone) && FailureCode(requestErr) == "http"
	if requestErr != nil && !hole {
		return windowFail(page, requestErr)
	}
	if hole {
		page.Metadata["recovery_hole"] = true
		c.advanceID(&state)
		return windowNext(page, state)
	}
	if state.Phase != "resolve" && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return windowFail(page, httpFailure(response.StatusCode))
	}
	if state.Phase == "resolve" && response.StatusCode >= 200 && response.StatusCode < 300 && response.Header.Get("Location") == "" {
		page.Metadata["recovery_unresolved"] = true
		c.advanceID(&state)
		return windowNext(page, state)
	}
	switch state.Phase {
	case "root", "partition", "variant", "options_list", "reconcile", "discover":
		err = c.list(&page, &state, response.Body)
	case "options":
		err = c.options(&state, response.Body)
	case "resolve":
		err = c.resolve(&state, request, response)
	case "detail":
		recovery := c.provider.Traversal.IDRecovery
		var root any
		if err := decodeJSON(response.Body, &root); err != nil {
			return windowFail(page, err)
		}
		var raw json.RawMessage
		var found bool
		raw, found, err = rawPointer(response.Body, recovery.DetailPath)
		if err == nil && !found {
			err = errors.New("detail_path is missing")
		}
		if err == nil {
			page.Items = []model.Record{c.record(raw, recovery.Mapping)}
			c.advanceID(&state)
		}
	default:
		err = errors.New("invalid traversal phase")
	}
	if err != nil {
		return windowFail(page, err)
	}
	return windowNext(page, state)
}

func (c *windowJSONConnector) request(state windowCursor) (Request, error) {
	p, t := c.provider, c.provider.Traversal
	request := Request{Method: http.MethodGet, Headers: http.Header{"Accept": {"application/json"}}, DisableRedirects: true}
	switch state.Phase {
	case "options":
		value, err := windowURL(p, t.Options.URL, windowScopeValues(p, t.Scopes[state.Scope]))
		request.URL = value
		return request, err
	case "resolve":
		value, err := windowURL(p, t.IDRecovery.ResolveURL, map[string]string{"id": strconv.FormatInt(state.recoveryID(), 10)})
		request.Method, request.URL = http.MethodHead, value
		return request, err
	case "detail":
		value, err := windowURL(p, t.IDRecovery.DetailURL, map[string]string{"value": state.Pending})
		request.URL = value
		return request, err
	}
	request.Query = make(url.Values)
	merge := func(values map[string]any) {
		for name, value := range values {
			request.Query[name], _ = queryValues(value)
		}
	}
	merge(p.HTTP.Query)
	if state.Phase == "discover" {
		merge(t.IDRecovery.DiscoveryQuery)
	} else {
		merge(t.Scopes[state.Scope].Query)
	}
	switch state.Phase {
	case "partition":
		request.Query.Set(t.Partitions[state.Partition].Parameter, strconv.Itoa(state.Value))
	case "variant":
		merge(t.QueryVariants[state.Variant])
	case "options_list":
		if state.Variant >= 0 {
			merge(t.QueryVariants[state.Variant])
		}
		request.Query.Set(t.Options.QueryParam, state.Options[state.Option])
	}
	request.Query.Set(p.Pagination.PageParam, strconv.Itoa(state.Page))
	request.Query.Set(p.Pagination.SizeParam, strconv.Itoa(p.PageSize))
	return request, nil
}

// Consult archived, committed observations once per query, including the first
// fetch after a restart. Fingerprints remain compatible with older checkpoints;
// completeness is about distinct native IDs, not page lengths or content hashes.
func (c *windowJSONConnector) filterComplete(ctx context.Context, state windowCursor, current string) (bool, error) {
	if current == c.checkedFilterQuery {
		return false, nil
	}
	fingerprints := make([]string, state.Variant+2)
	fingerprints[len(fingerprints)-1] = current
	for variant := -1; variant < state.Variant; variant++ {
		previous := state
		previous.Variant = variant
		request, err := c.request(previous)
		if err != nil {
			return false, err
		}
		fingerprints[variant+1] = c.fingerprint(previous, request)
	}
	complete, err := c.env.FilterComplete(ctx, c.provider.Traversal.Scopes[state.Scope].ID, fingerprints)
	if err == nil && !complete {
		c.checkedFilterQuery = current
	}
	return complete, err
}

func (c *windowJSONConnector) fingerprint(state windowCursor, request Request) string {
	query := make(url.Values, len(request.Query))
	for name, values := range request.Query {
		if name != c.provider.Pagination.PageParam && name != c.provider.Pagination.SizeParam {
			query[name] = values
		}
	}
	// Scope indices and phase distinguish overlapping complete queries, while
	// all pages of one query share the repeated-page boundary.
	identity := fmt.Sprintf("%d:%d:%d:%d:%s:%t:%d:%d:%d:%d:%d:%d:%d:%d:%s:%s", state.Version, state.Generation, state.Round, state.CatchupRound, state.Phase, state.Recovery, state.Scope, state.Partition, state.Value, state.Variant, state.Option, state.NextID, state.RecheckID, state.RecheckAfter, request.URL, query.Encode())
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func windowTotal(root any, paths []string) (int64, error) {
	for _, path := range paths {
		value, found, err := pointerValue(root, path)
		if err != nil {
			return 0, err
		}
		if !found {
			continue
		}
		total, err := integerValue(value)
		if err != nil || total < 0 {
			return 0, errors.New("first present total path must select a nonnegative integer")
		}
		return total, nil
	}
	return 0, errors.New("no configured total path is present")
}

func (c *windowJSONConnector) record(raw json.RawMessage, mapping model.Mapping) model.Record {
	id, fields, err := FieldsFromJSON(raw, mapping, c.unit)
	record := model.Record{SourceID: id, Fields: fields, Raw: raw, ContentType: "application/json"}
	if err != nil {
		record.Error = err.Error()
	} else {
		// An explicit empty set is valid negative coverage, unlike nil
		// (unspecified or invalid interpretation).
		record.CoverageScopes = []string{}
	}
	matched := false
	for _, scope := range c.provider.Traversal.Scopes {
		matches := true
		for field, expected := range scope.Match {
			if FieldString(fields[field]) != FieldString(expected) {
				matches = false
				break
			}
		}
		if matches {
			matched = true
			if err == nil {
				record.CoverageScopes = append(record.CoverageScopes, scope.ID)
			}
		}
	}
	record.Ignored = !matched
	return record
}

func (c *windowJSONConnector) list(page *model.Page, state *windowCursor, body []byte) error {
	p, t := c.provider, c.provider.Traversal
	var root any
	if err := decodeJSON(body, &root); err != nil {
		return err
	}
	selected, found, err := rawPointer(body, p.HTTP.ItemsPath)
	if err != nil {
		return err
	}
	if !found || len(bytes.TrimSpace(selected)) == 0 || bytes.TrimSpace(selected)[0] != '[' {
		return errors.New("items_path must select a JSON array")
	}
	var items []json.RawMessage
	if err := decodeJSON(selected, &items); err != nil {
		return err
	}
	page.Items = make([]model.Record, 0, len(items))
	for _, raw := range items {
		page.Items = append(page.Items, c.record(raw, p.Mapping))
	}
	total, err := windowTotal(root, t.TotalPaths)
	if err != nil {
		return err
	}
	page.Metadata["total"] = total
	if p.Pagination.CurrentPath != "" {
		value, err := requiredPointer(root, p.Pagination.CurrentPath)
		if err != nil {
			return err
		}
		current, err := boundedCount(value)
		if err != nil || current != state.Page {
			return fmt.Errorf("%w: returned position differs from requested page", model.ErrStalled)
		}
	}
	if len(items) > p.PageSize {
		return errors.New("listing exceeds the configured page size")
	}
	if state.Phase == "discover" {
		if len(items) == 0 {
			page.Metadata["recovery_discovery_error"] = "empty"
			return errors.New("ID recovery discovery returned no source IDs")
		}
		var maximum int64
		for _, record := range page.Items {
			id, err := strconv.ParseInt(record.SourceID, 10, 64)
			if err != nil || id < 1 || strconv.FormatInt(id, 10) != record.SourceID {
				page.Metadata["recovery_discovery_error"] = "noncanonical_id"
				return errors.New("ID recovery discovery requires canonical positive numeric source IDs")
			}
			maximum = max(maximum, id)
		}
		if state.MaxID == math.MaxInt64 {
			// Commit discovery observations before the final coverage check.
			c.reconcile(state)
			return nil
		}
		first := max(int64(t.IDRecovery.First), state.MaxID+1)
		last := first + min(int64(windowRecoverySpan-1), math.MaxInt64-first)
		if maximum >= first {
			last = min(last, maximum)
		}
		state.MaxID, state.NextID, state.Phase = last, first, "resolve"
		return nil
	}
	if (state.Phase == "reconcile" || (state.Phase == "root" && state.Page == p.Pagination.Start)) && (t.TotalMode != "at_least" || state.Totals[state.Scope] < 0) {
		if state.Totals[state.Scope] >= 0 && total < state.Totals[state.Scope] && !slices.Contains(state.Contracted, state.Scope) {
			state.Contracted = append(state.Contracted, state.Scope)
		}
		state.Totals[state.Scope] = total
	}
	if t.TotalMode == "at_least" && state.Phase == "root" && state.Page == p.Pagination.Start {
		if target, complete := c.completeRootTarget(page.Items, total, t.Scopes[state.Scope]); complete && target < state.Totals[state.Scope] {
			page.Metadata["coverage_scope"] = t.Scopes[state.Scope].ID
			page.Metadata["coverage_advertised_total"] = total
			page.Metadata["coverage_previous_target"] = state.Totals[state.Scope]
			page.Metadata["coverage_target"] = target
			state.Totals[state.Scope] = target
		}
	}
	if state.Phase == "reconcile" {
		state.Scope++
		return nil
	}
	pages := state.Page - p.Pagination.Start + 1
	// A capped or short page ends this query, not the overall traversal. Totals
	// may legitimately change between requests on a live listing.
	atTotal := total == 0 || int64(pages) >= (total-1)/int64(p.PageSize)+1
	if pages == t.WindowPages || len(items) < p.PageSize || atTotal {
		state.Page = p.Pagination.Start
		switch state.Phase {
		case "root":
			state.Scope++
		case "partition":
			if state.Value == state.Ends[state.Partition] {
				state.Partition++
				if state.Partition < len(t.Partitions) {
					state.Value = t.Partitions[state.Partition].Start
				}
			} else {
				state.Value++
			}
		case "variant":
			state.Variant++
		case "options_list":
			state.Variant++
			if state.Variant == len(t.QueryVariants) {
				state.Variant = -1
				state.Option++
			}
		}
	} else {
		state.Page++
	}
	return nil
}

// A capped/short listing is not proof. Only an entire advertised root result,
// with distinct valid native IDs and explicit classification fields, can lower
// a cumulative target. Records in another scope still retain their own identity.
func (c *windowJSONConnector) completeRootTarget(records []model.Record, total int64, scope model.JSONScope) (int64, bool) {
	if total > int64(c.provider.PageSize) || int64(len(records)) != total {
		return 0, false
	}
	seen := make(map[string]bool, len(records))
	var matched int64
	for _, record := range records {
		if record.Error != "" || record.SourceID == "" || record.Auxiliary || record.Ignored || seen[record.SourceID] || len(record.CoverageScopes) == 0 {
			return 0, false
		}
		seen[record.SourceID] = true
		for field := range scope.Match {
			if FieldString(record.Fields[field]) == "" {
				return 0, false
			}
		}
		if slices.Contains(record.CoverageScopes, scope.ID) {
			matched++
		}
	}
	return matched, true
}

func (c *windowJSONConnector) options(state *windowCursor, body []byte) error {
	var root any
	if err := decodeJSON(body, &root); err != nil {
		return err
	}
	config := c.provider.Traversal.Options
	value, err := requiredPointer(root, config.GroupsPath)
	if err != nil {
		return err
	}
	groups, ok := value.([]any)
	if !ok {
		return errors.New("options groups_path must select an array")
	}
	if config.PriorityPath != "" {
		ordered := make([]any, 0, len(groups))
		for _, priority := range []bool{true, false} {
			for _, group := range groups {
				value, _, err := pointerValue(group, config.PriorityPath)
				if err != nil {
					return err
				}
				if windowTruthy(value) == priority {
					ordered = append(ordered, group)
				}
			}
		}
		groups = ordered
	}
	values := make([]string, 0)
	seen := make(map[string]bool)
	// Option catalogs may also describe free-text controls and headings.
	// Only enumerated values with scalar identities can partition a search.
	for _, group := range groups {
		selected, _, err := pointerValue(group, config.ValuesPath)
		if err != nil {
			return err
		}
		entries, ok := selected.([]any)
		if !ok {
			continue
		}
		for _, entry := range entries {
			selected, _, err := pointerValue(entry, config.ValuePath)
			if err != nil {
				return err
			}
			text := FieldString(selected)
			if text == "" {
				continue
			}
			if !seen[text] {
				seen[text] = true
				values = append(values, text)
			}
		}
	}
	state.Options, state.Option, state.Variant, state.Phase = values, 0, -1, "options_list"
	return nil
}

func (c *windowJSONConnector) validToken(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\r\n\x00") || c.resolver == nil {
		return false
	}
	match := c.resolver.FindStringIndex(value)
	return match != nil && match[0] == 0 && match[1] == len(value)
}

func (c *windowJSONConnector) resolve(state *windowCursor, request Request, response Response) error {
	location := response.Header.Get("Location")
	if location == "" {
		return errors.New("ID resolver response has no Location")
	}
	base, err := c.client.target(request.URL)
	if err != nil {
		return err
	}
	reference, err := url.Parse(location)
	if err != nil {
		return errors.New("invalid resolver Location")
	}
	target := base.ResolveReference(reference)
	if !sameOrigin(c.client.origin, target) {
		return errors.New("cross-origin resolver Location refused")
	}
	path := strings.TrimRight(target.EscapedPath(), "/")
	last := path[strings.LastIndex(path, "/")+1:]
	value, err := url.PathUnescape(last)
	if err != nil || !c.validToken(value) {
		return errors.New("resolver Location does not match resolve_pattern")
	}
	state.Pending, state.Phase = value, "detail"
	return nil
}

func (state windowCursor) recoveryID() int64 {
	if state.RecheckID > 0 {
		return state.RecheckID
	}
	return state.NextID
}

func (c *windowJSONConnector) advanceID(state *windowCursor) {
	state.Pending = ""
	if state.RecheckID > 0 {
		state.RecheckAfter, state.RecheckID, state.Phase = state.RecheckID, 0, "recheck"
		return
	}
	// The three-round guard bounds reconciliation without forward progress,
	// not unrelated catalogue changes during a long numeric scan.
	state.Round = 0
	if state.NextID == state.MaxID || (state.NextID-int64(c.provider.Traversal.IDRecovery.First)+1)%windowRecoverySpan == 0 {
		// Also bound the next root refresh when resuming older checkpoints
		// whose scan limit covered the entire observed ID space.
		state.MaxID = state.NextID
		c.reconcile(state)
	} else {
		state.NextID++
		state.Phase = "resolve"
	}
}

func windowTruthy(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case json.Number:
		number, err := value.Float64()
		return err == nil && number != 0
	case []any:
		return len(value) != 0
	case map[string]any:
		return len(value) != 0
	default:
		return false
	}
}
