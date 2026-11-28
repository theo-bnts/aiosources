package notifications

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

func ntfyTagForMedia(mediaType string, isTest bool) string {
	if isTest {
		return "bell"
	}
	switch mediaType {
	case "movie":
		return "clapper"
	case "tv":
		return "tv"
	case "game":
		return "video_game"
	case "software":
		return "computer"
	case "music":
		return "musical_note"
	case "book":
		return "books"
	default:
		return "bell"
	}
}

func sendNtfy(ctx context.Context, client *http.Client, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	base := channel.Config.ServerURL
	if base == "" {
		base = "https://ntfy.sh"
	}
	if _, err := validHTTPURL(base); err != nil {
		return errors.New("ntfy server URL is invalid")
	}
	target := strings.TrimRight(base, "/") + "/" + url.PathEscape(channel.Config.Topic)
	body := strings.Join(FormatSummaryLines(msg), "\n")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return errors.New("invalid ntfy request")
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", "AIOSources")
	req.Header.Set("Title", sanitizeHeaderValue(FormatHeading(msg)))
	req.Header.Set("Tags", ntfyTagForMedia(msg.MediaType, msg.Event == EventTest))
	if channel.Config.Priority != "" {
		req.Header.Set("Priority", channel.Config.Priority)
	}
	if link := safeLinkURL(msg.ItemURL); link != "" {
		req.Header.Set("Click", link)
	}
	if cover := safeCoverURL(msg.CoverURL); cover != "" {
		req.Header.Set("Attach", cover)
	}
	if token := strings.TrimSpace(secrets.Primary); token != "" {
		if strings.Contains(token, ":") {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(token)))
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return executeRequest(client, req)
}

func sanitizeHeaderValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 || r == 0x7f {
			return ' '
		}
		// HTTP header values in Go's net/http must be ASCII/latin1-safe bytes.
		if r > 0x7e {
			if r == '·' || r == '–' || r == '—' {
				return '-'
			}
			return -1
		}
		return r
	}, value)
	return strings.Join(strings.Fields(cleaned), " ")
}
