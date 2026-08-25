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

	"github.com/TheOriUHD/homehub/internal/assistant"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/rbac"
)

// assistantRuntime resolves the provider for one turn. `pick` is an optional
// per-turn model chosen in the chat's model switcher: it only ever selects
// from the provider's own catalog, so a browser cannot point the API key at
// an arbitrary model, and it never touches the instance-wide default.
func (s *Server) assistantRuntime(
	ctx context.Context,
	id *httpctx.Identity,
	pick string,
	route assistant.Routing,
) (*assistant.Service, assistant.Provider, assistant.Config, string, string) {
	enabled, providerName, model, baseURL := s.settings.Assistant(ctx)
	switch {
	case pick == assistant.ModelAuto:
		basic, advanced := s.settings.AssistantAutoLanes(ctx)
		chosen := assistant.LaneModel(providerName, route.Lane, basic, advanced)
		if chosen == "" {
			return nil, nil, assistant.Config{}, "auto_unconfigured",
				"Auto needs a model for each lane — set them in the assistant settings"
		}
		model = chosen
	case pick != "":
		resolved, ok := assistant.ResolveModel(providerName, pick)
		if !ok {
			return nil, nil, assistant.Config{}, "unknown_model", "that model is not offered by the configured provider"
		}
		model = resolved
	}
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

// streamWriter turns an http.ResponseWriter into an NDJSON event sink, or
// reports that the transport can't stream.
func streamWriter(w http.ResponseWriter) (func(any), bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	return func(v any) {
		_ = enc.Encode(v)
		flusher.Flush()
	}, true
}

// HandleAssistantChatStream streams a turn as NDJSON: the stored id of the
// user message, interim assistant text, tool/tool_result pairs (spinners), a
// confirm event listing any write actions the model queued (executed only
// after the user approves via /assistant/actions/apply, which then resumes
// the same answer), then done.
//
// The body places the turn in the conversation tree. parentId is the branch
// the reader is on; sending a message with the parent of an earlier question
// forks an alternative branch (the edit flow), and regenerate answers an
// existing user message again without duplicating it (the retry flow).
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
		ParentID     *int64 `json:"parentId"`
		Regenerate   bool   `json:"regenerate"`
		Model        string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, `{"type":"error","message":"bad request"}`, http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(body.Message)
	if message == "" && !body.Regenerate {
		http.Error(w, `{"type":"error","message":"say something"}`, http.StatusBadRequest)
		return
	}
	// Regenerating answers the parent user message again; carrying text too
	// would insert a duplicate question alongside it.
	if body.Regenerate {
		message = ""
		if body.ParentID == nil {
			http.Error(w, `{"type":"error","message":"nothing to regenerate"}`, http.StatusBadRequest)
			return
		}
	}
	// An id from someone else's conversation must not become a branch point.
	if body.ParentID != nil {
		if _, err := s.store.GetAssistantMessage(ctx, id.User.ID, *body.ParentID); err != nil {
			http.Error(w, `{"type":"error","message":"unknown message"}`, http.StatusBadRequest)
			return
		}
	}

	write, ok := streamWriter(w)
	if !ok {
		http.Error(w, `{"type":"error","message":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}

	pick := strings.TrimSpace(body.Model)
	route := assistant.Routing{Lane: assistant.LaneBasic}
	if pick == assistant.ModelAuto {
		classify := message
		if classify == "" && body.ParentID != nil {
			// A regeneration re-answers a question already stored; route on
			// that text so a retry lands in the same lane.
			if parent, err := s.store.GetAssistantMessage(ctx, id.User.ID, *body.ParentID); err == nil {
				classify = parent.Content
			}
		}
		route = assistant.ClassifyLane(classify)
	}
	svc, provider, cfg, code, emsg := s.assistantRuntime(ctx, id, pick, route)
	if code != "" {
		write(assistant.Event{Type: "error", Message: emsg})
		return
	}
	access := s.assistantAccess(ctx, id)
	access.Conversation = strings.TrimSpace(body.Conversation)
	access.Route = route.Rule
	var toolNames []string
	err := svc.RunTurnStream(ctx, provider, cfg, access,
		assistant.TurnInput{Text: message, Parent: body.ParentID},
		func(e assistant.Event) {
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
	// ParentID is the assistant message that queued these actions. The
	// continuation hangs off it, so the resumed answer stays on the branch
	// the user was reading.
	ParentID *int64 `json:"parentId"`
	Model    string `json:"model"`
}

// HandleAssistantApply executes the write actions the user approved and then
// RESUMES the paused turn, streaming NDJSON like the chat endpoint. Each
// action runs deterministically under the caller's identity — the model never
// gets a second pass at them — and their output goes back to the model as
// data so it finishes the answer it was in the middle of. Raw output is not
// dumped into the transcript; the model quotes what matters.
func (s *Server) HandleAssistantApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, `{"type":"error","message":"sign in first"}`, http.StatusUnauthorized)
		return
	}
	var body applyActionsBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil || len(body.Actions) == 0 {
		http.Error(w, `{"type":"error","message":"no actions to apply"}`, http.StatusBadRequest)
		return
	}
	if body.ParentID != nil {
		if _, err := s.store.GetAssistantMessage(ctx, id.User.ID, *body.ParentID); err != nil {
			http.Error(w, `{"type":"error","message":"unknown message"}`, http.StatusBadRequest)
			return
		}
	}
	write, ok := streamWriter(w)
	if !ok {
		http.Error(w, `{"type":"error","message":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}

	outcomes := make([]assistant.ActionOutcome, 0, len(body.Actions))
	applied := 0
	for _, a := range body.Actions {
		summary, output, err := s.applyAssistantAction(ctx, id, a)
		out := assistant.ActionOutcome{Summary: summary, Output: output}
		if err != nil {
			// Don't echo the raw error (may wrap store/internal detail); log
			// it, and give the model the plain fact that it failed.
			s.logger.Warn("assistant apply action failed", "kind", a.Kind, "error", err)
			out.Failed = true
			out.Output = strings.TrimSpace(out.Output + "\n" + err.Error())
		} else {
			applied++
		}
		outcomes = append(outcomes, out)
		write(assistant.Event{Type: "action_result", Outcome: &out})
	}
	s.audit.Log(ctx, actorFor(id), "assistant.apply", "system/settings", "success", map[string]any{"applied": applied})

	// No branch point means a legacy client: the actions still ran, but there
	// is nothing to resume, so stop here rather than starting a stray thread.
	if body.ParentID == nil {
		write(assistant.Event{Type: "done"})
		return
	}
	pick := strings.TrimSpace(body.Model)
	route := assistant.Routing{Lane: assistant.LaneBasic}
	if pick == assistant.ModelAuto && body.ParentID != nil {
		// Continue on whatever answered the paused turn rather than
		// re-classifying: the continuation is the same thought.
		if parent, err := s.store.GetAssistantMessage(ctx, id.User.ID, *body.ParentID); err == nil && parent.Model != "" {
			pick = parent.Model
			if parent.Route != nil {
				route.Rule = *parent.Route
			}
		}
	}
	svc, provider, cfg, code, emsg := s.assistantRuntime(ctx, id, pick, route)
	if code != "" {
		write(assistant.Event{Type: "error", Message: emsg})
		return
	}
	access := s.assistantAccess(ctx, id)
	access.Conversation = strings.TrimSpace(body.Conversation)
	access.Route = route.Rule
	if err := svc.RunActionOutcomes(ctx, provider, cfg, access, *body.ParentID, outcomes,
		func(e assistant.Event) { write(e) }); err != nil {
		write(assistant.Event{Type: "error", Message: err.Error()})
	}
}

// applyAssistantAction executes one approved write by REPLAYING it through
// SelfCall — the same in-process handler path the model's read tools use — so
// every action inherits the REST handler's RBAC check and audit log instead of
// re-implementing the write here. Any future action kind that maps to a
// permission-gated operation is therefore gated automatically; it must NOT be
// added as a direct store/service call.
func (s *Server) applyAssistantAction(ctx context.Context, id *httpctx.Identity, a assistant.PendingAction) (string, string, error) {
	switch a.Kind {
	case "calendar.delete_event":
		var p struct {
			EventID         uuid.UUID `json:"eventId"`
			Scope           string    `json:"scope"`
			OccurrenceStart string    `json:"occurrenceStart"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Delete event", "", err
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
		return label, "", selfCallResult(s.SelfCall(ctx, id, http.MethodDelete, path, nil))

	case "calendar.create_calendar":
		var p struct {
			Name, Color, Kind, URL string
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Create calendar", "", err
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
			return label, "", err
		}
		return fmt.Sprintf("Created calendar “%s”", p.Name), "", nil

	case "ssh.run":
		var p struct {
			HostID   string `json:"hostId"`
			HostName string `json:"hostName"`
			Command  string `json:"command"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Run SSH command", "", err
		}
		summary := fmt.Sprintf("Run on %s: %s", p.HostName, p.Command)
		hostID, perr := uuid.Parse(p.HostID)
		if perr != nil {
			return summary, "", errors.New("bad host id")
		}
		// Re-check permission at apply time: the proposal may be minutes old.
		if !s.can(ctx, id, ResSSHHosts, rbac.LevelControl) &&
			!s.can(ctx, id, sshHostResource(hostID), rbac.LevelControl) {
			s.deniedAudit(ctx, id, "ssh_host.agent_run", sshHostResource(hostID))
			return summary, "", errors.New("not permitted")
		}
		out, exit, rerr := s.runSSHOnHost(ctx, p.HostID, p.Command)
		s.audit.Log(ctx, actorFor(id), "ssh_host.agent_run", sshHostResource(hostID),
			map[bool]string{true: "success", false: "error"}[rerr == nil && exit == 0],
			map[string]any{"command": p.Command, "exit": exit})
		if rerr != nil {
			return summary, "", rerr
		}
		output := strings.TrimSpace(out)
		if exit != 0 {
			output = strings.TrimSpace(output + fmt.Sprintf("\n(exit status %d)", exit))
			return summary, output, fmt.Errorf("exit status %d", exit)
		}
		return summary, output, nil

	case "signals.dismiss":
		var p struct {
			Ids []int64 `json:"ids"`
		}
		if err := json.Unmarshal(a.Params, &p); err != nil {
			return "Dismiss signals", "", err
		}
		code, body := s.SelfCall(ctx, id, http.MethodPost, "/signals/dismiss", map[string]any{"ids": p.Ids})
		if err := selfCallResult(code, body); err != nil {
			return "Dismiss signals", "", err
		}
		n := len(p.Ids)
		var r struct {
			Dismissed int `json:"dismissed"`
		}
		if json.Unmarshal(body, &r) == nil {
			n = r.Dismissed
		}
		return fmt.Sprintf("Dismissed %d signal(s)", n), "", nil
	}
	return a.Summary, "", fmt.Errorf("unknown action %q", a.Kind)
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

// ---- outbound notifications ---------------------------------------------------
