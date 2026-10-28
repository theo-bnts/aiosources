package model

import "fmt"

// Service diagnostics contain only fixed categories and HTTP status codes.
// Remote response bodies, URLs and credential values must never enter activity.
type ServiceDiagnostic struct {
	Operation   string
	FailureCode string
	StatusCode  int
}

func ServiceName(service string) string {
	switch service {
	case "tmdb":
		return "TMDB"
	case "alldebrid", "cleanup":
		return "AllDebrid"
	case "torbox":
		return "TorBox"
	default:
		for _, provider := range DebridProviders() {
			if provider.ID == service {
				return provider.Name
			}
		}
		return ""
	}
}

func (d ServiceDiagnostic) Valid() bool {
	switch d.Operation {
	case "configuration", "changes", "inventory", "details", "identification", "storage":
	default:
		return false
	}
	switch d.FailureCode {
	case "", "authentication", "network", "timeout", "http", "parse", "storage", "change_gap", "cooldown", "interrupted", "unknown":
	default:
		return false
	}
	return d.StatusCode == 0 || (d.StatusCode >= 100 && d.StatusCode <= 599)
}

func (d ServiceDiagnostic) Message() string {
	if d.FailureCode == "" {
		return "Information update completed."
	}
	if d.FailureCode == "interrupted" {
		return "Information update interrupted. Committed progress is preserved."
	}
	return "Information update failed while " + d.failureDescription() + ". Committed progress is preserved."
}

func (d ServiceDiagnostic) failureDescription() string {
	stage := map[string]string{
		"configuration":  "reading the TMDB configuration",
		"changes":        "reading TMDB changes",
		"inventory":      "downloading or importing the TMDB inventory",
		"details":        "updating film & series information",
		"identification": "identifying torrents",
		"storage":        "saving the information update",
	}[d.Operation]
	reason := map[string]string{
		"authentication": "TMDB refused the configured token",
		"network":        "the connection to TMDB failed",
		"timeout":        "the TMDB request timed out",
		"parse":          "TMDB returned incomplete or invalid data",
		"storage":        "the local database or temporary storage operation failed",
		"change_gap":     "the change history could not cover the interruption; a progressive refresh is required",
		"cooldown":       "TMDB requested a waiting period before further requests",
		"unknown":        "the operation could not complete",
	}[d.FailureCode]
	if d.FailureCode == "http" {
		reason = fmt.Sprintf("TMDB returned HTTP %d", d.StatusCode)
		if d.StatusCode == 429 {
			reason += " (rate limit)"
		}
	}
	return stage + ": " + reason
}

func (d ServiceDiagnostic) DeferredMessage(count int) string {
	return fmt.Sprintf("%d information records deferred for retry while %s. Previous information remains available.", count, d.failureDescription())
}
