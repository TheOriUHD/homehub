package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// OpenAI's prompt_tokens includes the cached prefix. Pricing it all as fresh
// input overstated every turn of a long conversation by most of its history.
func TestOpenAIUsageSplitsTheCachedPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}],
			"usage":{"prompt_tokens":10000,"completion_tokens":300,"prompt_tokens_details":{"cached_tokens":8000}}}`))
	}))
	defer srv.Close()
	reply, err := (&OpenAIProvider{BaseURL: srv.URL + "/v1"}).Chat(context.Background(), "m", System{Base: "s"}, []Turn{{Role: "user", Text: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{InputTokens: 2000, OutputTokens: 300, CacheReadTokens: 8000}
	if reply.Usage != want {
		t.Fatalf("usage = %+v, want %+v", reply.Usage, want)
	}
	if reply.Usage.PromptTokens() != 10000 {
		t.Errorf("the whole prompt must still read as 10000 tokens, got %d", reply.Usage.PromptTokens())
	}
}

// Gemini bills thinking tokens as output but leaves them out of
// candidatesTokenCount, and its prompt count includes the implicit cache.
func TestGeminiUsageCountsThinkingAndCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}],
			"usageMetadata":{"promptTokenCount":5000,"candidatesTokenCount":200,"cachedContentTokenCount":4000,"thoughtsTokenCount":900}}`))
	}))
	defer srv.Close()
	reply, err := (&GeminiProvider{APIKey: "k", BaseURL: srv.URL}).Chat(context.Background(), "gem", System{Base: "s"}, []Turn{{Role: "user", Text: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{InputTokens: 1000, OutputTokens: 1100, CacheReadTokens: 4000}
	if reply.Usage != want {
		t.Fatalf("usage = %+v, want %+v", reply.Usage, want)
	}
}

// The figures behind the info tooltip. Wrong prices here are invisible until
// someone reconciles a bill, so pin the ratios the picker's warnings promise.
func TestEstimateCostMatchesTheAdvertisedRatios(t *testing.T) {
	u := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	cost := func(model string) float64 {
		c, ok := EstimateCost(model, u)
		if !ok {
			t.Fatalf("no price for %s", model)
		}
		return c
	}
	sonnet, opus, fable := cost("claude-sonnet-5"), cost("claude-opus-5"), cost("claude-fable-5")

	// "About 2.5× the price of Sonnet 5" — the Opus gate's claim.
	if ratio := opus / sonnet; ratio < 2.4 || ratio > 2.6 {
		t.Errorf("Opus/Sonnet = %.2f, but the confirmation says about 2.5×", ratio)
	}
	// "Twice the price of Opus 5" — the Fable gate's claim.
	if ratio := fable / opus; ratio < 1.95 || ratio > 2.05 {
		t.Errorf("Fable/Opus = %.2f, but the confirmation says twice", ratio)
	}
	// A dated snapshot must price identically to the row it aliases.
	pinned, ok := EstimateCost("claude-haiku-4-5-20251001", u)
	if !ok || pinned != cost("claude-haiku-4-5") {
		t.Errorf("aliased Haiku snapshot priced differently: %v %v", pinned, ok)
	}
	// An unknown model reports "no price", never a silent zero.
	if _, ok := EstimateCost("qwen3:8b", u); ok {
		t.Error("a model with no published price must not be priced")
	}
}

// A worked example in the units the tooltip shows.
func TestEstimateCostWorkedExample(t *testing.T) {
	// 8.2k in + 420 out on Haiku 4.5 ($1 / $5 per MTok).
	got, _ := EstimateCost("claude-haiku-4-5", Usage{InputTokens: 8200, OutputTokens: 420})
	want := 8200/1e6*1.0 + 420/1e6*5.0 // = 0.0103
	if got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	if got < 0.0102 || got > 0.0104 {
		t.Fatalf("expected about $0.010, got %v", got)
	}
}
