package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider speaks the OpenAI Chat Completions dialect — the lingua
// franca of the ecosystem: OpenAI itself, Mistral, Groq, DeepSeek, xAI,
// OpenRouter, Together, Fireworks, and every local runtime (Ollama,
// LM Studio, llama.cpp, vLLM, LocalAI) expose it. An empty APIKey simply
// omits the Authorization header, which local runtimes expect.
type OpenAIProvider struct {
	APIKey  string
	BaseURL string // ends in /v1 by convention
	Client  *http.Client
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaRequest struct {
	Model    string      `json:"model"`
	Messages []oaMessage `json:"messages"`
	Tools    []oaTool    `json:"tools,omitempty"`
	// ReasoningEffort is only ever sent as an adaptive retry: OpenAI's
	// reasoning models refuse function tools on chat/completions unless
	// it is "none", while older models reject the parameter entirely.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaResponse struct {
	Choices []struct {
		Message struct {
			Content   *string      `json:"content"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *OpenAIProvider) Chat(ctx context.Context, model, system string, turns []Turn, tools []ToolDef) (Reply, error) {
	msgs := []oaMessage{{Role: "system", Content: system}}
	for _, turn := range turns {
		// Tool results become individual role:"tool" messages BEFORE the
		// next user text, mirroring how the calls were issued.
		for _, res := range turn.Results {
			content := res.Content
			if res.IsError {
				content = "ERROR: " + content
			}
			msgs = append(msgs, oaMessage{Role: "tool", ToolCallID: res.ID, Content: content})
		}
		if turn.Role == "assistant" && len(turn.Calls) > 0 {
			m := oaMessage{Role: "assistant", Content: turn.Text}
			for _, call := range turn.Calls {
				tc := oaToolCall{ID: call.ID, Type: "function"}
				tc.Function.Name = call.Name
				tc.Function.Arguments = string(call.Input)
				m.ToolCalls = append(m.ToolCalls, tc)
			}
			msgs = append(msgs, m)
			continue
		}
		if turn.Text != "" {
			msgs = append(msgs, oaMessage{Role: turn.Role, Content: turn.Text})
		}
	}

	req := oaRequest{Model: model, Messages: msgs}
	for _, def := range tools {
		var t oaTool
		t.Type = "function"
		t.Function.Name = def.Name
		t.Function.Description = def.Description
		t.Function.Parameters = def.InputSchema
		req.Tools = append(req.Tools, t)
	}
	parsed, status, err := p.do(ctx, req)
	if err != nil {
		return Reply{}, err
	}
	if status != http.StatusOK &&
		parsed.Error != nil && strings.Contains(parsed.Error.Message, "reasoning_effort") &&
		req.ReasoningEffort == "" {
		// The model's own instruction: tools on chat/completions need
		// reasoning_effort "none". Retry once, its way.
		req.ReasoningEffort = "none"
		parsed, status, err = p.do(ctx, req)
		if err != nil {
			return Reply{}, err
		}
	}
	if status != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return Reply{}, fmt.Errorf("the AI provider refused the request: %s", parsed.Error.Message)
		}
		return Reply{}, fmt.Errorf("the AI provider returned HTTP %d", status)
	}
	if len(parsed.Choices) == 0 {
		return Reply{}, fmt.Errorf("the AI provider sent no choices")
	}

	var out Reply
	msg := parsed.Choices[0].Message
	if msg.Content != nil {
		out.Text = *msg.Content
	}
	for _, tc := range msg.ToolCalls {
		input := json.RawMessage(tc.Function.Arguments)
		if len(input) == 0 || !json.Valid(input) {
			input = json.RawMessage("{}")
		}
		out.Calls = append(out.Calls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: input})
	}
	return out, nil
}

// do performs one round-trip and parses whatever came back; non-200
// responses return with the parsed error so the caller can adapt.
func (p *OpenAIProvider) do(ctx context.Context, req oaRequest) (oaResponse, int, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return oaResponse{}, 0, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIEndpoint(p.BaseURL), bytes.NewReader(body))
	if err != nil {
		return oaResponse{}, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	client := p.Client
	if client == nil {
		// Local models on small hardware can be slow; give them room.
		client = &http.Client{Timeout: 300 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return oaResponse{}, 0, fmt.Errorf("the AI provider could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return oaResponse{}, resp.StatusCode, err
	}
	var parsed oaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return oaResponse{}, resp.StatusCode, fmt.Errorf("the AI provider sent an unreadable response (HTTP %d)", resp.StatusCode)
	}
	return parsed, resp.StatusCode, nil
}

// openAIEndpoint normalizes a user-entered base URL into the chat
// endpoint: trailing slashes go, and a pasted full path is accepted
// rather than doubled.
func openAIEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}
