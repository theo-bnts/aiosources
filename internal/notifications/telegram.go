package notifications

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/theo-bnts/aiosources/internal/model"
)

type telegramPayload struct {
	ChatID          string `json:"chat_id"`
	Text            string `json:"text"`
	ParseMode       string `json:"parse_mode"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
}

func sendTelegram(ctx context.Context, client *http.Client, opts Options, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	token := strings.TrimSpace(secrets.Primary)
	if token == "" || strings.ContainsAny(token, "/?#@\r\n\x00 ") {
		return errors.New("Telegram Bot API token is invalid")
	}
	base := strings.TrimRight(opts.TelegramBaseURL, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	lines := []string{"<b>" + html.EscapeString(FormatHeading(msg)) + "</b>"}
	for _, line := range FormatSummaryLines(msg) {
		lines = append(lines, html.EscapeString(line))
	}
	if link := safeLinkURL(msg.ItemURL); link != "" {
		lines = append(lines, `<a href="`+html.EscapeString(link)+`">Open in AIOSources</a>`)
	}
	payload := telegramPayload{
		ChatID:    channel.Config.ChatID,
		Text:      strings.Join(lines, "\n"),
		ParseMode: "HTML",
	}
	if channel.Config.ThreadID != "" {
		if id, err := strconv.ParseInt(channel.Config.ThreadID, 10, 64); err == nil && id > 0 {
			payload.MessageThreadID = id
		}
	}
	target := base + "/bot" + url.PathEscape(token) + "/sendMessage"
	return postJSON(ctx, client, target, payload, nil)
}
