package connectors

import (
	"encoding/json"
	"math"
)

// FailureReasonMessage accepts only connector-owned reason codes. Never use an
// upstream error string as a reason, even when the response was HTTP 200.
func FailureReasonMessage(reason string) string {
	switch reason {
	case "invalid_position":
		return "source pagination position is missing or is not a nonnegative integer"
	case "position_mismatch":
		return "source returned a different pagination position than requested"
	case "invalid_total":
		return "source pagination total is missing or is not a nonnegative integer"
	case "total_changed":
		return "source pagination total changed beyond this run's allowed policy"
	case "records_exceed_total":
		return "source records extend beyond the advertised total"
	case "empty_before_total":
		return "source returned an empty page before the advertised total"
	case "invalid_continuation":
		return "source continuation is missing, invalid or outside the allowed origin"
	case "continuation_repeated":
		return "source repeated its previous continuation"
	case "continuation_mismatch":
		return "source next position skips or repeats records"
	case "premature_end":
		return "source pagination ended before the advertised total"
	case "unexpected_continuation":
		return "source continuation contradicts page completion or disabled pagination"
	case "pagination_overflow":
		return "source pagination position exceeds the supported integer range"
	case "publication_order_invalid":
		return "source publication dates are not in descending order"
	default:
		return ""
	}
}

var diagnosticCountKeys = [...]string{"expected_total", "actual_total", "expected_position", "actual_position", "offset", "page", "requested_page", "record_index", "http_status"}

// SanitizeFailureDiagnostics removes unsafe values in the diagnostic namespace
// without changing unrelated page metadata or historical event contracts.
func SanitizeFailureDiagnostics(data map[string]any) {
	if reason, ok := data["failure_reason"].(string); !ok || FailureReasonMessage(reason) == "" {
		delete(data, "failure_reason")
	}
	for _, key := range diagnosticCountKeys {
		if value, ok := diagnosticInteger(data[key]); !ok || key == "http_status" && (value < 100 || value > 599) {
			delete(data, key)
		}
	}
}

// CopyFailureDiagnostics is the allowlist shared by retained events and public
// responses. Counts must be exact in both Go and the browser; strings, fractions,
// negative values and out-of-range integers are deliberately omitted.
func CopyFailureDiagnostics(dst, src map[string]any) {
	if reason, ok := src["failure_reason"].(string); ok && FailureReasonMessage(reason) != "" {
		dst["failure_reason"] = reason
	}
	for _, key := range diagnosticCountKeys {
		if value, ok := diagnosticInteger(src[key]); ok {
			if key != "http_status" || value >= 100 && value <= 599 {
				dst[key] = value
			}
		}
	}
}

func diagnosticInteger(value any) (int64, bool) {
	const maxSafe = int64(1<<53 - 1)
	var count int64
	switch value := value.(type) {
	case int:
		count = int64(value)
	case int64:
		count = value
	case json.Number:
		var err error
		count, err = value.Int64()
		if err != nil {
			return 0, false
		}
	case float64:
		if math.IsNaN(value) || value < 0 || value > float64(maxSafe) || math.Trunc(value) != value {
			return 0, false
		}
		count = int64(value)
	default:
		return 0, false
	}
	return count, count >= 0 && count <= maxSafe
}
