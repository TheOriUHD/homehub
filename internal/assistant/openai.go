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
	body, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}

	base := strings.TrimRight(p.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
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
		return Reply{}, fmt.Errorf("the AI provider could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Reply{}, err
	}
	var parsed oaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Reply{}, fmt.Errorf("the AI provider sent an unreadable response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return Reply{}, fmt.Errorf("the AI provider refused the request: %s", parsed.Error.Message)
		}
		return Reply{}, fmt.Errorf("the AI provider returned HTTP %d", resp.StatusCode)
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
