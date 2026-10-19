// Package connectors implements protocol-specific, resumable metadata readers.
package connectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
	"github.com/theo-bnts/aiosources/internal/outbound"
)

// Connector returns complete pages and explicit opaque continuation state.
// Close releases transports; it must not discard persisted continuation state.
type Connector interface {
	Fetch(context.Context, json.RawMessage) (model.Page, error)
	Close() error
}

type Environment struct {
	Network         *outbound.Router
	Secrets         model.SecretResolver
	LocalInstanceID string
	// Nil preserves the standalone limit of three automatic 429 retries.
	MaxQuotaRetries *int
	// RateLimited retains every 429, including the final exhausted response.
	// retryAt is zero when the source supplied no deadline; retry reports
	// whether the transport can make another automatic attempt.
	RateLimited func(ctx context.Context, response Response, retryAt time.Time, retry bool) error
	// TransientFailure retains complete GET/HEAD responses with HTTP 500, 502,
	// 503 or 504, including the final exhausted response. attempt is the
	// one-based network attempt; retry reports whether another attempt is
	// allowed. retryAt includes pacing and known quota deadlines, or is zero
	// when an exhausted quota has no known reset. Callback errors stop retries.
	TransientFailure func(ctx context.Context, response Response, retryAt time.Time, attempt int, retry bool) error
	// ResponseObserved reports transport-owned quota deadlines without archiving.
	// Checkpoint commits an intermediate decoded response before another request.
	ResponseObserved func(response Response, retryAt time.Time)
	Checkpoint       func(context.Context, model.Page) error
	// BeforeWait may stop a request at its safe pre-network boundary. The
	// transport still enforces the full source interval and quota deadline.
	BeforeWait func(context.Context, time.Time) error
	// BeforeRequest durably admits each primary-origin network attempt. It may
	// wait for a rolling quota; request timeouts start only after admission.
	BeforeRequest func(context.Context) error
	// Baseline membership predates this run; current-run writes cannot satisfy
	// an incremental stopping boundary.
	KnownIDs func(context.Context, []string) (map[string]bool, error)
	// MetadataCandidates walks missing metadata in the published catalogue,
	// ordered by native source ID after the committed cursor. Limit is at most 100.
	MetadataCandidates func(ctx context.Context, after string, limit int) ([]model.MetadataCandidate, error)
	// Traversals consult committed run-local evidence, never prior live rows.
	// These callbacks are required only when Provider.Traversal is configured.
	ScopeCounts func(context.Context) (map[string]int64, error)
	ObservedIDs func(context.Context, []string) (map[string]bool, error)
	// FilterComplete proves option coverage from committed native identities
	// across the supplied query fingerprints. Required with option variants.
	FilterComplete func(ctx context.Context, scope string, fingerprints []string) (bool, error)
	// Numeric recovery revisits invalidated identities in (after, through].
	// Returns the smallest candidate, or zero; required with IDRecovery.
	NextRefreshID func(ctx context.Context, after, through int64) (int64, error)
	// Changes only on an explicit Resume after failure, not budget pauses.
	// This renews bounded reconciliation without recycling fingerprints.
	CoverageAttempt string
}

// Response contains bytes exactly as received after HTTP transfer decoding.
// Headers stay internal: they are not copied into event logs or API errors.
type Response struct {
	Body       []byte
	StatusCode int
	Header     http.Header
	// Final request address for resolving relative HTML links after redirects.
	// Internal only: like Header, this must never be copied into diagnostics.
	URL string
	// Set only for an invalid HTML response to a JSON GET after HTTP 429.
	UnexpectedHTML bool
}

func (r Response) TransientError(attempt int) error {
	code := "http"
	if r.UnexpectedHTML {
		code = "parse"
	}
	return &transientHTTPFailure{failure: FailureError{code: code}, status: r.StatusCode, attempt: attempt, unexpectedHTML: r.UnexpectedHTML}
}

// Request is deliberately data-only, never a script or executable template.
type Request struct {
	Method           string
	URL              string
	Query            url.Values
	Body             []byte
	Headers          http.Header
	DisableRedirects bool
}
