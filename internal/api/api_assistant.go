package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/TheOriUHD/homehub/internal/assistant"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// The assistant runs with the CALLER's identity: every tool checks the
// caller's own RBAC, so the model can never read past the person asking.

const assistantVaultScope = "assistant"
const assistantVaultKey = "api-key"

func (s *Server) assistantKeySet(ctx context.Context) bool {
	_, err := s.vault.Open(ctx, assistantVaultScope, assistantVaultKey)
	return err == nil
}

func (s *Server) GetAssistant(ctx context.Context, _ GetAssistantRequestObject) (GetAssistantResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	enabled, provider, model, baseURL := s.settings.Assistant(ctx)
	out := GetAssistant200JSONResponse{
		Enabled:      enabled,
		Provider:     provider,
		Model:        model,
		BaseUrl:      baseURL,
		KeySet:       s.assistantKeySet(ctx),
		CanConfigure: s.can(ctx, id, ResSettings, rbac.LevelConfigure),
	}
	for _, info := range assistant.Providers {
		entry := struct {
			DefaultBaseUrl *string                                      `json:"defaultBaseUrl,omitempty"`
			DefaultModel   string                                       `json:"defaultModel"`
			Id             string                                       `json:"id"`
			Kind           GetAssistant200JSONResponseBodyProvidersKind `json:"kind"`
			Label          string                                       `json:"label"`
			ModelHint      *string                                      `json:"modelHint,omitempty"`
			RequiresKey    bool                                         `json:"requiresKey"`
		}{
			DefaultModel: info.DefaultModel,
			Id:           info.ID,
			Kind:         GetAssistant200JSONResponseBodyProvidersKind(info.Kind),
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
	if key := strings.TrimSpace(apiKey); key != "" {
		if err := s.vault.Seal(ctx, assistantVaultScope, assistantVaultKey, []byte(key)); err != nil {
			return nil, err
		}
	}
	info := assistant.ProviderByID(req.Body.Provider)
	if info == nil {
		return PutAssistantSettings400JSONResponse{BadRequestJSONResponse(errBody("invalid_settings", "unknown AI provider"))}, nil
	}
	if req.Body.Enabled && info.RequiresKey && !s.assistantKeySet(ctx) {
		return PutAssistantSettings400JSONResponse{BadRequestJSONResponse(errBody("no_api_key", "enter an API key before enabling the assistant"))}, nil
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
	out := AssistantDiscover200JSONResponse{Instances: []struct {
		BaseUrl string   `json:"baseUrl"`
		Models  []string `json:"models"`
	}{}}
	for _, inst := range found {
		out.Instances = append(out.Instances, struct {
			BaseUrl string   `json:"baseUrl"`
			Models  []string `json:"models"`
		}{BaseUrl: inst.BaseURL, Models: inst.Models})
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
	models, err := assistant.VerifyOpenAIEndpoint(ctx, req.Body.BaseUrl)
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
	enabled, providerName, model, baseURL := s.settings.Assistant(ctx)
	if !enabled {
		return AssistantChat409JSONResponse(errBody("assistant_disabled", "the assistant is not enabled — an administrator can turn it on in its settings")), nil
	}
	info := assistant.ProviderByID(providerName)
	if info == nil {
		return AssistantChat409JSONResponse(errBody("assistant_unconfigured", "the configured AI provider no longer exists")), nil
	}
	apiKey := ""
	if keyRaw, err := s.vault.Open(ctx, assistantVaultScope, assistantVaultKey); err == nil {
		apiKey = string(keyRaw)
	} else if !errors.Is(err, vault.ErrNotFound) && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if info.RequiresKey && apiKey == "" {
		return AssistantChat409JSONResponse(errBody("assistant_unconfigured", "the assistant has no API key configured")), nil
	}

	cfg := assistant.Config{Provider: providerName, Model: model, APIKey: apiKey, BaseURL: baseURL}
	provider, err := assistant.NewProviderFor(cfg)
	if err != nil {
		return AssistantChat409JSONResponse(errBody("assistant_unconfigured", err.Error())), nil
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

func (s *Server) GetAssistantHistory(ctx context.Context, _ GetAssistantHistoryRequestObject) (GetAssistantHistoryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListAssistantMessages(ctx, id.User.ID, 100)
	if err != nil {
		return nil, err
	}
	out := GetAssistantHistory200JSONResponse{}
	for _, row := range rows {
		item := struct {
			Content string                                     `json:"content"`
			Id      int64                                      `json:"id"`
			Role    GetAssistantHistory200JSONResponseBodyRole `json:"role"`
			Steps   *[]AssistantStep                           `json:"steps,omitempty"`
			Tools   *[]string                                  `json:"tools,omitempty"`
			Ts      time.Time                                  `json:"ts"`
		}{
			Content: row.Content,
			Id:      row.ID,
			Role:    GetAssistantHistory200JSONResponseBodyRole(row.Role),
			Ts:      row.CreatedAt,
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

func (s *Server) ClearAssistantHistory(ctx context.Context, _ ClearAssistantHistoryRequestObject) (ClearAssistantHistoryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if err := s.store.ClearAssistantMessages(ctx, id.User.ID); err != nil {
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
