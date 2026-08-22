package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIRequestShapeAndParse(t *testing.T) {
	var got oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header wrong: %q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("request not JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c9","type":"function","function":{"name":"list_systems","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()

	p := &OpenAIProvider{APIKey: "k", BaseURL: srv.URL + "/v1"}
	turns := []Turn{
		{Role: "user", Text: "status?"},
		{Role: "assistant", Calls: []ToolCall{{ID: "a", Name: "x", Input: json.RawMessage(`{"q":1}`)}}},
		{Role: "user", Results: []ToolResult{{ID: "a", Content: "[]"}}},
	}
	reply, err := p.Chat(context.Background(), "m1", System{Base: "sys"}, turns, []ToolDef{{Name: "list_systems", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	// system + user + assistant(tool_calls) + tool = 4 messages
	if len(got.Messages) != 4 || got.Messages[0].Role != "system" {
		t.Fatalf("message shape wrong: %+v", got.Messages)
	}
	if got.Messages[2].ToolCalls[0].Function.Arguments != `{"q":1}` {
		t.Fatalf("arguments not stringified: %+v", got.Messages[2])
	}
	if got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "a" {
		t.Fatalf("tool result message wrong: %+v", got.Messages[3])
	}
	if len(got.Tools) != 1 || got.Tools[0].Type != "function" {
		t.Fatalf("tools wrong: %+v", got.Tools)
	}
	if len(reply.Calls) != 1 || reply.Calls[0].Name != "list_systems" || reply.Text != "" {
		t.Fatalf("parse wrong: %+v", reply)
	}
}

// No Authorization header at all for keyless local runtimes — some reject
// an empty Bearer.
func TestOpenAINoAuthHeaderWithoutKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("Authorization header must be absent when key is empty")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()
	p := &OpenAIProvider{BaseURL: srv.URL + "/v1"}
	reply, err := p.Chat(context.Background(), "m", System{Base: "s"}, []Turn{{Role: "user", Text: "x"}}, nil)
	if err != nil || reply.Text != "hi" {
		t.Fatalf("keyless chat failed: %+v %v", reply, err)
	}
}

func TestGeminiRequestShapeAndParse(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1beta/models/gem-x:generateContent") {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "gk" {
			t.Errorf("key header wrong")
		}
		raw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"checking"},{"functionCall":{"name":"list_systems","args":{"a":1}}}]}}]}`))
	}))
	defer srv.Close()

	p := &GeminiProvider{APIKey: "gk", BaseURL: srv.URL}
	turns := []Turn{
		{Role: "user", Text: "status?"},
		{Role: "assistant", Calls: []ToolCall{{ID: "id1", Name: "x", Input: json.RawMessage(`{"b":2}`)}}},
		{Role: "user", Results: []ToolResult{{ID: "id1", Content: "[]"}}},
	}
	reply, err := p.Chat(context.Background(), "gem-x", System{Base: "sys"}, turns, []ToolDef{{Name: "list_systems", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req["systemInstruction"] == nil || req["tools"] == nil {
		t.Fatalf("system/tools missing: %v", string(raw))
	}
	// The functionResponse must pair by NAME (x), recovered from the call id.
	if !strings.Contains(string(raw), `"functionResponse":{"name":"x"`) {
		t.Fatalf("functionResponse not paired by name: %s", string(raw))
	}
	if reply.Text != "checking" || len(reply.Calls) != 1 || reply.Calls[0].Name != "list_systems" {
		t.Fatalf("parse wrong: %+v", reply)
	}
	if !strings.HasPrefix(reply.Calls[0].ID, "gem-list_systems") {
		t.Fatalf("synthetic id missing: %+v", reply.Calls[0])
	}
}

func TestRegistryEngines(t *testing.T) {
	for _, info := range Providers {
		p, err := NewProviderFor(Config{Provider: info.ID, Model: "m", APIKey: "k"})
		if err != nil || p == nil {
			t.Errorf("provider %s does not construct: %v", info.ID, err)
		}
	}
	if _, err := NewProviderFor(Config{Provider: "nope"}); err == nil {
		t.Error("unknown provider must error")
	}
}

// OpenAI's reasoning models refuse tools on chat/completions unless
// reasoning_effort is "none"; the provider must retry once their way.
func TestOpenAIReasoningEffortRetry(t *testing.T) {
	var requests []oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req oaRequest
		_ = json.Unmarshal(raw, &req)
		requests = append(requests, req)
		if req.ReasoningEffort != "none" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-5.6-luna in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()

	p := &OpenAIProvider{APIKey: "k", BaseURL: srv.URL + "/v1"}
	// A model OUTSIDE the known reasoning families exercises the backstop.
	reply, err := p.Chat(context.Background(), "futurellm-9", System{Base: "s"},
		[]Turn{{Role: "user", Text: "x"}},
		[]ToolDef{{Name: "t", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil || reply.Text != "hi" {
		t.Fatalf("retry did not recover: %+v %v", reply, err)
	}
	if len(requests) != 2 || requests[0].ReasoningEffort != "" || requests[1].ReasoningEffort != "none" {
		t.Fatalf("wrong retry sequence: %+v", requests)
	}
}

// Known reasoning families are hardcoded: "none" on the FIRST request,
// no refusal round-trip.
func TestOpenAIReasoningEffortHardcoded(t *testing.T) {
	var requests []oaRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req oaRequest
		_ = json.Unmarshal(raw, &req)
		requests = append(requests, req)
		if req.ReasoningEffort != "none" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Function tools with reasoning_effort are not supported"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()
	p := &OpenAIProvider{APIKey: "k", BaseURL: srv.URL + "/v1"}
	for _, model := range []string{"gpt-5.6-luna", "gpt-5", "o3-mini", "o4-nano"} {
		requests = nil
		reply, err := p.Chat(context.Background(), model, System{Base: "s"},
			[]Turn{{Role: "user", Text: "x"}},
			[]ToolDef{{Name: "t", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})
		if err != nil || reply.Text != "hi" {
			t.Fatalf("%s: %+v %v", model, reply, err)
		}
		if len(requests) != 1 || requests[0].ReasoningEffort != "none" {
			t.Fatalf("%s: expected single hardcoded request, got %+v", model, requests)
		}
	}
	// Toolless requests must NOT carry the parameter even on gpt-5.
	requests = nil
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req oaRequest
		_ = json.Unmarshal(raw, &req)
		requests = append(requests, req)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv2.Close()
	p2 := &OpenAIProvider{APIKey: "k", BaseURL: srv2.URL + "/v1"}
	if _, err := p2.Chat(context.Background(), "gpt-5.6-luna", System{Base: "s"}, []Turn{{Role: "user", Text: "x"}}, nil); err != nil {
		t.Fatal(err)
	}
	if requests[0].ReasoningEffort != "" {
		t.Fatalf("toolless request must not set reasoning_effort: %+v", requests[0])
	}
}

// Unrelated refusals must NOT trigger the retry.
func TestOpenAINoRetryOnOtherErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
	}))
	defer srv.Close()
	p := &OpenAIProvider{APIKey: "bad", BaseURL: srv.URL + "/v1"}
	_, err := p.Chat(context.Background(), "m", System{Base: ""}, []Turn{{Role: "user", Text: "x"}}, nil)
	if err == nil || calls != 1 {
		t.Fatalf("unexpected retry: calls=%d err=%v", calls, err)
	}
}
