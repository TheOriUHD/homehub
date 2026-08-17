// Package notify is the single doorway for user notifications: it writes the
// in-app inbox row, pushes it over the live socket, and fans out to the
// user's subscribed outbound channels via the job queue (so deliveries retry
// and never block the caller).
package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/alerting"
	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
	"github.com/TheOriUHD/homehub/internal/ws"
)

// EventTypes catalogs every event users can subscribe to; the preferences UI
// renders exactly this set.
var EventTypes = []string{
	"integration.unreachable",
	"integration.recovered",
	"alert.firing",
	"alert.resolved",
	"docker.job",
	"security",
}

type Service struct {
	store  *store.Store
	hub    *ws.Hub
	vault  *vault.Vault
	logger *slog.Logger
	// enqueue schedules a channel delivery job; wired to the jobs engine.
	enqueue func(ctx context.Context, payload DeliveryPayload)
}

// DeliveryPayload parameterizes one outbound channel delivery.
type DeliveryPayload struct {
	ChannelID uuid.UUID         `json:"channelId"`
	Message   alerting.Message  `json:"message"`
	Recipient string            `json:"recipient,omitempty"`
}

func New(st *store.Store, hub *ws.Hub, v *vault.Vault, logger *slog.Logger) *Service {
	return &Service{store: st, hub: hub, vault: v, logger: logger.With("component", "notify")}
}

// SetEnqueue wires the delivery job scheduler (avoids a package cycle with
// the jobs engine at construction time).
func (s *Service) SetEnqueue(fn func(ctx context.Context, payload DeliveryPayload)) {
	s.enqueue = fn
}

// Notify records and routes one notification for one user.
func (s *Service) Notify(ctx context.Context, userID uuid.UUID, severity, title, body, resource, eventType string) {
	n, err := s.store.InsertNotification(ctx, &store.Notification{
		UserID:    userID,
		Severity:  severity,
		Title:     title,
		Body:      body,
		Resource:  resource,
		EventType: eventType,
	})
	if err != nil {
		s.logger.Error("insert notification", "error", err)
		return
	}
	s.hub.PublishUser(userID, "notifications", map[string]any{
		"id": n.ID, "ts": n.TS, "severity": n.Severity, "title": n.Title,
		"body": n.Body, "resource": n.Resource, "eventType": n.EventType, "read": false,
	})

	// Outbound routing per the user's preferences for this event type.
	pref, err := s.store.GetNotificationPref(ctx, userID, eventType)
	if err != nil {
		return // no preference row: default is in-app only
	}
	if len(pref.ChannelIDs) == 0 {
		return
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return
	}
	msg := alerting.Message{
		Title:     title,
		Body:      body,
		Severity:  severity,
		Resource:  resource,
		EventType: eventType,
		Time:      time.Now(),
	}
	recipient := ""
	if user.Email != nil {
		recipient = *user.Email
	}
	for _, channelID := range pref.ChannelIDs {
		if s.enqueue == nil {
			return
		}
		s.enqueue(ctx, DeliveryPayload{ChannelID: channelID, Message: msg, Recipient: recipient})
	}
}

// Deliver executes one channel delivery — the "notify.deliver" job handler.
func (s *Service) Deliver(ctx context.Context, payload json.RawMessage) error {
	var p DeliveryPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	channel, err := s.store.GetChannel(ctx, p.ChannelID)
	if err != nil {
		if err == store.ErrNotFound {
			s.logger.Warn("delivery to deleted channel dropped", "channel", p.ChannelID)
			return nil
		}
		return err
	}
	if !channel.Enabled {
		return nil
	}
	p.Message.RecipientEmail = p.Recipient
	return alerting.Send(ctx, channel, func(name string) string {
		raw, serr := s.vault.Open(ctx, "channel:"+channel.ID.String(), name)
		if serr != nil {
			return ""
		}
		return string(raw)
	}, p.Message)
}

// NotifyResourceViewers routes one event to every active user who can see
// the resource. canView is supplied by the caller (RBAC evaluation).
func (s *Service) NotifyResourceViewers(ctx context.Context, resource string,
	canView func(ctx context.Context, userID uuid.UUID) bool,
	severity, title, body, eventType string) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.logger.Error("notify viewers: list users", "error", err)
		return
	}
	for _, user := range users {
		if user.Status != "active" || !canView(ctx, user.ID) {
			continue
		}
		s.Notify(ctx, user.ID, severity, title, body, resource, eventType)
	}
}
