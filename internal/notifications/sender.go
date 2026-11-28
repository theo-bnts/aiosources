// Package notifications delivers release alerts to external messaging and
// webhook endpoints using Go's standard HTTP client and application routing.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

const (
	EventReleaseAvailable = "release_available"
	EventTest             = "test"

	defaultTimeout = 15 * time.Second
	maxErrorBytes  = 4096
)

type Message struct {
	Event        string
	WorkID       int64
	MediaType    string
	Title        string
	Years        []int
	Season       *int
	Episode      *int
	CoverURL     string
	ItemURL      string
	ReleaseTitle string
	ReleaseSize  int64
	SourceName   string
	Languages    []string
	Cached       bool
	ExtraMatches int
	Timestamp    time.Time
}

type Secrets struct {
	Primary string
	Header  string
}

type Options struct {
	Client          *http.Client
	TelegramBaseURL string
	AllowAnyDiscord bool
}

func NewHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   defaultTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func Send(ctx context.Context, opts Options, channel model.NotificationChannel, secrets Secrets, msg Message) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	if msg.Event == "" {
		msg.Event = EventReleaseAvailable
	}
	client := opts.Client
	if client == nil {
		client = NewHTTPClient(http.DefaultTransport)
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	switch channel.Type {
	case model.NotificationDiscord:
		return sendDiscord(ctx, client, opts, channel, secrets, msg)
	case model.NotificationNtfy:
		return sendNtfy(ctx, client, channel, secrets, msg)
	case model.NotificationGotify:
		return sendGotify(ctx, client, channel, secrets, msg)
	case model.NotificationTelegram:
		return sendTelegram(ctx, client, opts, channel, secrets, msg)
	case model.NotificationSlack:
		return sendSlack(ctx, client, channel, secrets, msg)
	case model.NotificationWebhook:
		return sendWebhook(ctx, client, channel, secrets, msg)
	default:
		return errors.New("unsupported notification channel type")
	}
}

func TestMessage() Message {
	return Message{
		Event:        EventTest,
		MediaType:    "movie",
		Title:        "AIOSources test notification",
		ReleaseTitle: "Your notification channel is configured and working.",
		Timestamp:    time.Now().UTC(),
	}
}

func mediaTypeLabel(mediaType string) string {
	switch mediaType {
	case "movie":
		return "Movie"
	case "tv":
		return "Series"
	case "game":
		return "Game"
	case "software":
		return "Software"
	case "music":
		return "Music"
	case "book":
		return "Book"
	default:
		return "Release"
	}
}

func formatEpisodeTag(season, episode *int) string {
	if season == nil || episode == nil {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", *season, *episode)
}

func FormatHeading(msg Message) string {
	if msg.Event == EventTest {
		return "AIOSources test notification"
	}
	title := strings.TrimSpace(msg.Title)
	if title == "" {
		title = "Watched title"
	}
	if msg.MediaType == "tv" {
		if ep := formatEpisodeTag(msg.Season, msg.Episode); ep != "" {
			return fmt.Sprintf("Episode available: %s · %s", title, ep)
		}
	}
	if len(msg.Years) > 0 && msg.Years[0] > 0 {
		title = fmt.Sprintf("%s (%d)", title, msg.Years[0])
	}
	return fmt.Sprintf("%s available: %s", mediaTypeLabel(msg.MediaType), title)
}

func FormatSize(size int64) string {
	if size <= 0 {
		return ""
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	return fmt.Sprintf("%.2f %s", float64(size)/float64(div), units[exp])
}

func FormatSummaryLines(msg Message) []string {
	if msg.Event == EventTest {
		return []string{"Your notification channel is configured and ready to receive release alerts."}
	}
	lines := []string{}
	if rel := strings.TrimSpace(msg.ReleaseTitle); rel != "" {
		lines = append(lines, rel)
	}
	details := []string{}
	if sz := FormatSize(msg.ReleaseSize); sz != "" {
		details = append(details, sz)
	}
	if len(msg.Languages) > 0 {
		details = append(details, strings.Join(msg.Languages, ", "))
	}
	if src := strings.TrimSpace(msg.SourceName); src != "" {
		details = append(details, src)
	}
	if msg.Cached {
		details = append(details, "Cached")
	}
	if len(details) > 0 {
		lines = append(lines, strings.Join(details, " · "))
	}
	if msg.ExtraMatches > 0 {
		if msg.ExtraMatches == 1 {
			lines = append(lines, "+1 other matching release")
		} else {
			lines = append(lines, fmt.Sprintf("+%d other matching releases", msg.ExtraMatches))
		}
	}
	return lines
}

func validHTTPURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 2048 || strings.ContainsAny(trimmed, "\r\n\x00") {
		return nil, errors.New("notification endpoint URL is invalid")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("notification endpoint URL must use HTTP or HTTPS")
	}
	return u, nil
}

func safeLinkURL(raw string) string {
	u, err := validHTTPURL(raw)
	if err != nil {
		return ""
	}
	return u.String()
}

func safeCoverURL(raw string) string {
	u, err := validHTTPURL(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func postJSON(ctx context.Context, client *http.Client, targetURL string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("could not encode notification payload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid notification request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "AIOSources")
	for k, v := range headers {
		if k != "" && v != "" {
			req.Header.Set(k, v)
		}
	}
	return executeRequest(client, req)
}

// executeRequest sends the request and returns a sanitized error that never
// embeds URLs, tokens or arbitrary remote response bodies.
func executeRequest(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("notification request timed out")
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		return errors.New("could not reach notification endpoint")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBytes))
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("notification endpoint returned unexpected redirect (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("notification endpoint rate limited the request (HTTP 429)")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("notification endpoint rejected credentials (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned HTTP %s", strconv.Itoa(resp.StatusCode))
	}
	return nil
}
