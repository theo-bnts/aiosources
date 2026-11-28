package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestDiscordRichEmbedAndThreadID(t *testing.T) {
	var gotPath, gotQuery string
	var payload discordPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode discord payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ch := model.NotificationChannel{
		Type:   model.NotificationDiscord,
		Config: model.NotificationChannelConfig{Username: "Alerts", ThreadID: "987654321"},
	}
	msg := Message{
		Event:        EventReleaseAvailable,
		WorkID:       42,
		MediaType:    "movie",
		Title:        "Inception",
		Years:        []int{2010},
		CoverURL:     "https://image.tmdb.org/t/p/w342/poster.jpg",
		ItemURL:      "https://aiosources.example/browse/search/42",
		ReleaseTitle: "Inception.2010.2160p.BluRay.MULTI",
		ReleaseSize:  4 << 30,
		SourceName:   "TrackerOne",
		Languages:    []string{"FR", "EN"},
		Cached:       true,
		ExtraMatches: 2,
		Timestamp:    time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC),
	}
	err := Send(context.Background(), Options{Client: srv.Client(), AllowAnyDiscord: true}, ch, Secrets{Primary: srv.URL + "/api/webhooks/123/secret"}, msg)
	if err != nil {
		t.Fatalf("send discord: %v", err)
	}
	if gotPath != "/api/webhooks/123/secret" || gotQuery != "thread_id=987654321" {
		t.Fatalf("unexpected discord request path=%q query=%q", gotPath, gotQuery)
	}
	if payload.Username != "Alerts" || len(payload.Embeds) != 1 {
		t.Fatalf("unexpected discord payload: %+v", payload)
	}
	embed := payload.Embeds[0]
	if embed.Title != "Movie available: Inception (2010)" {
		t.Fatalf("unexpected title: %q", embed.Title)
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL != msg.CoverURL {
		t.Fatalf("missing thumbnail: %+v", embed.Thumbnail)
	}
	if embed.URL != msg.ItemURL || !strings.Contains(embed.Description, "+2 other matching releases") {
		t.Fatalf("unexpected embed details: %+v", embed)
	}
}

func TestDiscordHostRestrictionRejectsArbitraryURL(t *testing.T) {
	ch := model.NotificationChannel{Type: model.NotificationDiscord}
	err := Send(context.Background(), Options{}, ch, Secrets{Primary: "https://evil.example/api/webhooks/1/abc"}, TestMessage())
	if err == nil {
		t.Fatal("expected error for non-Discord host")
	}
	if strings.Contains(err.Error(), "abc") || strings.Contains(err.Error(), "evil.example") {
		t.Fatalf("error leaked secret/host: %v", err)
	}
}

func TestNtfyGotifyTelegramSlackWebhookAdapters(t *testing.T) {
	season, episode := 2, 5
	tvMsg := Message{
		Event:        EventReleaseAvailable,
		WorkID:       77,
		MediaType:    "tv",
		Title:        "The <Show> & Friends",
		Season:       &season,
		Episode:      &episode,
		ItemURL:      "https://aio.example/browse/search/77?season=2&episode=5",
		CoverURL:     "https://image.tmdb.org/t/p/w342/show.jpg",
		ReleaseTitle: "The.Show.S02E05.1080p.WEB-DL",
		ReleaseSize:  1500 << 20,
		SourceName:   "SourceA",
		Languages:    []string{"FR"},
	}

	t.Run("ntfy", func(t *testing.T) {
		var gotAuth, gotTitle, gotTags, gotBody, gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotTitle = r.Header.Get("Title")
			gotTags = r.Header.Get("Tags")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ch := model.NotificationChannel{
			Type:   model.NotificationNtfy,
			Config: model.NotificationChannelConfig{ServerURL: srv.URL, Topic: "releases", Priority: "high"},
		}
		if err := Send(context.Background(), Options{Client: srv.Client()}, ch, Secrets{Primary: "tk_secret"}, tvMsg); err != nil {
			t.Fatalf("send ntfy: %v", err)
		}
		if gotPath != "/releases" || gotAuth != "Bearer tk_secret" || gotTags != "tv" || !strings.Contains(gotTitle, "S02E05") || !strings.Contains(gotBody, "The.Show.S02E05") {
			t.Fatalf("unexpected ntfy call: path=%q auth=%q title=%q tags=%q body=%q", gotPath, gotAuth, gotTitle, gotTags, gotBody)
		}
	})

	t.Run("gotify", func(t *testing.T) {
		var gotKey string
		var body gotifyPayload
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-Gotify-Key")
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ch := model.NotificationChannel{
			Type:   model.NotificationGotify,
			Config: model.NotificationChannelConfig{ServerURL: srv.URL, Priority: "8"},
		}
		if err := Send(context.Background(), Options{Client: srv.Client()}, ch, Secrets{Primary: "app_token"}, tvMsg); err != nil {
			t.Fatalf("send gotify: %v", err)
		}
		if gotKey != "app_token" || body.Priority != 8 || !strings.Contains(body.Title, "S02E05") {
			t.Fatalf("unexpected gotify call: key=%q body=%+v", gotKey, body)
		}
	})

	t.Run("telegram", func(t *testing.T) {
		var gotPath string
		var body telegramPayload
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ch := model.NotificationChannel{
			Type:   model.NotificationTelegram,
			Config: model.NotificationChannelConfig{ChatID: "-100123456", ThreadID: "42"},
		}
		if err := Send(context.Background(), Options{Client: srv.Client(), TelegramBaseURL: srv.URL}, ch, Secrets{Primary: "123:bot_token"}, tvMsg); err != nil {
			t.Fatalf("send telegram: %v", err)
		}
		if gotPath != "/bot123:bot_token/sendMessage" || body.ChatID != "-100123456" || body.MessageThreadID != 42 || !strings.Contains(body.Text, "&lt;Show&gt; &amp; Friends") {
			t.Fatalf("unexpected telegram call: path=%q body=%+v", gotPath, body)
		}
	})

	t.Run("slack", func(t *testing.T) {
		var body slackPayload
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ch := model.NotificationChannel{Type: model.NotificationSlack}
		if err := Send(context.Background(), Options{Client: srv.Client()}, ch, Secrets{Primary: srv.URL}, tvMsg); err != nil {
			t.Fatalf("send slack: %v", err)
		}
		if len(body.Attachments) != 1 || !strings.Contains(body.Attachments[0].Title, "S02E05") {
			t.Fatalf("unexpected slack body: %+v", body)
		}
	})

	t.Run("webhook", func(t *testing.T) {
		var gotHeader string
		var body webhookPayload
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeader = r.Header.Get("X-Webhook-Secret")
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusAccepted)
		}))
		defer srv.Close()
		ch := model.NotificationChannel{
			Type:   model.NotificationWebhook,
			Config: model.NotificationChannelConfig{HeaderName: "X-Webhook-Secret"},
		}
		if err := Send(context.Background(), Options{Client: srv.Client()}, ch, Secrets{Primary: srv.URL, Header: "my-secret"}, tvMsg); err != nil {
			t.Fatalf("send webhook: %v", err)
		}
		if gotHeader != "my-secret" || body.Event != EventReleaseAvailable || body.Work == nil || *body.Work.Episode != 5 {
			t.Fatalf("unexpected webhook call: header=%q body=%+v", gotHeader, body)
		}
	})
}

func TestRejectRedirectsAndSanitizeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	}))
	defer srv.Close()
	client := NewHTTPClient(http.DefaultTransport)
	ch := model.NotificationChannel{Type: model.NotificationWebhook}
	err := Send(context.Background(), Options{Client: client}, ch, Secrets{Primary: srv.URL + "/super-secret-token"}, TestMessage())
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected redirect rejection, got: %v", err)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("error leaked secret token: %v", err)
	}
}
