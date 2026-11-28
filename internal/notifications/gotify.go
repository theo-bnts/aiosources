package notifications

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type gotifyPayload struct {
	Title    string         `json:"title"`
	Message  string         `json:"message"`
	Priority int            `json:"priority,omitempty"`
	Extras   map[string]any `json:"extras,omitempty"`
}

func sendGotify(ctx context.Context, client *http.Client, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	if _, err := validHTTPURL(channel.Config.ServerURL); err != nil {
		return errors.New("Gotify server URL is invalid")
	}
	token := strings.TrimSpace(secrets.Primary)
	if token == "" {
		return errors.New("Gotify application token is missing")
	}
	priority := 5
	if channel.Config.Priority != "" {
		if parsed, err := strconv.Atoi(channel.Config.Priority); err == nil && parsed >= 0 && parsed <= 10 {
			priority = parsed
		}
	}
	payload := gotifyPayload{
		Title:    FormatHeading(msg),
		Message:  strings.Join(FormatSummaryLines(msg), "\n"),
		Priority: priority,
	}
	if link := safeLinkURL(msg.ItemURL); link != "" {
		payload.Extras = map[string]any{
			"client::notification": map[string]any{
				"click": map[string]any{
					"url": link,
				},
			},
		}
	}
	target := strings.TrimRight(channel.Config.ServerURL, "/") + "/message"
	return postJSON(ctx, client, target, payload, map[string]string{
		"X-Gotify-Key": token,
	})
}
