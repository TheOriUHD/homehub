package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"homehub/internal/assistant"
	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// The assistant runs with the CALLER's identity: every tool checks the
// caller's own RBAC, so the model can never read past the person asking.

const assistantVaultScope = "assistant"

// assistantLegacyKey is the pre-per-provider slot; it counts as the key
// of whichever provider is currently configured, so nothing breaks for
// installs that stored a key before slots were per-provider.
const assistantLegacyKey = "api-key"

func assistantKeyName(provider string) string { return "api-key-" + provider }

// assistantKeyFor returns the stored key for one provider: its own slot
// first, the legacy slot when that provider is the configured one.
func (s *Server) assistantKeyFor(ctx context.Context, provider string) (string, bool) {
	if raw, err := s.vault.Open(ctx, assistantVaultScope, assistantKeyName(provider)); err == nil {
		return string(raw), true
	}
	if _, current, _, _ := s.settings.Assistant(ctx); provider == current {
		if raw, err := s.vault.Open(ctx, assistantVaultScope, assistantLegacyKey); err == nil {
			return string(raw), true
		}
	}
	return "", false
}

// assistantKeysSet lists every provider holding a stored key.
func (s *Server) assistantKeysSet(ctx context.Context) []string {
	out := []string{}
	for _, info := range assistant.Providers {
		if _, ok := s.assistantKeyFor(ctx, info.ID); ok {
			out = append(out, info.ID)
		}
	}
	return out
}

func (s *Server) GetAssistant(ctx context.Context, _ GetAssistantRequestObject) (GetAssistantResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	enabled, provider, model, baseURL := s.settings.Assistant(ctx)
	_, keySet := s.assistantKeyFor(ctx, provider)
	out := GetAssistant200JSONResponse{
		Enabled:      enabled,
		Provider:     provider,
		Model:        model,
		BaseUrl:      baseURL,
		KeySet:       keySet,
		KeysSet:      s.assistantKeysSet(ctx),
		CanConfigure: s.can(ctx, id, ResSettings, rbac.LevelConfigure),
	}
	for _, info := range assistant.Providers {
		entry := AssistantProvider{
			DefaultModel: info.DefaultModel,
			Id:           info.ID,
			Kind:         AssistantProviderKind(info.Kind),
			Label:        info.Label,
			RequiresKey:  info.RequiresKey,
		}
		if info.DefaultBaseURL != "" {
			u := info.DefaultBaseURL
			entry.DefaultBaseUrl = &u
		}
		if info.ModelHint != "" {
			h := info.ModelHint
			entry.ModelHint = &h
		}
		if len(info.Models) > 0 {
			models := make([]AssistantModel, 0, len(info.Models))
			for _, m := range info.Models {
				model := AssistantModel{Id: m.ID, Label: m.Label, Blurb: m.Blurb}
				if m.Tier != "" {
					tier := AssistantModelTier(m.Tier)
					model.Tier = &tier
				}
				if m.CostNote != "" {
					note := m.CostNote
					model.CostNote = &note
				}
				if len(m.Aliases) > 0 {
					aliases := m.Aliases
					model.Aliases = &aliases
				}
				models = append(models, model)
			}
			entry.Models = &models
		}
		out.Providers = append(out.Providers, entry)
	}
	return out, nil
}

func (s *Server) PutAssistantSettings(ctx context.Context, req PutAssistantSettingsRequestObject) (PutAssistantSettingsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "assistant.configure", ResSettings)
		return PutAssistantSettings403JSONResponse{forbidden()}, nil
	}
	apiKey := ""
	if req.Body.ApiKey != nil {
		apiKey = *req.Body.ApiKey
	}
	info := assistant.ProviderByID(req.Body.Provider)
	if info == nil {
		return PutAssistantSettings400JSONResponse{BadRequestJSONResponse(errBody("invalid_settings", "unknown AI provider"))}, nil
	}
	// The key lands in the SUBMITTED provider's own slot — switching
	// providers later switches back to whatever that one had stored.
	if key := strings.TrimSpace(apiKey); key != "" {
		if err := s.vault.Seal(ctx, assistantVaultScope, assistantKeyName(req.Body.Provider), []byte(key)); err != nil {
			return nil, err
		}
	}
	if req.Body.Enabled && info.RequiresKey {
		if _, ok := s.assistantKeyFor(ctx, req.Body.Provider); !ok {
			return PutAssistantSettings400JSONResponse{BadRequestJSONResponse(errBody("no_api_key", "enter an API key before enabling the assistant"))}, nil
		}
	}
	baseURL := ""
	if req.Body.BaseUrl != nil {
		baseURL = strings.TrimSpace(*req.Body.BaseUrl)
	}
	if err := s.settings.SetAssistant(ctx, req.Body.Enabled, req.Body.Provider, req.Body.Model, baseURL); err != nil {
		return PutAssistantSettings400JSONResponse{BadRequestJSONResponse(errBody("invalid_settings", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "assistant.configure", ResSettings, "success", map[string]any{
		"enabled": req.Body.Enabled, "provider": req.Body.Provider, "model": req.Body.Model,
		"keyChanged": strings.TrimSpace(apiKey) != "",
	})
	return PutAssistantSettings204Response{}, nil
}

func (s *Server) AssistantDiscover(ctx context.Context, _ AssistantDiscoverRequestObject) (AssistantDiscoverResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	// A network sweep is an admin's tool, same bar as configuring the
	// assistant it feeds.
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		return AssistantDiscover403JSONResponse{forbidden()}, nil
	}
	found := assistant.DiscoverLMStudio(ctx)
	type wireInstance = struct {
		BaseUrl     string   `json:"baseUrl"`
		Models      []string `json:"models"`
		RequiresKey *bool    `json:"requiresKey,omitempty"`
	}
	out := AssistantDiscover200JSONResponse{Instances: []wireInstance{}}
	for _, inst := range found {
		entry := wireInstance{BaseUrl: inst.BaseURL, Models: inst.Models}
		if inst.RequiresKey {
			v := true
			entry.RequiresKey = &v
		}
		out.Instances = append(out.Instances, entry)
	}
	return out, nil
}

func (s *Server) AssistantVerifyEndpoint(ctx context.Context, req AssistantVerifyEndpointRequestObject) (AssistantVerifyEndpointResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		return AssistantVerifyEndpoint403JSONResponse{forbidden()}, nil
	}
	// Probe with the key the chat would use: an explicitly typed (not yet
	// saved) key wins, else the selected provider's stored key. The key
	// itself never returns to the client.
	probeKey := ""
	if req.Body.ApiKey != nil {
		probeKey = strings.TrimSpace(*req.Body.ApiKey)
	}
	if probeKey == "" && req.Body.Provider != nil && *req.Body.Provider != "" {
		probeKey, _ = s.assistantKeyFor(ctx, *req.Body.Provider)
	}
	models, err := assistant.VerifyOpenAIEndpoint(ctx, req.Body.BaseUrl, probeKey)
	out := AssistantVerifyEndpoint200JSONResponse{Ok: err == nil, Models: models}
	if err != nil {
		msg := err.Error()
		out.Message = &msg
		out.Models = []string{}
	}
	return out, nil
}

func (s *Server) assistantAccess(ctx context.Context, id *httpctx.Identity) assistant.Access {
	return assistant.Access{
		UserID:      id.User.ID,
		DisplayName: id.User.DisplayName,
		Can: func(resource, level string) bool {
			return s.can(ctx, id, resource, rbac.Level(level))
		},
		VisibleUnder: func(prefix string) bool {
			return s.visibleUnder(ctx, id, prefix)
		},
	}
}

func (s *Server) AssistantChat(ctx context.Context, req AssistantChatRequestObject) (AssistantChatResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	// No picker on this path — the Telegram bridge runs the instance default.
	svc, provider, cfg, code, emsg := s.assistantRuntime(ctx, id, "")
	if code != "" {
		return AssistantChat409JSONResponse(errBody(code, emsg)), nil
	}

	message := strings.TrimSpace(req.Body.Message)
	if message == "" {
		return AssistantChat400JSONResponse{BadRequestJSONResponse(errBody("empty_message", "say something"))}, nil
	}

	reply, steps, err := svc.RunTurn(ctx, provider, cfg, s.assistantAccess(ctx, id), message)
	toolNames := make([]string, 0, len(steps))
	for _, step := range steps {
		toolNames = append(toolNames, step.Tool)
	}
	if err != nil {
		// The provider's words carry the actionable detail (quota, bad key).
		return AssistantChat502JSONResponse{UpstreamErrorJSONResponse(errBody("assistant_failed", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "assistant.chat", "system/settings", "success", map[string]any{"tools": toolNames})
	out := AssistantChat200JSONResponse{Reply: reply, Tools: toolNames}
	if apiSteps := stepsToAPI(steps); len(apiSteps) > 0 {
		out.Steps = &apiSteps
	}
	return out, nil
}

// stepsToAPI maps service step info to the wire shape.
func stepsToAPI(steps []assistant.StepInfo) []AssistantStep {
	out := make([]AssistantStep, 0, len(steps))
	for _, step := range steps {
		entry := AssistantStep{Tool: step.Tool}
		if len(step.Input) > 0 {
			var input map[string]interface{}
			if json.Unmarshal(step.Input, &input) == nil {
				entry.Input = &input
			}
		}
		if step.IsError {
			v := true
			entry.IsError = &v
		}
		out = append(out, entry)
	}
	return out
}

func (s *Server) GetAssistantHistory(ctx context.Context, req GetAssistantHistoryRequestObject) (GetAssistantHistoryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conversation := ""
	if req.Params.Conversation != nil {
		conversation = strings.TrimSpace(*req.Params.Conversation)
	}
	rows, err := s.store.ListAssistantMessages(ctx, id.User.ID, store.AssistantChannelWeb, conversation, 100)
	if err != nil {
		return nil, err
	}
	out := GetAssistantHistory200JSONResponse{}
	for _, row := range rows {
		item := struct {
			Content  string                                     `json:"content"`
			Id       int64                                      `json:"id"`
			ParentId *int64                                     `json:"parentId,omitempty"`
			Role     GetAssistantHistory200JSONResponseBodyRole `json:"role"`
			Steps    *[]AssistantStep                           `json:"steps,omitempty"`
			Tools    *[]string                                  `json:"tools,omitempty"`
			Ts       time.Time                                  `json:"ts"`
			Usage    *AssistantUsage                            `json:"usage,omitempty"`
		}{
			Content:  row.Content,
			Id:       row.ID,
			ParentId: row.ParentID,
			Role:     GetAssistantHistory200JSONResponseBodyRole(row.Role),
			Ts:       row.CreatedAt,
			Usage:    assistantUsage(row),
		}
		if names := toolNamesFromTrace(row.ToolTrace); len(names) > 0 {
			item.Tools = &names
		}
		if steps := stepsFromTrace(row.ToolTrace); len(steps) > 0 {
			item.Steps = &steps
		}
		out = append(out, item)
	}
	return out, nil
}

// assistantUsage prices a stored answer. Rows written before usage was
// recorded carry no token counts and return nil, so the UI can distinguish
// "we did not measure this" from "this cost nothing".
func assistantUsage(row store.AssistantMessage) *AssistantUsage {
	if row.InputTokens == nil || row.OutputTokens == nil || row.Model == "" {
		return nil
	}
	out := &AssistantUsage{
		Model:        row.Model,
		InputTokens:  *row.InputTokens,
		OutputTokens: *row.OutputTokens,
	}
	if info := assistant.ModelInfoFor(row.Model); info != nil {
		label := info.Label
		out.ModelLabel = &label
	}
	if cost, ok := assistant.EstimateCost(row.Model, assistant.Usage{
		InputTokens:  *row.InputTokens,
		OutputTokens: *row.OutputTokens,
	}); ok {
		out.CostUsd = &cost
	}
	return out
}

func (s *Server) ClearAssistantHistory(ctx context.Context, _ ClearAssistantHistoryRequestObject) (ClearAssistantHistoryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if err := s.store.ClearAssistantMessages(ctx, id.User.ID, store.AssistantChannelWeb); err != nil {
		return nil, err
	}
	return ClearAssistantHistory204Response{}, nil
}

// stepsFromTrace rehydrates full steps (with inputs) from a stored trace.
func stepsFromTrace(trace json.RawMessage) []AssistantStep {
	if len(trace) == 0 {
		return nil
	}
	var steps []assistant.StepInfo
	if json.Unmarshal(trace, &steps) != nil {
		return nil
	}
	return stepsToAPI(steps)
}

// toolNamesFromTrace extracts the tool names from a stored trace.
func toolNamesFromTrace(trace json.RawMessage) []string {
	if len(trace) == 0 {
		return nil
	}
	var steps []assistant.StepInfo
	if json.Unmarshal(trace, &steps) != nil {
		return nil
	}
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, step.Tool)
	}
	return names
}
