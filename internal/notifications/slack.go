package notifications

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color     string       `json:"color,omitempty"`
	Title     string       `json:"title,omitempty"`
	TitleLink string       `json:"title_link,omitempty"`
	Text      string       `json:"text,omitempty"`
	Fields    []slackField `json:"fields,omitempty"`
	ThumbURL  string       `json:"thumb_url,omitempty"`
	Footer    string       `json:"footer,omitempty"`
	Ts        int64        `json:"ts,omitempty"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func sendSlack(ctx context.Context, client *http.Client, _ model.NotificationChannel, secrets Secrets, msg Message) error {
	u, err := validHTTPURL(secrets.Primary)
	if err != nil {
		return errors.New("Slack/Mattermost webhook URL is invalid")
	}
	heading := FormatHeading(msg)
	color := "#22c55e"
	if msg.Event == EventTest {
		color = "#3b82f6"
	}
	attachment := slackAttachment{
		Color:     color,
		Title:     heading,
		TitleLink: safeLinkURL(msg.ItemURL),
		Footer:    "AIOSources",
		Ts:        msg.Timestamp.Unix(),
	}
	if msg.Event == EventTest {
		attachment.Text = "Your incoming webhook is configured and ready to receive release alerts."
	} else {
		attachment.Text = strings.Join(FormatSummaryLines(msg), "\n")
		if sz := FormatSize(msg.ReleaseSize); sz != "" {
			attachment.Fields = append(attachment.Fields, slackField{Title: "Size", Value: sz, Short: true})
		}
		if src := strings.TrimSpace(msg.SourceName); src != "" {
			attachment.Fields = append(attachment.Fields, slackField{Title: "Source", Value: src, Short: true})
		}
	}
	if cover := safeCoverURL(msg.CoverURL); cover != "" {
		attachment.ThumbURL = cover
	}
	return postJSON(ctx, client, u.String(), slackPayload{
		Text:        heading,
		Attachments: []slackAttachment{attachment},
	}, nil)
}
