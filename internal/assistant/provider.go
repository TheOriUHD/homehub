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
	// InputTokens is the uncached remainder — tokens processed at full price.
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	// Prompt-cache activity, billed at different rates: reads ~0.1x input,
	// writes ~1.25x. Total prompt size is the sum of all three input fields.
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
}

// Add accumulates another call's usage.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.CacheWriteTokens += other.CacheWriteTokens
}

// PromptTokens is every token of prompt the model processed this turn,
// however it was billed — what a reader means by "input".
func (u Usage) PromptTokens() int64 {
	return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// System is the instruction prefix, split by how often it changes. Prompt
// caching is a prefix match, so a per-user greeting sitting in front of the
// shared instructions would give every user their own cache entry and share
// nothing. Base is byte-identical for everyone; Context follows it.
type System struct {
	Base    string
	Context string
}

// Text flattens the two halves for providers that do not cache prefixes.
func (s System) Text() string {
	if s.Context == "" {
		return s.Base
	}
	return s.Base + "\n" + s.Context
}

// Provider speaks to one LLM API. Implementations are stateless; the
// service owns history and the tool loop.
type Provider interface {
	Chat(ctx context.Context, model string, system System, turns []Turn, tools []ToolDef) (Reply, error)
}
