package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureRequest runs one Chat and returns the JSON body that went to the API.
func captureRequest(t *testing.T, system System, turns []Turn, tools []ToolDef) anthropicRequest {
	t.Helper()
	var got anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p := &AnthropicProvider{APIKey: "k", BaseURL: srv.URL}
	if _, err := p.Chat(context.Background(), "claude-sonnet-5", system, turns, tools); err != nil {
		t.Fatal(err)
	}
	return got
}

// Caching is a prefix match on exact bytes, so the marker has to sit on the
// stable half of the instructions — never after the per-user line, which
// would give every user a private entry and share nothing.
func TestCacheBreakpointSitsOnTheStablePrefix(t *testing.T) {
	req := captureRequest(t,
		System{Base: "frozen instructions", Context: "You are talking to Philipp."},
		[]Turn{{Role: "user", Text: "status?"}},
		[]ToolDef{{Name: "list_systems", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}})

	if len(req.System) != 2 {
		t.Fatalf("system should be two blocks (stable, then per-user), got %d", len(req.System))
	}
	if req.System[0].Text != "frozen instructions" || req.System[0].CacheControl == nil {
		t.Error("the stable block must carry the breakpoint")
	}
	if req.System[1].CacheControl != nil {
		t.Error("the per-user block must NOT be cached — it is what differs between users")
	}
}

// Each round of the tool loop resends the whole conversation. Marking the
// newest block is what lets the next round read that prefix back.
func TestCacheBreakpointFollowsTheConversation(t *testing.T) {
	turns := []Turn{
		{Role: "user", Text: "status?"},
		{Role: "assistant", Text: "looking", Calls: []ToolCall{{ID: "a", Name: "x", Input: json.RawMessage(`{}`)}}},
		{Role: "user", Results: []ToolResult{{ID: "a", Content: "[]"}}},
	}
	req := captureRequest(t, System{Base: "b"}, turns, nil)

	last := req.Messages[len(req.Messages)-1]
	if last.Content[len(last.Content)-1].CacheControl == nil {
		t.Error("the newest content block must carry a breakpoint")
	}
	// Anthropic allows at most four breakpoints per request; more is a 400.
	marks := 0
	for _, b := range req.System {
		if b.CacheControl != nil {
			marks++
		}
	}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.CacheControl != nil {
				marks++
			}
		}
	}
	if marks > 4 {
		t.Errorf("%d breakpoints — the API caps them at 4", marks)
	}
}

// The prefix must be byte-identical between turns or nothing is ever read.
// The one input that varies per user is the only thing allowed to move.
func TestStablePrefixIsIdenticalAcrossUsers(t *testing.T) {
	a := captureRequest(t, systemPrompt(Access{DisplayName: "Alice"}), []Turn{{Role: "user", Text: "hi"}}, nil)
	b := captureRequest(t, systemPrompt(Access{DisplayName: "Bob"}), []Turn{{Role: "user", Text: "hi"}}, nil)
	if a.System[0].Text != b.System[0].Text {
		t.Error("the cached system block differs per user — no entry can ever be shared")
	}
	if a.System[1].Text == b.System[1].Text {
		t.Error("the uncached block should be the part that names the user")
	}
}

// A cache read bills at a tenth of the input rate and a write at 1.25x.
// Pricing them as plain input would overstate every turn after the first.
func TestCostUsesCacheRates(t *testing.T) {
	plain, _ := EstimateCost("claude-sonnet-5", Usage{InputTokens: 1_000_000})
	read, _ := EstimateCost("claude-sonnet-5", Usage{CacheReadTokens: 1_000_000})
	write, _ := EstimateCost("claude-sonnet-5", Usage{CacheWriteTokens: 1_000_000})

	if got := read / plain; got < 0.09 || got > 0.11 {
		t.Errorf("cache read priced at %.2fx input, expected ~0.1x", got)
	}
	if got := write / plain; got < 1.2 || got > 1.3 {
		t.Errorf("cache write priced at %.2fx input, expected ~1.25x", got)
	}
	// The reader's "input" figure is every token processed, whatever the rate.
	u := Usage{InputTokens: 100, CacheReadTokens: 8000, CacheWriteTokens: 100}
	if u.PromptTokens() != 8200 {
		t.Errorf("PromptTokens = %d, want the sum of all three input fields", u.PromptTokens())
	}
}

// The persona is per-surface, but it is a constant — so each surface still
// has ONE stable cacheable prefix, not one per user or per turn.
func TestPersonaIsPerSurfaceAndStillCacheable(t *testing.T) {
	web := systemPrompt(Access{DisplayName: "Philipp"})
	tg := systemPrompt(Access{DisplayName: "Philipp", Channel: "telegram"})

	if !strings.Contains(tg.Base, "management system") {
		t.Error("the Telegram surface should carry its own voice")
	}
	if strings.Contains(web.Base, "management system") {
		t.Error("the web assistant sits inside the app and keeps the plain voice")
	}
	if !strings.Contains(tg.Base, systemBase) {
		t.Error("the persona must extend the shared instructions, not replace them")
	}

	// Two people on the same surface must share the cached prefix, or every
	// user pays to write their own entry.
	other := systemPrompt(Access{DisplayName: "Someone Else", Channel: "telegram"})
	if tg.Base != other.Base {
		t.Error("the Telegram prefix differs per user — nothing would ever be cached")
	}
	if tg.Context == other.Context {
		t.Error("the per-user half should be the part that names them")
	}
}

// Brevity instructions push a small model toward answering from nothing.
func TestTelegramPersonaForbidsEstimates(t *testing.T) {
	base := systemPrompt(Access{Channel: "telegram"}).Base
	for _, want := range []string{"do not estimate", "look it up before answering", "outranks a confident guess"} {
		if !strings.Contains(strings.ToLower(base), strings.ToLower(want)) {
			t.Errorf("the Telegram persona is missing %q — brevity without this invites invented figures", want)
		}
	}
}
