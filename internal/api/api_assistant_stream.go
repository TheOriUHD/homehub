package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/assistant"
	"homehub/internal/calendar"
	"homehub/internal/httpctx"
	"homehub/internal/store"
)

// assistantRuntime resolves the configured provider + a Service for one turn,
// or a (code, message) pair when the assistant isn't usable.
func (s *Server) assistantRuntime(ctx context.Context, id *httpctx.Identity) (*assistant.Service, assistant.Provider, assistant.Config, string, string) {
	enabled, providerName, model, baseURL := s.settings.Assistant(ctx)
	if !enabled {
		return nil, nil, assistant.Config{}, "assistant_disabled", "the assistant is not enabled"
	}
	info := assistant.ProviderByID(providerName)
	if info == nil {
		return nil, nil, assistant.Config{}, "assistant_unconfigured", "the configured AI provider no longer exists"
	}
	apiKey, _ := s.assistantKeyFor(ctx, providerName)
	if info.RequiresKey && apiKey == "" {
		return nil, nil, assistant.Config{}, "assistant_unconfigured", "the assistant has no API key configured"
	}
	cfg := assistant.Config{Provider: providerName, Model: model, APIKey: apiKey, BaseURL: baseURL}
	provider, err := assistant.NewProviderFor(cfg)
	if err != nil {
		return nil, nil, assistant.Config{}, "assistant_unconfigured", err.Error()
	}
	svc := &assistant.Service{
		Store: s.store,
		Toolbox: &assistant.Toolbox{
			Store:   s.store,
			Metrics: s.metrics,
			Call: func(cctx context.Context, method, path string, body any) (int, []byte) {
				return s.SelfCall(cctx, id, method, path, body)
			},
		},
	}
	return svc, provider, cfg, "", ""
}

// HandleAssistantChatStream streams a turn as NDJSON: interim assistant text,
// tool/tool_result pairs (spinners), a confirm event listing any write
// actions the model queued (executed only after the user approves via
// /assistant/actions/apply), then done.
func (s *Server) HandleAssistantChatStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, `{"type":"error","message":"sign in first"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, `{"type":"error","message":"bad request"}`, http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(body.Message)
	if message == "" {
		http.Error(w, `{"type":"error","message":"say something"}`, http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"type":"error","message":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	write := func(v any) {
		_ = enc.Encode(v)
		flusher.Flush()
	}

	svc, provider, cfg, code, emsg := s.assistantRuntime(ctx, id)
	if code != "" {
		write(assistant.Event{Type: "error", Message: emsg})
		return
	}
	access := s.assistantAccess(ctx, id)
	var toolNames []string
	err := svc.RunTurnStream(ctx, provider, cfg, access, message, func(e assistant.Event) {
		if e.Type == "tool" {
			toolNames = append(toolNames, e.Tool)
		}
		write(e)
	})
	if err != nil {
		write(assistant.Event{Type: "error", Message: err.Error()})
		return
	}
	s.audit.Log(ctx, actorFor(id), "assistant.chat", "system/settings", "success", map[string]any{"tools": toolNames})
}

// ---- confirmed write actions -----------------------------------------------

type applyActionsBody struct {
	Actions []assistant.PendingAction `json:"actions"`
}

// HandleAssistantApply executes the write actions the user approved. Each
// runs deterministically under the caller's identity — the model does not
// get a second pass at them. A summary is stored as an assistant message so
// the transcript reflects what happened.
func (s *Server) HandleAssistantApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, `{"message":"sign in first"}`, http.StatusUnauthorized)
		return
	}
	var body applyActionsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil || len(body.Actions) == 0 {
		http.Error(w, `{"message":"no actions to apply"}`, http.StatusBadRequest)
		return
	}

	summaries := make([]string, 0, len(body.Actions))
	applied := 0
	for _, a := range body.Actions {
		sm, err := s.applyAssistantAction(ctx, id, a)
		if err != nil {
			summaries = append(summaries, "⚠️ "+sm+" — failed: "+err.Error())
			continue
		}
		summaries = append(summaries, "✓ "+sm)
		applied++
	}
	msg := strings.Join(summaries, "\n")
	_ = s.store.AddAssistantMessage(ctx, id.User.ID, "assistant", msg, nil)
	s.audit.Log(ctx, actorFor(id), "assistant.apply", "system/settings", "success", map[string]any{"applied": applied})
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "message": msg, "summaries": summaries})
}

func (s *Server) applyAssistantAction(ctx context.Context, id *httpctx.Identity, a assistant.PendingAction) (string, error) {
	switch a.Kind {
	case "calendar.delete_event":
		var p struct {
			EventID         uuid.UUID `json:"eventId"`
			Scope           string    `json:"scope"`
			OccurrenceStart string    `json:"occurrenceStart"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Delete event", err
		}
		label := "Deleted the event"
		if ev, err := s.store.GetCalendarEvent(ctx, p.EventID); err == nil {
			label = fmt.Sprintf("Deleted “%s”", ev.Title)
		}
		if s.calendar == nil {
			return label, fmt.Errorf("calendar service unavailable")
		}
		scope := calendar.EditScope(p.Scope)
		if scope == "" {
			scope = calendar.ScopeAll
		}
		occ := time.Time{}
		if p.OccurrenceStart != "" {
			if t, terr := time.Parse(time.RFC3339, p.OccurrenceStart); terr == nil {
				occ = t
			}
		}
		return label, s.calendar.DeleteEvent(ctx, p.EventID, scope, occ)

	case "calendar.create_calendar":
		var p struct {
			Name, Color, Kind, URL string
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Create calendar", err
		}
		kind := p.Kind
		if kind == "" {
			kind = "local"
		}
		row := store.Calendar{Name: p.Name, Color: p.Color, Kind: kind, URL: p.URL, ReadOnly: kind != "local"}
		if kind == "trash" {
			row.Settings = json.RawMessage(`{"leadHours":[72,24]}`)
		}
		created, err := s.store.CreateCalendar(ctx, row)
		if err != nil {
			return fmt.Sprintf("Create calendar “%s”", p.Name), err
		}
		// Subscribed feeds pull their events straight away (errors land in the
		// calendar's syncError, not here).
		if (kind == "ics" || kind == "trash") && p.URL != "" && s.calendar != nil {
			_ = s.calendar.Sync(ctx, created.ID)
		}
		return fmt.Sprintf("Created calendar “%s”", created.Name), nil

	case "signals.dismiss":
		var p struct {
			Ids []int64 `json:"ids"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Dismiss signals", err
		}
		n, err := s.store.DismissSignals(ctx, p.Ids, id.User.ID)
		if err == nil {
			s.hub.Publish("signals", map[string]any{"event": "changed"})
		}
		return fmt.Sprintf("Dismissed %d signal(s)", n), err
	}
	return a.Summary, fmt.Errorf("unknown action %q", a.Kind)
}
