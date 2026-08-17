package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicRequestShapeAndParse(t *testing.T) {
	var got anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("auth headers missing: %v", r.Header)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("request not JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"t1","name":"list_systems","input":{}}],"stop_reason":"tool_use"}`))
	}))
	defer srv.Close()

	p := &AnthropicProvider{APIKey: "k", BaseURL: srv.URL}
	turns := []Turn{
		{Role: "user", Text: "status?"},
		{Role: "assistant", Text: "looking", Calls: []ToolCall{{ID: "a", Name: "x", Input: json.RawMessage(`{}`)}}},
		{Role: "user", Results: []ToolResult{{ID: "a", Content: "[]"}}},
	}
	reply, err := p.Chat(context.Background(), "model-x", "sys", turns, []ToolDef{{Name: "list_systems", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "model-x" || got.System != "sys" || len(got.Messages) != 3 || len(got.Tools) != 1 {
		t.Fatalf("request shape wrong: %+v", got)
	}
	// tool_result must precede any text in its user message per API contract
	if got.Messages[2].Content[0].Type != "tool_result" || got.Messages[2].Content[0].ToolUseID != "a" {
		t.Fatalf("tool_result block wrong: %+v", got.Messages[2])
	}
	if reply.Text != "checking" || len(reply.Calls) != 1 || reply.Calls[0].Name != "list_systems" {
		t.Fatalf("parse wrong: %+v", reply)
	}
}

func TestAnthropicErrorSurfacesProviderMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()
	p := &AnthropicProvider{APIKey: "bad", BaseURL: srv.URL}
	_, err := p.Chat(context.Background(), "m", "", []Turn{{Role: "user", Text: "hi"}}, nil)
	if err == nil || !contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("provider error not surfaced: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
