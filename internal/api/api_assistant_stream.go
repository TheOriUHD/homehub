package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/assistant"
	"homehub/internal/httpctx"
	"homehub/internal/rbac"
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
			SSHRun: func(cctx context.Context, hostID, command string) (string, int, error) {
				return s.runSSHOnHost(cctx, hostID, command)
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
		Message      string `json:"message"`
		Conversation string `json:"conversation"`
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
	access.Conversation = strings.TrimSpace(body.Conversation)
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
	Actions      []assistant.PendingAction `json:"actions"`
	Conversation string                    `json:"conversation"`
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
			// Don't echo the raw error (may wrap store/internal detail); log it.
			s.logger.Warn("assistant apply action failed", "kind", a.Kind, "error", err)
			summaries = append(summaries, "⚠️ "+sm+" — failed")
			continue
		}
		summaries = append(summaries, "✓ "+sm)
		applied++
	}
	msg := strings.Join(summaries, "\n")
	_ = s.store.AddAssistantMessage(ctx, id.User.ID, store.AssistantChannelWeb, strings.TrimSpace(body.Conversation), "assistant", msg, nil)
	s.audit.Log(ctx, actorFor(id), "assistant.apply", "system/settings", "success", map[string]any{"applied": applied})
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "message": msg, "summaries": summaries})
}

// applyAssistantAction executes one approved write by REPLAYING it through
// SelfCall — the same in-process handler path the model's read tools use — so
// every action inherits the REST handler's RBAC check and audit log instead of
// re-implementing the write here. Any future action kind that maps to a
// permission-gated operation is therefore gated automatically; it must NOT be
// added as a direct store/service call.
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
		scope := p.Scope
		if scope == "" {
			scope = "all"
		}
		path := "/calendar/events/" + p.EventID.String() + "?scope=" + url.QueryEscape(scope)
		if p.OccurrenceStart != "" {
			path += "&occurrenceStart=" + url.QueryEscape(p.OccurrenceStart)
		}
		return label, selfCallResult(s.SelfCall(ctx, id, http.MethodDelete, path, nil))

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
		reqBody := map[string]any{"name": p.Name, "kind": kind}
		if p.Color != "" {
			reqBody["color"] = p.Color
		}
		if p.URL != "" {
			reqBody["url"] = p.URL
		}
		// Preserve the trash-calendar default reminder lead times the direct
		// path used to set, since the REST body carries no default.
		if kind == "trash" {
			reqBody["settings"] = map[string]any{"leadHours": []int{72, 24}}
		}
		label := fmt.Sprintf("Create calendar “%s”", p.Name)
		if err := selfCallResult(s.SelfCall(ctx, id, http.MethodPost, "/calendars", reqBody)); err != nil {
			return label, err
		}
		return fmt.Sprintf("Created calendar “%s”", p.Name), nil

	case "ssh.run":
		var p struct {
			HostID   string `json:"hostId"`
			HostName string `json:"hostName"`
			Command  string `json:"command"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Run SSH command", err
		}
		summary := fmt.Sprintf("Run on %s: %s", p.HostName, p.Command)
		hostID, perr := uuid.Parse(p.HostID)
		if perr != nil {
			return summary, errors.New("bad host id")
		}
		// Re-check permission at apply time: the proposal may be minutes old.
		if !s.can(ctx, id, ResSSHHosts, rbac.LevelControl) &&
			!s.can(ctx, id, sshHostResource(hostID), rbac.LevelControl) {
			s.deniedAudit(ctx, id, "ssh_host.agent_run", sshHostResource(hostID))
			return summary, errors.New("not permitted")
		}
		out, exit, rerr := s.runSSHOnHost(ctx, p.HostID, p.Command)
		s.audit.Log(ctx, actorFor(id), "ssh_host.agent_run", sshHostResource(hostID),
			map[bool]string{true: "success", false: "error"}[rerr == nil && exit == 0],
			map[string]any{"command": p.Command, "exit": exit})
		if rerr != nil {
			return summary, rerr
		}
		if strings.TrimSpace(out) != "" {
			summary += "\n" + strings.TrimSpace(out)
		}
		if exit != 0 {
			summary += fmt.Sprintf("\n(exit %d)", exit)
		}
		return summary, nil

	case "signals.dismiss":
		var p struct {
			Ids []int64 `json:"ids"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Dismiss signals", err
		}
		code, body := s.SelfCall(ctx, id, http.MethodPost, "/signals/dismiss", map[string]any{"ids": p.Ids})
		if err := selfCallResult(code, body); err != nil {
			return "Dismiss signals", err
		}
		n := len(p.Ids)
		var r struct {
			Dismissed int `json:"dismissed"`
		}
		if json.Unmarshal(body, &r) == nil {
			n = r.Dismissed
		}
		return fmt.Sprintf("Dismissed %d signal(s)", n), nil
	}
	return a.Summary, fmt.Errorf("unknown action %q", a.Kind)
}

// selfCallResult turns a SelfCall (status, body) into an error when the status
// is >= 400, surfacing the handler's own error message.
func selfCallResult(code int, body []byte) error {
	if code < 400 {
		return nil
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return errors.New(e.Message)
	}
	return fmt.Errorf("request failed (%d)", code)
}
