// Package alerting evaluates threshold rules over the metric history and
// delivers notifications to outbound channels.
package alerting

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"homehub/internal/integration"
	"homehub/internal/store"
)

// Message is one outbound notification.
type Message struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Severity  string    `json:"severity"`
	Resource  string    `json:"resource,omitempty"`
	EventType string    `json:"eventType"`
	Time      time.Time `json:"time"`
	// RecipientEmail carries the target user's address for email channels.
	RecipientEmail string `json:"-"`
}

// ChannelTypes lists the supported outbound channel types with the secret
// names each stores in the vault (scope "channel:<id>").
var ChannelTypes = map[string][]string{
	"webhook":  {"authValue"},
	"email":    {"username", "password"},
	"ntfy":     {"token"},
	"gotify":   {"token"},
	"telegram": {"botToken"},
	"discord":  {"webhookUrl"},
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Send delivers one message through a channel. Secrets are resolved through
// the reader so tokens never sit in the channel row.
func Send(ctx context.Context, channel *store.NotificationChannel, secret func(name string) string, msg Message) error {
	cfg := integration.ConfigFromJSON(channel.Config)
	str := func(key string) string {
		v, _ := cfg[key].(string)
		return strings.TrimSpace(v)
	}
	switch channel.Type {
	case "webhook":
		return sendWebhook(ctx, str("url"), str("authHeader"), secret("authValue"), msg)
	case "email":
		port := 587
		if v, ok := cfg["port"].(float64); ok && v > 0 {
			port = int(v)
		}
		return sendEmail(str("host"), port, str("from"), secret("username"), secret("password"), msg)
	case "ntfy":
		return sendNtfy(ctx, str("server"), str("topic"), secret("token"), msg)
	case "gotify":
		return sendGotify(ctx, str("server"), secret("token"), msg)
	case "telegram":
		return sendTelegram(ctx, secret("botToken"), str("chatId"), msg)
	case "discord":
		return sendDiscord(ctx, secret("webhookUrl"), msg)
	}
	return fmt.Errorf("unknown channel type %q", channel.Type)
}

func postJSON(ctx context.Context, targetURL string, payload any, decorate func(*http.Request)) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if decorate != nil {
		decorate(req)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("delivery failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendWebhook(ctx context.Context, targetURL, authHeader, authValue string, msg Message) error {
	if targetURL == "" {
		return fmt.Errorf("webhook URL is not configured")
	}
	return postJSON(ctx, targetURL, msg, func(req *http.Request) {
		if authHeader != "" && authValue != "" {
			req.Header.Set(authHeader, authValue)
		}
	})
}

// sendEmail speaks SMTP with STARTTLS when offered; recipient is the target
// user's address.
func sendEmail(host string, port int, from, username, password string, msg Message) error {
	if host == "" || from == "" {
		return fmt.Errorf("SMTP host and from address are required")
	}
	if msg.RecipientEmail == "" {
		return fmt.Errorf("the recipient has no email address on their account")
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	body := fmt.Sprintf("From: HomeHub <%s>\r\nTo: %s\r\nSubject: [HomeHub] %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n\r\nResource: %s\r\nSeverity: %s\r\nTime: %s\r\n",
		from, msg.RecipientEmail, msg.Title, msg.Body, msg.Resource, msg.Severity, msg.Time.Format(time.RFC1123))

	if port == 465 {
		// Implicit TLS.
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer client.Close()
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(from); err != nil {
			return err
		}
		if err := client.Rcpt(msg.RecipientEmail); err != nil {
			return err
		}
		writer, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			return err
		}
		return writer.Close()
	}
	// Plain/STARTTLS path: SendMail upgrades via STARTTLS when the server
	// offers it.
	return smtp.SendMail(addr, auth, from, []string{msg.RecipientEmail}, []byte(body))
}

func ntfyPriority(severity string) string {
	switch severity {
	case "error":
		return "high"
	case "warning":
		return "default"
	}
	return "low"
}

func sendNtfy(ctx context.Context, server, topic, token string, msg Message) error {
	if server == "" || topic == "" {
		return fmt.Errorf("ntfy server and topic are required")
	}
	target := strings.TrimRight(server, "/") + "/" + url.PathEscape(topic)
	req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(msg.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", msg.Title)
	req.Header.Set("Priority", ntfyPriority(msg.Severity))
	if msg.Resource != "" {
		req.Header.Set("Tags", "homehub")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy delivery failed (%d)", resp.StatusCode)
	}
	return nil
}

func gotifyPriority(severity string) int {
	switch severity {
	case "error":
		return 8
	case "warning":
		return 5
	}
	return 2
}

func sendGotify(ctx context.Context, server, token string, msg Message) error {
	if server == "" {
		return fmt.Errorf("Gotify server is required")
	}
	if token == "" {
		return fmt.Errorf("Gotify application token is required")
	}
	target := strings.TrimRight(server, "/") + "/message?token=" + url.QueryEscape(token)
	return postJSON(ctx, target, map[string]any{
		"title":    msg.Title,
		"message":  msg.Body,
		"priority": gotifyPriority(msg.Severity),
	}, nil)
}

func sendTelegram(ctx context.Context, botToken, chatID string, msg Message) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("Telegram bot token and chat id are required")
	}
	text := "*" + msg.Title + "*"
	if msg.Body != "" {
		text += "\n" + msg.Body
	}
	target := "https://api.telegram.org/bot" + botToken + "/sendMessage"
	return postJSON(ctx, target, map[string]any{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}, nil)
}

func discordColor(severity string) int {
	switch severity {
	case "error":
		return 0xdc2626
	case "warning":
		return 0xd97706
	case "success":
		return 0x16a34a
	}
	return 0x6366f1
}

func sendDiscord(ctx context.Context, webhookURL string, msg Message) error {
	if webhookURL == "" {
		return fmt.Errorf("Discord webhook URL is required")
	}
	return postJSON(ctx, webhookURL, map[string]any{
		"embeds": []map[string]any{{
			"title":       msg.Title,
			"description": msg.Body,
			"color":       discordColor(msg.Severity),
			"footer":      map[string]string{"text": "HomeHub" + suffixIf(msg.Resource != "", " · "+msg.Resource)},
			"timestamp":   msg.Time.UTC().Format(time.RFC3339),
		}},
	}, nil)
}

func suffixIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}
