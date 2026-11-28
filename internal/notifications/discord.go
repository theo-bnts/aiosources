package notifications

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

var discordWebhookPathRE = regexp.MustCompile(`^/api/(?:v[0-9]+/)?webhooks/[0-9]+/[A-Za-z0-9._-]+/?$`)

func isAllowedDiscordHost(host string) bool {
	switch strings.ToLower(host) {
	case "discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com":
		return true
	}
	return false
}

type discordPayload struct {
	Username string         `json:"username,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Thumbnail   *discordThumbnail   `json:"thumbnail,omitempty"`
	Footer      *discordFooter      `json:"footer,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordThumbnail struct {
	URL string `json:"url"`
}

type discordFooter struct {
	Text string `json:"text"`
}

func sendDiscord(ctx context.Context, client *http.Client, opts Options, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	u, err := validHTTPURL(secrets.Primary)
	if err != nil {
		return errors.New("Discord webhook URL is invalid")
	}
	if !opts.AllowAnyDiscord {
		if u.Scheme != "https" || !isAllowedDiscordHost(u.Hostname()) || !discordWebhookPathRE.MatchString(u.Path) {
			return errors.New("Discord webhook URL must be an HTTPS discord.com/api/webhooks/ address")
		}
	}
	if channel.Config.ThreadID != "" {
		q := u.Query()
		if q.Get("thread_id") == "" {
			q.Set("thread_id", channel.Config.ThreadID)
			u.RawQuery = q.Encode()
		}
	}
	color := 0x22c55e
	if msg.Event == EventTest {
		color = 0x3b82f6
	}
	embed := discordEmbed{
		Title:     truncateRunes(FormatHeading(msg), 256),
		URL:       safeLinkURL(msg.ItemURL),
		Color:     color,
		Footer:    &discordFooter{Text: "AIOSources"},
		Timestamp: msg.Timestamp.UTC().Format(time.RFC3339),
	}
	if msg.Event == EventTest {
		embed.Description = "Your Discord webhook is configured and ready to receive release alerts."
	} else {
		desc := strings.TrimSpace(msg.ReleaseTitle)
		if msg.ExtraMatches == 1 {
			desc += "\n\n+1 other matching release"
		} else if msg.ExtraMatches > 1 {
			desc += "\n\n+" + itoa(msg.ExtraMatches) + " other matching releases"
		}
		embed.Description = truncateRunes(strings.TrimSpace(desc), 2048)
		if sz := FormatSize(msg.ReleaseSize); sz != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{Name: "Size", Value: sz, Inline: true})
		}
		if len(msg.Languages) > 0 {
			embed.Fields = append(embed.Fields, discordEmbedField{Name: "Languages", Value: truncateRunes(strings.Join(msg.Languages, ", "), 256), Inline: true})
		}
		if src := strings.TrimSpace(msg.SourceName); src != "" {
			embed.Fields = append(embed.Fields, discordEmbedField{Name: "Source", Value: truncateRunes(src, 256), Inline: true})
		}
		if msg.Cached {
			embed.Fields = append(embed.Fields, discordEmbedField{Name: "Availability", Value: "Cached", Inline: true})
		}
	}
	if cover := safeCoverURL(msg.CoverURL); cover != "" {
		embed.Thumbnail = &discordThumbnail{URL: cover}
	}
	username := channel.Config.Username
	if username == "" {
		username = "AIOSources"
	}
	return postJSON(ctx, client, u.String(), discordPayload{
		Username: username,
		Embeds:   []discordEmbed{embed},
	}, nil)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
