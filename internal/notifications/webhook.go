package notifications

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

type webhookPayload struct {
	Event     string          `json:"event"`
	Title     string          `json:"title"`
	Message   string          `json:"message"`
	URL       string          `json:"url,omitempty"`
	Timestamp string          `json:"timestamp"`
	Work      *webhookWork    `json:"work,omitempty"`
	Release   *webhookRelease `json:"release,omitempty"`
}

type webhookWork struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Years   []int  `json:"years,omitempty"`
	Season  *int   `json:"season,omitempty"`
	Episode *int   `json:"episode,omitempty"`
	Cover   string `json:"cover,omitempty"`
}

type webhookRelease struct {
	Title        string   `json:"title"`
	Size         int64    `json:"size,omitempty"`
	Source       string   `json:"source,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	Cached       bool     `json:"cached,omitempty"`
	ExtraMatches int      `json:"extra_matches,omitempty"`
}

func sendWebhook(ctx context.Context, client *http.Client, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	u, err := validHTTPURL(secrets.Primary)
	if err != nil {
		return errors.New("Webhook URL is invalid")
	}
	payload := webhookPayload{
		Event:     msg.Event,
		Title:     FormatHeading(msg),
		Message:   strings.Join(FormatSummaryLines(msg), "\n"),
		URL:       safeLinkURL(msg.ItemURL),
		Timestamp: msg.Timestamp.UTC().Format(time.RFC3339),
	}
	if msg.Event != EventTest {
		payload.Work = &webhookWork{
			ID:      msg.WorkID,
			Type:    msg.MediaType,
			Title:   msg.Title,
			Years:   msg.Years,
			Season:  msg.Season,
			Episode: msg.Episode,
			Cover:   safeCoverURL(msg.CoverURL),
		}
		payload.Release = &webhookRelease{
			Title:        msg.ReleaseTitle,
			Size:         msg.ReleaseSize,
			Source:       msg.SourceName,
			Languages:    msg.Languages,
			Cached:       msg.Cached,
			ExtraMatches: msg.ExtraMatches,
		}
	}
	headers := map[string]string{}
	if channel.Config.HeaderName != "" && secrets.Header != "" {
		headers[channel.Config.HeaderName] = secrets.Header
	}
	return postJSON(ctx, client, u.String(), payload, headers)
}
