// Package audit records every state-changing action. Entries are written
// synchronously inside the request path — an audit trail that can silently
// drop entries is not a trail — but a write failure never fails the user's
// action; it escalates to an error log instead.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"homehub/internal/store"
)

type Service struct {
	store  *store.Store
	logger *slog.Logger
}

func New(st *store.Store, logger *slog.Logger) *Service {
	return &Service{store: st, logger: logger.With("component", "audit")}
}

// Actor identifies who performed an action.
type Actor struct {
	Kind      string // user | system | agent
	ID        *uuid.UUID
	Name      string
	SessionID *uuid.UUID
	IP        string
}

// SystemActor is used for actions the app takes on its own (schedules,
// retention pruning, health transitions).
var SystemActor = Actor{Kind: "system", Name: "homehub"}

const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// Log records one entry. detail must marshal to JSON; nil is fine.
func (s *Service) Log(ctx context.Context, actor Actor, action, resource, outcome string, detail map[string]any) {
	var raw json.RawMessage
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			s.logger.Error("audit detail marshal failed", "action", action, "error", err)
			b = []byte(`{}`)
		}
		raw = b
	}
	entry := &store.AuditEntry{
		ActorKind: actor.Kind,
		ActorID:   actor.ID,
		ActorName: actor.Name,
		SessionID: actor.SessionID,
		Action:    action,
		Resource:  resource,
		Outcome:   outcome,
		IP:        actor.IP,
		Detail:    raw,
	}
	if err := s.store.InsertAudit(ctx, entry); err != nil {
		s.logger.Error("AUDIT WRITE FAILED — action occurred but was not recorded",
			"action", action, "resource", resource, "actor", actor.Name, "error", err)
	}
}
