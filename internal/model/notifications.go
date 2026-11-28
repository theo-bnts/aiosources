package model

import (
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	NotificationDiscord  = "discord"
	NotificationNtfy     = "ntfy"
	NotificationGotify   = "gotify"
	NotificationTelegram = "telegram"
	NotificationSlack    = "slack"
	NotificationWebhook  = "webhook"
)

const (
	AlertOwnerAdmin   = "admin"
	AlertOwnerTrusted = "trusted"
)

const (
	AlertStatusWatching = "watching"
	AlertStatusRetrying = "retrying"
)

const (
	MaxNotificationChannels = 64
	MaxAdminReleaseAlerts   = 500
	MaxTrustedReleaseAlerts = 100
)

var (
	notificationIDRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	ntfyTopicRE         = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	telegramChatIDRE    = regexp.MustCompile(`^(?:-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{3,64})$`)
	positiveDigitsRE    = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	discordThreadIDRE   = regexp.MustCompile(`^[0-9]{1,32}$`)
	webhookHeaderNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)
	alertProviderIDRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

func IsNotificationChannelType(kind string) bool {
	switch kind {
	case NotificationDiscord, NotificationNtfy, NotificationGotify, NotificationTelegram, NotificationSlack, NotificationWebhook:
		return true
	}
	return false
}

func ValidNotificationID(id string) bool {
	return notificationIDRE.MatchString(id)
}

type NotificationChannelConfig struct {
	Username   string `json:"username,omitempty"`
	ServerURL  string `json:"server_url,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Priority   string `json:"priority,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	HeaderName string `json:"header_name,omitempty"`
}

type NotificationChannel struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Type            string                    `json:"type"`
	Enabled         bool                      `json:"enabled"`
	IsDefault       bool                      `json:"is_default"`
	AllowTrusted    bool                      `json:"allow_trusted"`
	Config          NotificationChannelConfig `json:"config"`
	SecretRef       string                    `json:"secret_ref,omitempty"`
	HeaderSecretRef string                    `json:"header_secret_ref,omitempty"`
	ActiveAlerts    int64                     `json:"active_alerts,omitempty"`
	Revision        int64                     `json:"revision"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

// NotificationChannelSummary is safe to expose to trusted users: it never
// reveals endpoint addresses, chat identifiers or secret references.
type NotificationChannelSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	IsDefault bool   `json:"is_default"`
}

func (c NotificationChannel) Summary() NotificationChannelSummary {
	return NotificationChannelSummary{
		ID:        c.ID,
		Name:      c.Name,
		Type:      c.Type,
		IsDefault: c.IsDefault,
	}
}

func (c NotificationChannel) SecretReferences() []string {
	refs := []string{}
	if c.SecretRef != "" {
		refs = append(refs, c.SecretRef)
	}
	if c.HeaderSecretRef != "" {
		refs = append(refs, c.HeaderSecretRef)
	}
	return refs
}

func validateBaseServerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || len(trimmed) > 2048 || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(trimmed, "\r\n\x00\\") {
		return "", errors.New("Enter a valid HTTP or HTTPS server URL without credentials or query parameters")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("Server URL must use HTTP or HTTPS")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("Server port must be between 1 and 65535")
		}
	}
	return strings.TrimRight(trimmed, "/"), nil
}

// NormalizeAndValidate checks channel settings and removes inapplicable fields
// for the chosen channel type.
func (c *NotificationChannel) NormalizeAndValidate() error {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 160 || strings.ContainsAny(c.Name, "\r\n\x00") {
		return errors.New("Enter a display name for this notification channel")
	}
	c.Type = strings.TrimSpace(c.Type)
	if !IsNotificationChannelType(c.Type) {
		return errors.New("Select a supported notification channel type")
	}
	c.SecretRef = strings.TrimSpace(c.SecretRef)
	c.HeaderSecretRef = strings.TrimSpace(c.HeaderSecretRef)
	if c.SecretRef != "" && !networkSecret.MatchString(c.SecretRef) {
		return errors.New("Select a valid credential from Secrets")
	}
	if c.HeaderSecretRef != "" && !networkSecret.MatchString(c.HeaderSecretRef) {
		return errors.New("Select a valid header credential from Secrets")
	}
	cfg := NotificationChannelConfig{}
	switch c.Type {
	case NotificationDiscord:
		if c.SecretRef == "" {
			return errors.New("Select a Discord webhook URL from Secrets")
		}
		c.HeaderSecretRef = ""
		cfg.Username = strings.TrimSpace(c.Config.Username)
		if len(cfg.Username) > 80 || strings.ContainsAny(cfg.Username, "\r\n\x00") {
			return errors.New("Discord username must be at most 80 characters")
		}
		cfg.ThreadID = strings.TrimSpace(c.Config.ThreadID)
		if cfg.ThreadID != "" && !discordThreadIDRE.MatchString(cfg.ThreadID) {
			return errors.New("Discord thread ID must contain digits only")
		}
	case NotificationNtfy:
		c.HeaderSecretRef = ""
		serverURL := strings.TrimSpace(c.Config.ServerURL)
		if serverURL == "" {
			serverURL = "https://ntfy.sh"
		}
		normalized, err := validateBaseServerURL(serverURL)
		if err != nil {
			return err
		}
		cfg.ServerURL = normalized
		cfg.Topic = strings.TrimSpace(c.Config.Topic)
		if !ntfyTopicRE.MatchString(cfg.Topic) {
			return errors.New("Enter a valid ntfy topic (1–64 letters, numbers, '.', '_' or '-')")
		}
		cfg.Priority = strings.ToLower(strings.TrimSpace(c.Config.Priority))
		switch cfg.Priority {
		case "", "min", "low", "default", "high", "urgent":
		default:
			return errors.New("Select a valid ntfy priority")
		}
	case NotificationGotify:
		if c.SecretRef == "" {
			return errors.New("Select a Gotify application token from Secrets")
		}
		c.HeaderSecretRef = ""
		normalized, err := validateBaseServerURL(c.Config.ServerURL)
		if err != nil {
			return err
		}
		cfg.ServerURL = normalized
		cfg.Priority = strings.TrimSpace(c.Config.Priority)
		if cfg.Priority != "" {
			n, err := strconv.Atoi(cfg.Priority)
			if err != nil || n < 0 || n > 10 {
				return errors.New("Gotify priority must be between 0 and 10")
			}
			cfg.Priority = strconv.Itoa(n)
		}
	case NotificationTelegram:
		if c.SecretRef == "" {
			return errors.New("Select a Telegram Bot API token from Secrets")
		}
		c.HeaderSecretRef = ""
		cfg.ChatID = strings.TrimSpace(c.Config.ChatID)
		if !telegramChatIDRE.MatchString(cfg.ChatID) {
			return errors.New("Enter a valid Telegram chat ID (numeric ID or @channel)")
		}
		cfg.ThreadID = strings.TrimSpace(c.Config.ThreadID)
		if cfg.ThreadID != "" && !positiveDigitsRE.MatchString(cfg.ThreadID) {
			return errors.New("Telegram topic ID must be a positive integer")
		}
	case NotificationSlack:
		if c.SecretRef == "" {
			return errors.New("Select an incoming webhook URL from Secrets")
		}
		c.HeaderSecretRef = ""
	case NotificationWebhook:
		if c.SecretRef == "" {
			return errors.New("Select a webhook URL from Secrets")
		}
		cfg.HeaderName = strings.TrimSpace(c.Config.HeaderName)
		if (cfg.HeaderName == "") != (c.HeaderSecretRef == "") {
			return errors.New("Provide both a header name and a header secret, or leave both empty")
		}
		if cfg.HeaderName != "" && !webhookHeaderNameRE.MatchString(cfg.HeaderName) {
			return errors.New("Enter a valid HTTP header name (letters, numbers and hyphens)")
		}
	}
	c.Config = cfg
	return nil
}

type ReleaseAlertFilters struct {
	Providers    []string `json:"providers,omitempty"`
	Language     string   `json:"language,omitempty"`
	LanguageMode string   `json:"language_mode,omitempty"`
	MinSize      *int64   `json:"min_size,omitempty"`
	MaxSize      *int64   `json:"max_size,omitempty"`
	CachedOnly   bool     `json:"cached_only,omitempty"`
}

func (f *ReleaseAlertFilters) NormalizeAndValidate() error {
	if len(f.Providers) > 100 {
		return errors.New("Select at most 100 sources")
	}
	cleanProviders := make([]string, 0, len(f.Providers))
	for _, provider := range f.Providers {
		p := strings.TrimSpace(provider)
		if p == "" || !alertProviderIDRE.MatchString(p) {
			return errors.New("Invalid source filter")
		}
		if !slices.Contains(cleanProviders, p) {
			cleanProviders = append(cleanProviders, p)
		}
	}
	slices.Sort(cleanProviders)
	f.Providers = cleanProviders
	f.Language = strings.ToLower(strings.TrimSpace(f.Language))
	f.LanguageMode = strings.ToLower(strings.TrimSpace(f.LanguageMode))
	switch f.LanguageMode {
	case "", "all":
		f.LanguageMode = ""
		f.Language = ""
	case "preferred", "multi", "unknown":
		if f.Language == "" || len(f.Language) > 16 {
			return errors.New("Select a preferred language when language filtering is enabled")
		}
	default:
		return errors.New("Select a valid language filter mode")
	}
	if f.MinSize != nil && *f.MinSize < 0 {
		return errors.New("Minimum size must not be negative")
	}
	if f.MaxSize != nil && *f.MaxSize < 0 {
		return errors.New("Maximum size must not be negative")
	}
	if f.MinSize != nil && *f.MinSize == 0 {
		f.MinSize = nil
	}
	if f.MaxSize != nil && *f.MaxSize == 0 {
		f.MaxSize = nil
	}
	if f.MinSize != nil && f.MaxSize != nil && *f.MinSize > *f.MaxSize {
		return errors.New("Minimum size must not exceed maximum size")
	}
	return nil
}

func (f ReleaseAlertFilters) Active() bool {
	return len(f.Providers) > 0 || (f.LanguageMode != "" && f.LanguageMode != "all") || f.MinSize != nil || f.MaxSize != nil || f.CachedOnly
}

type ReleaseAlert struct {
	ID          string              `json:"id"`
	OwnerType   string              `json:"owner_type"`
	OwnerID     string              `json:"owner_id,omitempty"`
	OwnerEmail  string              `json:"owner_email,omitempty"`
	WorkID      int64               `json:"work_id"`
	MediaType   string              `json:"media_type"`
	Title       string              `json:"title"`
	Cover       string              `json:"cover,omitempty"`
	Years       []int               `json:"years,omitempty"`
	Season      *int                `json:"season,omitempty"`
	Episode     *int                `json:"episode,omitempty"`
	ChannelID   string              `json:"channel_id"`
	ChannelName string              `json:"channel_name,omitempty"`
	ChannelType string              `json:"channel_type,omitempty"`
	Filters     ReleaseAlertFilters `json:"filters"`
	Status      string              `json:"status"`
	Attempts    int                 `json:"attempts"`
	NextAttempt time.Time           `json:"next_attempt"`
	LastError   string              `json:"last_error,omitempty"`
	Revision    int64               `json:"revision"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type ReleaseAlertInput struct {
	WorkID    int64               `json:"work_id"`
	Season    *int                `json:"season,omitempty"`
	Episode   *int                `json:"episode,omitempty"`
	ChannelID string              `json:"channel_id"`
	Filters   ReleaseAlertFilters `json:"filters"`
	Revision  int64               `json:"revision,omitempty"`
}
