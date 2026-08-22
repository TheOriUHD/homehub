// Package assistant is HomeHub's conversational agent: a chat loop over a
// pluggable LLM provider with RBAC-scoped tools onto the systems HomeHub
// already manages. The caller's identity bounds everything a turn can see —
// the assistant never has more authority than the person talking to it.
package assistant

import (
	"context"
	"encoding/json"
)

// ToolDef describes one callable tool to the model.
type ToolDef struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolCall is the model asking for one tool invocation.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult answers one ToolCall.
type ToolResult struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	IsError bool   `json:"isError"`
}

// Turn is one exchange step. Role "user" carries Text (and, mid-loop, tool
// Results); role "assistant" carries Text and/or tool Calls.
type Turn struct {
	Role    string
	Text    string
	Calls   []ToolCall
	Results []ToolResult
}

// Reply is the model's next step: tool calls to run, or final text.
type Reply struct {
	Text  string
	Calls []ToolCall
	Usage Usage
}

// Usage is what one model call consumed. A turn sums it across every round
// of the tool loop, so the figure shown against an answer is the whole cost
// of producing it, not just the last round.
type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

// Add accumulates another call's usage.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
}

// Provider speaks to one LLM API. Implementations are stateless; the
// service owns history and the tool loop.
type Provider interface {
	Chat(ctx context.Context, model, system string, turns []Turn, tools []ToolDef) (Reply, error)
}
