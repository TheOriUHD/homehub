package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider speaks the Anthropic Messages API.
type AnthropicProvider struct {
	APIKey  string
	BaseURL string // default https://api.anthropic.com; overridable for tests
	Client  *http.Client
}

const anthropicVersion = "2023-06-01"

type anthropicContent struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		// Cached reads bill at a lower rate, but they are still input the
		// user paid something for; counting them keeps the total honest.
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *AnthropicProvider) Chat(ctx context.Context, model, system string, turns []Turn, tools []ToolDef) (Reply, error) {
	msgs := make([]anthropicMessage, 0, len(turns))
	for _, turn := range turns {
		var blocks []anthropicContent
		for _, res := range turn.Results {
			blocks = append(blocks, anthropicContent{
				Type: "tool_result", ToolUseID: res.ID, Content: res.Content, IsError: res.IsError,
			})
		}
		if turn.Text != "" {
			blocks = append(blocks, anthropicContent{Type: "text", Text: turn.Text})
		}
		for _, call := range turn.Calls {
			blocks = append(blocks, anthropicContent{
				Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Input,
			})
		}
		if len(blocks) == 0 {
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: turn.Role, Content: blocks})
	}

	req := anthropicRequest{Model: model, MaxTokens: 2048, System: system, Messages: msgs}
	for _, tool := range tools {
		req.Tools = append(req.Tools, anthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Reply{}, err
	}

	base := p.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
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
	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Reply{}, fmt.Errorf("the AI provider sent an unreadable response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil {
			// Surface the provider's own words — "invalid x-api-key" beats
			// a generic failure.
			return Reply{}, fmt.Errorf("the AI provider refused the request: %s", parsed.Error.Message)
		}
		return Reply{}, fmt.Errorf("the AI provider returned HTTP %d", resp.StatusCode)
	}

	var out Reply
	out.Usage = Usage{
		InputTokens: parsed.Usage.InputTokens + parsed.Usage.CacheReadInputTokens +
			parsed.Usage.CacheCreationInputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
	}
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			if out.Text != "" {
				out.Text += "\n"
			}
			out.Text += block.Text
		case "tool_use":
			out.Calls = append(out.Calls, ToolCall{ID: block.ID, Name: block.Name, Input: block.Input})
		}
	}
	return out, nil
}
